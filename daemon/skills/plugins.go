package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Plugin lifecycle is owned by the client that hosts the plugin (plugins may
// bundle MCP servers, hooks, agents, connectors, and auth). Zen never invents
// a parallel plugin manager; it consumes the owning client's authoritative
// catalog and builds exact reviewed commands for the user's terminal.

const (
	maxPluginCatalogBytes   = 8 << 20
	maxCatalogPlugins       = 512
	maxPluginHostedSkills   = 128
	maxPluginDescription    = 400
	maxPluginIDLength       = 141
	defaultPluginCLITimeout = 6 * time.Second
)

type PluginHost string

const (
	PluginHostClaude PluginHost = "claude"
	PluginHostCodex  PluginHost = "codex"
)

type PluginMutationOperation string

const (
	PluginOperationInstall   PluginMutationOperation = "install"
	PluginOperationUpdate    PluginMutationOperation = "update"
	PluginOperationUninstall PluginMutationOperation = "uninstall"
)

// PluginCLI is the owning client's read-only catalog surface. Production uses
// `claude plugin list --json --available`; tests inject a fake implementation
// so no real CLI is ever executed.
type PluginCLI interface {
	ListAvailable(ctx context.Context) ([]byte, error)
}

type claudePluginCLI struct {
	timeout time.Duration
}

// PluginCatalogState carries the owning client's authoritative catalog truth.
// A non-ready state is an explicit capability gap: the App must never render
// install affordances without a ready catalog.
type PluginCatalogState struct {
	Status    string                   `json:"status"`
	Available []AvailablePlugin        `json:"available,omitempty"`
	Installed []CatalogInstalledPlugin `json:"installed,omitempty"`
	Code      string                   `json:"code,omitempty"`
	Message   string                   `json:"message,omitempty"`
}

type CatalogInstalledPlugin struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
}

type AvailablePlugin struct {
	PluginID        string `json:"plugin_id"`
	Name            string `json:"name"`
	MarketplaceName string `json:"marketplace_name"`
	Description     string `json:"description,omitempty"`
	SourceURL       string `json:"source_url,omitempty"`
	SourceRef       string `json:"source_ref,omitempty"`
	Installable     bool   `json:"installable"`
}

type PluginHostedSkill struct {
	Name          string `json:"name"`
	CanonicalPath string `json:"canonical_path"`
	SourcePath    string `json:"source_path"`
}

type InstalledPluginRow struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Marketplace string              `json:"marketplace"`
	Version     string              `json:"version"`
	Scope       string              `json:"scope"`
	Enabled     bool                `json:"enabled"`
	Host        PluginHost          `json:"host"`
	Mutable     bool                `json:"mutable"`
	Source      string              `json:"source"`
	SkillCount  int                 `json:"skill_count"`
	Skills      []PluginHostedSkill `json:"skills"`
}

type PluginInventory struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Catalog     PluginCatalogState   `json:"catalog"`
	Installed   []InstalledPluginRow `json:"installed"`
	Warnings    []string             `json:"warnings,omitempty"`
}

type PluginMutationRequest struct {
	Operation PluginMutationOperation
	PluginID  string
	Scope     string
}

type PluginMutationCommand struct {
	Operation PluginMutationOperation `json:"operation"`
	Command   string                  `json:"command"`
	PluginID  string                  `json:"plugin_id"`
	Scope     string                  `json:"scope"`
	Host      PluginHost              `json:"host"`
}

var (
	pluginNamePattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	pluginMarketplacePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	pluginVersionPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
)

func ValidatePluginID(value string) error {
	if value == "" || len(value) > maxPluginIDLength || !utf8.ValidString(value) {
		return errors.New("invalid plugin identity")
	}
	parts := strings.Split(value, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("plugin identity must be name@marketplace")
	}
	if !pluginNamePattern.MatchString(parts[0]) || !pluginMarketplacePattern.MatchString(parts[1]) {
		return errors.New("plugin identity contains unsupported characters")
	}
	return nil
}

func ValidatePluginScope(scope string) error {
	if scope != "user" {
		return fmt.Errorf("unsupported managed plugin scope %q", scope)
	}
	return nil
}

var (
	ErrClaudeCLIUnavailable   = errors.New("the claude CLI is not available on this server")
	ErrClaudeCatalogTimeout   = errors.New("the claude plugin catalog request timed out")
	ErrClaudeCatalogOversized = errors.New("the claude plugin catalog response exceeded the size limit")
	ErrClaudeCatalogMalformed = errors.New("the claude plugin catalog returned malformed data")
)

// NewClaudePluginCLI returns the production catalog source: the owning
// client's read-only list command executed directly (no shell), with bounded
// timeout.
func NewClaudePluginCLI() PluginCLI {
	return &claudePluginCLI{timeout: defaultPluginCLITimeout}
}

func (cli *claudePluginCLI) ListAvailable(ctx context.Context) ([]byte, error) {
	path, err := exec.LookPath("claude")
	if err != nil {
		return nil, ErrClaudeCLIUnavailable
	}
	timeout := cli.timeout
	if timeout <= 0 {
		timeout = defaultPluginCLITimeout
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(cmdCtx, path, "plugin", "list", "--json", "--available")
	command.Env = append(os.Environ(), "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1")
	output, err := command.Output()
	if err != nil {
		if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
			return nil, ErrClaudeCatalogTimeout
		}
		return nil, fmt.Errorf("claude plugin catalog failed: %w", err)
	}
	if len(output) > maxPluginCatalogBytes {
		return nil, ErrClaudeCatalogOversized
	}
	return output, nil
}

type claudeCatalogEnvelope struct {
	Installed []struct {
		ID      string `json:"id"`
		Version string `json:"version"`
		Scope   string `json:"scope"`
		Enabled bool   `json:"enabled"`
	} `json:"installed"`
	Available []struct {
		PluginID        string `json:"pluginId"`
		Name            string `json:"name"`
		MarketplaceName string `json:"marketplaceName"`
		Description     string `json:"description"`
		Source          struct {
			URL string `json:"url"`
			Ref string `json:"ref"`
		} `json:"source"`
	} `json:"available"`
}

func parseClaudeCatalog(data []byte) (PluginCatalogState, error) {
	if len(data) == 0 || len(data) > maxPluginCatalogBytes {
		return PluginCatalogState{}, ErrClaudeCatalogMalformed
	}
	var envelope claudeCatalogEnvelope
	if json.Unmarshal(data, &envelope) != nil {
		return PluginCatalogState{}, ErrClaudeCatalogMalformed
	}
	state := PluginCatalogState{Status: "ready"}

	installedIDs := make(map[string]struct{}, len(envelope.Installed))
	for _, entry := range envelope.Installed {
		if ValidatePluginID(entry.ID) != nil || entry.Scope != "user" {
			continue
		}
		installedIDs[entry.ID] = struct{}{}
		state.Installed = append(state.Installed, CatalogInstalledPlugin{
			ID:      entry.ID,
			Version: cleanMetadata(entry.Version, 64),
			Enabled: entry.Enabled,
		})
	}
	sort.Slice(state.Installed, func(i, j int) bool {
		return state.Installed[i].ID < state.Installed[j].ID
	})
	if len(envelope.Installed) > 0 && len(state.Installed) == 0 {
		return PluginCatalogState{}, ErrClaudeCatalogMalformed
	}

	if len(envelope.Available) > maxCatalogPlugins {
		return PluginCatalogState{}, ErrClaudeCatalogMalformed
	}
	seen := make(map[string]struct{}, len(envelope.Available))
	for _, entry := range envelope.Available {
		if ValidatePluginID(entry.PluginID) != nil {
			continue
		}
		name := cleanMetadata(entry.Name, maxSkillNameLength)
		marketplace := cleanMetadata(entry.MarketplaceName, maxSkillNameLength)
		if name == "" || marketplace == "" {
			continue
		}
		if _, exists := seen[entry.PluginID]; exists {
			continue
		}
		seen[entry.PluginID] = struct{}{}
		_, alreadyInstalled := installedIDs[entry.PluginID]
		state.Available = append(state.Available, AvailablePlugin{
			PluginID:        entry.PluginID,
			Name:            name,
			MarketplaceName: marketplace,
			Description:     cleanMetadata(entry.Description, maxPluginDescription),
			SourceURL:       cleanMetadata(entry.Source.URL, 1024),
			SourceRef:       cleanMetadata(entry.Source.Ref, 128),
			Installable:     !alreadyInstalled,
		})
	}
	sort.Slice(state.Available, func(i, j int) bool {
		return state.Available[i].PluginID < state.Available[j].PluginID
	})
	if len(envelope.Available) > 0 && len(state.Available) == 0 {
		return PluginCatalogState{}, ErrClaudeCatalogMalformed
	}
	return state, nil
}

// DiscoverPluginInventory builds the authoritative Plugin inventory: the
// owning client's catalog for lifecycle truth, plus the bounded cache walk for
// installed rows and their hosted Skills. A claude CLI gap is an explicit
// catalog state, never a silent guess.
func DiscoverPluginInventory(options InventoryOptions, cli PluginCLI) (PluginInventory, error) {
	normalized, err := normalizeInventoryOptions(options)
	if err != nil {
		return PluginInventory{}, err
	}
	if normalized.Context == nil {
		normalized.Context = context.Background()
	}

	catalog := readClaudeCatalog(normalized.Context, cli)
	installed, warnings := resolvePluginLifecycle(normalized, catalog)

	now := time.Now
	if normalized.Now != nil {
		now = normalized.Now
	}
	return PluginInventory{
		GeneratedAt: now().UTC(),
		Catalog:     catalog,
		Installed:   installed,
		Warnings:    warnings,
	}, nil
}

func readClaudeCatalog(ctx context.Context, cli PluginCLI) PluginCatalogState {
	if cli == nil {
		cli = NewClaudePluginCLI()
	}
	data, err := cli.ListAvailable(ctx)
	if err != nil {
		code := "claude_catalog_unavailable"
		if errors.Is(err, ErrClaudeCatalogTimeout) {
			code = "claude_catalog_timeout"
		} else if errors.Is(err, ErrClaudeCatalogOversized) {
			code = "claude_catalog_oversized"
		}
		return PluginCatalogState{
			Status:  "unavailable",
			Code:    code,
			Message: err.Error(),
		}
	}
	state, parseErr := parseClaudeCatalog(data)
	if parseErr != nil {
		return PluginCatalogState{
			Status:  "unavailable",
			Code:    "claude_catalog_malformed",
			Message: parseErr.Error(),
		}
	}
	return state
}

// resolvePluginLifecycle builds the Installed view from cache rows plus the
// owning client's catalog truth. The catalog is the only lifecycle authority:
// a Claude row is manageable (source "catalog", mutable) exactly when the
// ready catalog lists it as installed. Cache rows enrich names, versions, and
// hosted Skills only; without catalog membership they are explicitly
// read-only cache rows. Catalog-installed entries appear even when no cache
// directory exists.
func resolvePluginLifecycle(options InventoryOptions, catalog PluginCatalogState) ([]InstalledPluginRow, []string) {
	rows, warnings := walkPluginCaches(options)
	catalogInstalled := make(map[string]CatalogInstalledPlugin, len(catalog.Installed))
	for _, entry := range catalog.Installed {
		catalogInstalled[entry.ID] = entry
	}
	catalogReady := catalog.Status == "ready"

	byID := make(map[string]*InstalledPluginRow, len(rows)+len(catalogInstalled))
	for index := range rows {
		row := &rows[index]
		if row.Host == PluginHostClaude && catalogReady {
			if entry, ok := catalogInstalled[row.ID]; ok {
				row.Source = "catalog"
				row.Mutable = true
				row.Enabled = entry.Enabled
				if entry.Version != "" && entry.Version != "unknown" {
					row.Version = entry.Version
				}
			}
		}
		byID[row.ID] = row
	}

	// Catalog-installed membership proves installed even without a cache dir.
	if catalogReady {
		for _, entry := range catalog.Installed {
			if _, exists := byID[entry.ID]; exists {
				continue
			}
			name, marketplace, ok := splitPluginID(entry.ID)
			if !ok {
				continue
			}
			byID[entry.ID] = &InstalledPluginRow{
				ID:          entry.ID,
				Name:        name,
				Marketplace: marketplace,
				Version:     entry.Version,
				Scope:       "user",
				Enabled:     entry.Enabled,
				Host:        PluginHostClaude,
				Mutable:     true,
				Source:      "catalog",
				SkillCount:  0,
				Skills:      []PluginHostedSkill{},
			}
		}
	}

	final := make([]InstalledPluginRow, 0, len(byID))
	for _, row := range byID {
		final = append(final, *row)
	}
	sort.Slice(final, func(i, j int) bool { return final[i].ID < final[j].ID })
	return final, warnings
}

func splitPluginID(id string) (name, marketplace string, ok bool) {
	parts := strings.Split(id, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// walkPluginCaches lists installed plugins from the client plugin caches with
// the same bounded traversal rules as the Skills inventory scan. Cache rows
// are disk truth (a plugin is installed); lifecycle truth comes from the
// owning client's catalog and mutation builder.
func walkPluginCaches(options InventoryOptions) ([]InstalledPluginRow, []string) {
	type pendingDirectory struct {
		path  string
		depth int
	}
	warnings := []string(nil)
	byID := make(map[string]*InstalledPluginRow)

	visited := 0
	roots := 0
	collect := func(cachePath string, host PluginHost, provenance string) {
		pending := []pendingDirectory{{path: cachePath}}
		for len(pending) > 0 {
			if visited >= maxPluginWalkEntries || roots >= maxPluginRoots {
				warnings = append(warnings, "Plugin inventory stopped at its bounded traversal limit.")
				return
			}
			current := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			directory, err := os.Open(current.path)
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) || current.depth > 0 {
					warnings = append(warnings, "Could not read the "+provenance+" plugin cache.")
				}
				continue
			}
			entries, readErr := directory.ReadDir(-1)
			_ = directory.Close()
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				warnings = append(warnings, "Could not finish reading the "+provenance+" plugin cache.")
			}
			for _, entry := range entries {
				if visited >= maxPluginWalkEntries || roots >= maxPluginRoots {
					warnings = append(warnings, "Plugin inventory stopped at its bounded traversal limit.")
					return
				}
				visited++
				if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
					continue
				}
				path := filepath.Join(current.path, entry.Name())
				nextDepth := current.depth + 1
				if nextDepth == 3 {
					// cache/marketplace/name/version
					marketplace := filepath.Base(filepath.Dir(filepath.Dir(path)))
					name := filepath.Base(filepath.Dir(path))
					version := entry.Name()
					roots++
					if !validatePluginPathIdentity(marketplace, name, version) {
						continue
					}
					row := &InstalledPluginRow{
						ID:          name + "@" + marketplace,
						Name:        name,
						Marketplace: marketplace,
						Version:     version,
						Scope:       "user",
						Host:        host,
						Mutable:     false,
						Source:      "cache",
						Skills:      []PluginHostedSkill{},
					}
					collectHostedSkills(row, path)
					if previous, exists := byID[row.ID]; exists {
						if previous.Version == "unknown" && row.Version != "unknown" {
							previous.Version = row.Version
						}
						if len(previous.Skills) == 0 && len(row.Skills) > 0 {
							previous.Skills = row.Skills
						}
					} else {
						byID[row.ID] = row
					}
					continue
				}
				if nextDepth > 3 {
					continue
				}
				pending = append(pending, pendingDirectory{path: path, depth: nextDepth})
			}
		}
	}

	collect(filepath.Join(options.ClaudeHome, "plugins", "cache"), PluginHostClaude, "Claude Code")
	collect(filepath.Join(options.CodexHome, "plugins", "cache"), PluginHostCodex, "Codex")

	rows := make([]InstalledPluginRow, 0, len(byID))
	for _, row := range byID {
		sort.Slice(row.Skills, func(i, j int) bool { return row.Skills[i].Name < row.Skills[j].Name })
		row.SkillCount = len(row.Skills)
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, warnings
}

func validatePluginPathIdentity(marketplace, name, version string) bool {
	if !pluginMarketplacePattern.MatchString(marketplace) || !pluginNamePattern.MatchString(name) {
		return false
	}
	if !pluginVersionPattern.MatchString(version) || len(version) > 64 {
		return false
	}
	return true
}

func collectHostedSkills(row *InstalledPluginRow, versionRoot string) {
	skillsRoot := filepath.Join(versionRoot, "skills")
	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if len(row.Skills) >= maxPluginHostedSkills {
			break
		}
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := cleanMetadata(entry.Name(), maxSkillNameLength)
		if name == "" {
			continue
		}
		path := filepath.Join(skillsRoot, entry.Name())
		realPath, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr != nil {
			continue
		}
		realPath, resolveErr = filepath.Abs(realPath)
		if resolveErr != nil {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(path, "SKILL.md")); statErr != nil {
			continue
		}
		row.Skills = append(row.Skills, PluginHostedSkill{
			Name:          name,
			CanonicalPath: filepath.Clean(realPath),
			SourcePath:    filepath.Clean(path),
		})
	}
}

// BuildPluginMutationCommand re-reads the owning client's bounded catalog at
// command preparation and validates the request against it. The catalog is
// the only lifecycle authority: install requires an exact ready-catalog
// available/installable identity; update/uninstall require exact
// catalog-installed membership. Unavailable, absent, malformed, timed-out,
// or unsupported states are rejected. No command is ever executed by the
// daemon and no shell interpolation exists: every token is a validated
// literal.
func BuildPluginMutationCommand(options InventoryOptions, request PluginMutationRequest, cli PluginCLI) (PluginMutationCommand, error) {
	if err := ValidatePluginScope(request.Scope); err != nil {
		return PluginMutationCommand{}, err
	}
	if err := ValidatePluginID(request.PluginID); err != nil {
		return PluginMutationCommand{}, err
	}
	if options.Context == nil {
		options.Context = context.Background()
	}
	catalog := readClaudeCatalog(options.Context, cli)
	if catalog.Status != "ready" {
		return PluginMutationCommand{}, fmt.Errorf("the plugin catalog is unavailable: %s", catalog.Message)
	}

	switch request.Operation {
	case PluginOperationInstall:
		var entry *AvailablePlugin
		for index := range catalog.Available {
			if catalog.Available[index].PluginID == request.PluginID {
				entry = &catalog.Available[index]
				break
			}
		}
		if entry == nil {
			return PluginMutationCommand{}, errors.New("the plugin identity is not present in the owning client's catalog")
		}
		if !entry.Installable {
			return PluginMutationCommand{}, errors.New("the plugin is already installed on this server")
		}
		return PluginMutationCommand{
			Operation: request.Operation,
			Command:   "claude plugin install " + request.PluginID + " --scope user",
			PluginID:  request.PluginID,
			Scope:     request.Scope,
			Host:      PluginHostClaude,
		}, nil
	case PluginOperationUpdate, PluginOperationUninstall:
		var installed *CatalogInstalledPlugin
		for index := range catalog.Installed {
			if catalog.Installed[index].ID == request.PluginID {
				installed = &catalog.Installed[index]
				break
			}
		}
		if installed == nil {
			return PluginMutationCommand{}, errors.New("the plugin is not present in the owning client's installed catalog")
		}
		verb := "update"
		flags := ""
		if request.Operation == PluginOperationUninstall {
			verb = "uninstall"
			flags = " --yes"
		}
		return PluginMutationCommand{
			Operation: request.Operation,
			Command:   "claude plugin " + verb + " " + request.PluginID + " --scope user" + flags,
			PluginID:  request.PluginID,
			Scope:     request.Scope,
			Host:      PluginHostClaude,
		}, nil
	default:
		return PluginMutationCommand{}, fmt.Errorf("unsupported plugin operation %q", request.Operation)
	}
}
