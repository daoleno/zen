package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxPluginCatalogBytes   = 8 << 20
	maxCatalogPlugins       = 512
	maxInstalledPlugins     = 128
	maxPluginComponents     = 128
	maxPluginDescription    = 400
	maxPluginIDLength       = 141
	maxPluginManifestBytes  = 1 << 20
	defaultPluginCLITimeout = 6 * time.Second
)

type PluginHost string

const (
	PluginHostClaude PluginHost = "claude"
	PluginHostCodex  PluginHost = "codex"
)

type PluginSource string

const (
	PluginSourceManager     PluginSource = "manager"
	PluginSourceCache       PluginSource = "cache"
	PluginSourceRemoteCache PluginSource = "remote_cache"
)

type PluginMutationOperation string

const (
	PluginOperationInstall   PluginMutationOperation = "install"
	PluginOperationUninstall PluginMutationOperation = "uninstall"
)

type PluginRuntime interface {
	List(ctx context.Context, host PluginHost, options InventoryOptions, includeAvailable bool) ([]byte, error)
	Execute(ctx context.Context, host PluginHost, args []string, options MutationExecutionOptions) (MutationExecution, error)
}

type nativePluginRuntime struct{}

type AvailablePlugin struct {
	PluginID        string     `json:"plugin_id"`
	Name            string     `json:"name"`
	DisplayName     string     `json:"display_name,omitempty"`
	MarketplaceName string     `json:"marketplace_name"`
	Version         string     `json:"version,omitempty"`
	Description     string     `json:"description,omitempty"`
	SourceURL       string     `json:"source_url,omitempty"`
	SourceRef       string     `json:"source_ref,omitempty"`
	Host            PluginHost `json:"host"`
	Installable     bool       `json:"installable"`
}

type PluginComponent struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
}

type PluginCapability struct {
	CanUninstall bool   `json:"can_uninstall"`
	Reason       string `json:"reason,omitempty"`
}

type InstalledPluginCopy struct {
	CopyID        string            `json:"copy_id"`
	PluginID      string            `json:"plugin_id"`
	Name          string            `json:"name"`
	DisplayName   string            `json:"display_name,omitempty"`
	Description   string            `json:"description,omitempty"`
	Marketplace   string            `json:"marketplace"`
	Version       string            `json:"version,omitempty"`
	Scope         string            `json:"scope"`
	Enabled       bool              `json:"enabled"`
	Host          PluginHost        `json:"host"`
	Source        PluginSource      `json:"source"`
	RootPath      string            `json:"root_path"`
	CanonicalPath string            `json:"canonical_path"`
	AllowedRoot   string            `json:"allowed_root"`
	Location      string            `json:"location"`
	Revision      string            `json:"revision"`
	Agents        []Agent           `json:"agents"`
	Components    []PluginComponent `json:"components"`
	Capability    PluginCapability  `json:"capability"`
}

type PluginInventory struct {
	GeneratedAt time.Time             `json:"generated_at"`
	Installed   []InstalledPluginCopy `json:"installed"`
	Available   []AvailablePlugin     `json:"available"`
	Warnings    []string              `json:"warnings,omitempty"`
}

type PluginMutationRequest struct {
	Operation     PluginMutationOperation
	PluginID      string
	Host          PluginHost
	Source        PluginSource
	Scope         string
	CopyID        string
	Name          string
	Version       string
	RootPath      string
	CanonicalPath string
	AllowedRoot   string
	Revision      string
	Agents        []Agent
}

type PluginMutationCommand struct {
	Operation     PluginMutationOperation `json:"operation"`
	PluginID      string                  `json:"plugin_id"`
	Host          PluginHost              `json:"host"`
	Source        PluginSource            `json:"source,omitempty"`
	Scope         string                  `json:"scope"`
	CopyID        string                  `json:"copy_id,omitempty"`
	Name          string                  `json:"name"`
	DisplayName   string                  `json:"display_name,omitempty"`
	Version       string                  `json:"version,omitempty"`
	RootPath      string                  `json:"root_path,omitempty"`
	CanonicalPath string                  `json:"canonical_path,omitempty"`
	AllowedRoot   string                  `json:"allowed_root,omitempty"`
	Location      string                  `json:"location,omitempty"`
	Revision      string                  `json:"revision,omitempty"`
	Agents        []Agent                 `json:"agents,omitempty"`
	Summary       string                  `json:"summary"`
	Destructive   bool                    `json:"destructive"`
}

var (
	pluginNamePattern        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	pluginMarketplacePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	pluginVersionPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)
)

var (
	ErrPluginCLIUnavailable   = errors.New("the plugin manager CLI is not available on this server")
	ErrPluginCatalogTimeout   = errors.New("the plugin manager inventory request timed out")
	ErrPluginCatalogOversized = errors.New("the plugin manager inventory response exceeded the size limit")
	ErrPluginCatalogMalformed = errors.New("the plugin manager returned malformed inventory data")
)

func NewPluginRuntime() PluginRuntime {
	return &nativePluginRuntime{}
}

func ValidatePluginID(value string) error {
	if value == "" || len(value) > maxPluginIDLength || !utf8.ValidString(value) {
		return errors.New("invalid Plugin identity")
	}
	parts := strings.Split(value, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("Plugin identity must be name@marketplace")
	}
	if !pluginNamePattern.MatchString(parts[0]) || !pluginMarketplacePattern.MatchString(parts[1]) {
		return errors.New("Plugin identity contains unsupported characters")
	}
	return nil
}

func ValidatePluginScope(scope string) error {
	if scope != "user" {
		return fmt.Errorf("unsupported managed Plugin scope %q", scope)
	}
	return nil
}

func ValidatePluginHost(host PluginHost) error {
	if host != PluginHostClaude && host != PluginHostCodex {
		return fmt.Errorf("unsupported Plugin host %q", host)
	}
	return nil
}

func ValidatePluginSource(source PluginSource) error {
	if source != PluginSourceManager && source != PluginSourceCache && source != PluginSourceRemoteCache {
		return fmt.Errorf("unsupported Plugin source %q", source)
	}
	return nil
}

func (runtime *nativePluginRuntime) List(ctx context.Context, host PluginHost, options InventoryOptions, includeAvailable bool) ([]byte, error) {
	binary, args, err := pluginListCommand(host, includeAvailable)
	if err != nil {
		return nil, err
	}
	path, err := exec.LookPath(binary)
	if err != nil {
		return nil, ErrPluginCLIUnavailable
	}
	commandCtx, cancel := context.WithTimeout(ctx, defaultPluginCLITimeout)
	defer cancel()
	command := exec.CommandContext(commandCtx, path, args...)
	command.Env = pluginCommandEnv(options)
	output, err := command.Output()
	if err != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return nil, ErrPluginCatalogTimeout
		}
		return nil, fmt.Errorf("%s Plugin inventory failed: %w", pluginHostLabel(host), err)
	}
	if len(output) > maxPluginCatalogBytes {
		return nil, ErrPluginCatalogOversized
	}
	return output, nil
}

func (runtime *nativePluginRuntime) Execute(ctx context.Context, host PluginHost, args []string, options MutationExecutionOptions) (MutationExecution, error) {
	binary := ""
	switch host {
	case PluginHostClaude:
		binary = "claude"
	case PluginHostCodex:
		binary = "codex"
	default:
		return MutationExecution{}, fmt.Errorf("unsupported Plugin host %q", host)
	}
	return executeCommandTokens(ctx, binary, args, options, PluginMutationTimeoutForOperation(args), pluginCommandEnv(options.InventoryOptions))
}

func pluginListCommand(host PluginHost, includeAvailable bool) (string, []string, error) {
	switch host {
	case PluginHostClaude:
		args := []string{"plugin", "list", "--json"}
		if includeAvailable {
			args = append(args, "--available")
		}
		return "claude", args, nil
	case PluginHostCodex:
		args := []string{"plugin", "list"}
		if includeAvailable {
			args = append(args, "--available")
		}
		args = append(args, "--json")
		return "codex", args, nil
	default:
		return "", nil, fmt.Errorf("unsupported Plugin host %q", host)
	}
}

func pluginCommandEnv(options InventoryOptions) []string {
	overrides := map[string]string{
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	}
	if options.Home != "" {
		overrides["HOME"] = options.Home
	}
	if options.CodexHome != "" {
		overrides["CODEX_HOME"] = options.CodexHome
	}
	if options.ClaudeHome != "" {
		overrides["CLAUDE_CONFIG_DIR"] = options.ClaudeHome
	}
	for key, value := range options.Env {
		overrides[key] = value
	}
	return replaceEnvironment(os.Environ(), overrides)
}

func replaceEnvironment(base []string, overrides map[string]string) []string {
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	replaced := make(map[string]struct{}, len(overrides))
	env := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, exists := overrides[key]; exists {
				replaced[key] = struct{}{}
				continue
			}
		}
		env = append(env, entry)
	}
	for _, key := range keys {
		env = append(env, key+"="+overrides[key])
	}
	return env
}

type claudeCatalogEnvelope struct {
	Installed []struct {
		ID          string `json:"id"`
		Version     string `json:"version"`
		Scope       string `json:"scope"`
		Enabled     bool   `json:"enabled"`
		InstallPath string `json:"installPath"`
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

type codexCatalogEnvelope struct {
	Installed []codexCatalogPlugin `json:"installed"`
	Available []codexCatalogPlugin `json:"available"`
}

type codexCatalogPlugin struct {
	PluginID        string `json:"pluginId"`
	Name            string `json:"name"`
	MarketplaceName string `json:"marketplaceName"`
	Version         string `json:"version"`
	Installed       bool   `json:"installed"`
	Enabled         bool   `json:"enabled"`
	Source          struct {
		Source string `json:"source"`
		Path   string `json:"path"`
	} `json:"source"`
}

type managerSnapshot struct {
	installed []managerInstalledPlugin
	available []AvailablePlugin
	warnings  []string
}

type managerInstalledPlugin struct {
	pluginID    string
	name        string
	marketplace string
	version     string
	enabled     bool
	host        PluginHost
	rootPath    string
}

func DiscoverPluginInventory(options InventoryOptions, runtime PluginRuntime) (PluginInventory, error) {
	normalized, err := normalizeInventoryOptions(options)
	if err != nil {
		return PluginInventory{}, err
	}
	if runtime == nil {
		runtime = NewPluginRuntime()
	}

	claude, codex := readPluginManagers(normalized, runtime)
	if err := normalized.Context.Err(); err != nil {
		return PluginInventory{}, err
	}

	installed, scanWarnings := discoverPluginCopies(normalized, claude.installed, codex.installed)
	available := append(append([]AvailablePlugin{}, claude.available...), codex.available...)
	sort.Slice(available, func(i, j int) bool {
		if available[i].Name != available[j].Name {
			return available[i].Name < available[j].Name
		}
		if available[i].Host != available[j].Host {
			return available[i].Host < available[j].Host
		}
		return available[i].PluginID < available[j].PluginID
	})
	warnings := append(append(append([]string{}, claude.warnings...), codex.warnings...), scanWarnings...)
	if len(warnings) > 12 {
		warnings = warnings[:12]
	}
	now := time.Now
	if normalized.Now != nil {
		now = normalized.Now
	}
	return PluginInventory{
		GeneratedAt: now().UTC(),
		Installed:   installed,
		Available:   available,
		Warnings:    warnings,
	}, nil
}

func readPluginManagers(options InventoryOptions, runtime PluginRuntime) (managerSnapshot, managerSnapshot) {
	type result struct {
		host     PluginHost
		snapshot managerSnapshot
	}
	results := make(chan result, 2)
	for _, host := range []PluginHost{PluginHostClaude, PluginHostCodex} {
		go func() {
			results <- result{host: host, snapshot: readPluginManager(options, runtime, host)}
		}()
	}
	var claude managerSnapshot
	var codex managerSnapshot
	for range 2 {
		current := <-results
		if current.host == PluginHostClaude {
			claude = current.snapshot
		} else {
			codex = current.snapshot
		}
	}
	return claude, codex
}

func readPluginManager(options InventoryOptions, runtime PluginRuntime, host PluginHost) managerSnapshot {
	data, err := runtime.List(options.Context, host, options, true)
	if err != nil {
		return managerSnapshot{warnings: []string{pluginHostLabel(host) + " Plugin manager is unavailable; cache copies are read-only."}}
	}
	if len(data) == 0 || len(data) > maxPluginCatalogBytes {
		return managerSnapshot{warnings: []string{pluginHostLabel(host) + " Plugin manager returned invalid inventory; cache copies are read-only."}}
	}
	switch host {
	case PluginHostClaude:
		return parseClaudeCatalog(data)
	case PluginHostCodex:
		return parseCodexCatalog(data)
	default:
		return managerSnapshot{warnings: []string{"Unsupported Plugin manager inventory was ignored."}}
	}
}

func parseClaudeCatalog(data []byte) managerSnapshot {
	var envelope claudeCatalogEnvelope
	if json.Unmarshal(data, &envelope) != nil || len(envelope.Installed) > maxInstalledPlugins || len(envelope.Available) > maxCatalogPlugins {
		return managerSnapshot{warnings: []string{"Claude Code Plugin manager returned malformed inventory; cache copies are read-only."}}
	}
	snapshot := managerSnapshot{}
	installedIDs := make(map[string]struct{}, len(envelope.Installed))
	for _, entry := range envelope.Installed {
		name, marketplace, ok := splitPluginID(entry.ID)
		if !ok || entry.Scope != "user" {
			continue
		}
		version := cleanPluginVersion(entry.Version)
		rootPath := filepath.Clean(strings.TrimSpace(entry.InstallPath))
		if rootPath == "." || !filepath.IsAbs(rootPath) {
			rootPath = ""
		}
		snapshot.installed = append(snapshot.installed, managerInstalledPlugin{
			pluginID: entry.ID, name: name, marketplace: marketplace,
			version: version, enabled: entry.Enabled, host: PluginHostClaude, rootPath: rootPath,
		})
		installedIDs[entry.ID] = struct{}{}
	}
	for _, entry := range envelope.Available {
		name, marketplace, ok := splitPluginID(entry.PluginID)
		if !ok || name != entry.Name || marketplace != entry.MarketplaceName {
			continue
		}
		_, installed := installedIDs[entry.PluginID]
		snapshot.available = append(snapshot.available, AvailablePlugin{
			PluginID: entry.PluginID, Name: name, DisplayName: cleanMetadata(entry.Name, maxSkillNameLength),
			MarketplaceName: marketplace, Description: cleanMetadata(entry.Description, maxPluginDescription),
			SourceURL: cleanMetadata(entry.Source.URL, 1024), SourceRef: cleanMetadata(entry.Source.Ref, 128),
			Host: PluginHostClaude, Installable: !installed,
		})
	}
	return snapshot
}

func parseCodexCatalog(data []byte) managerSnapshot {
	var envelope codexCatalogEnvelope
	if json.Unmarshal(data, &envelope) != nil || len(envelope.Installed) > maxInstalledPlugins || len(envelope.Available) > maxCatalogPlugins {
		return managerSnapshot{warnings: []string{"Codex Plugin manager returned malformed inventory; cache copies are read-only."}}
	}
	snapshot := managerSnapshot{}
	installedIDs := make(map[string]struct{}, len(envelope.Installed))
	for _, entry := range envelope.Installed {
		name, marketplace, ok := splitPluginID(entry.PluginID)
		if !ok || name != entry.Name || marketplace != entry.MarketplaceName {
			continue
		}
		version := cleanPluginVersion(entry.Version)
		snapshot.installed = append(snapshot.installed, managerInstalledPlugin{
			pluginID: entry.PluginID, name: name, marketplace: marketplace,
			version: version, enabled: entry.Enabled, host: PluginHostCodex,
		})
		installedIDs[entry.PluginID] = struct{}{}
	}
	for _, entry := range envelope.Available {
		name, marketplace, ok := splitPluginID(entry.PluginID)
		if !ok || name != entry.Name || marketplace != entry.MarketplaceName {
			continue
		}
		_, installed := installedIDs[entry.PluginID]
		snapshot.available = append(snapshot.available, AvailablePlugin{
			PluginID: entry.PluginID, Name: name, DisplayName: cleanMetadata(entry.Name, maxSkillNameLength),
			MarketplaceName: marketplace, Version: cleanPluginVersion(entry.Version),
			SourceRef: cleanMetadata(entry.Source.Path, 1024), Host: PluginHostCodex,
			Installable: !installed && !entry.Installed,
		})
	}
	return snapshot
}

func discoverPluginCopies(options InventoryOptions, managerRows ...[]managerInstalledPlugin) ([]InstalledPluginCopy, []string) {
	rows := make([]InstalledPluginCopy, 0)
	warnings := []string(nil)
	managedRoots := map[string]struct{}{}
	for _, group := range managerRows {
		for _, entry := range group {
			copy, warning := managerPluginCopy(options, entry)
			if warning != "" {
				warnings = append(warnings, warning)
			}
			rows = append(rows, copy)
			if copy.RootPath != "" {
				managedRoots[pluginRootKey(copy.Host, copy.RootPath)] = struct{}{}
			}
		}
	}
	cacheRows, cacheWarnings := walkPluginCaches(options, managedRoots)
	rows = append(rows, cacheRows...)
	warnings = append(warnings, cacheWarnings...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		if rows[i].Host != rows[j].Host {
			return rows[i].Host < rows[j].Host
		}
		if rows[i].Marketplace != rows[j].Marketplace {
			return rows[i].Marketplace < rows[j].Marketplace
		}
		return rows[i].RootPath < rows[j].RootPath
	})
	if len(rows) > maxInstalledPlugins {
		warnings = append(warnings, "Plugin inventory stopped at its installed copy limit.")
		rows = rows[:maxInstalledPlugins]
	}
	return rows, warnings
}

func managerPluginCopy(options InventoryOptions, entry managerInstalledPlugin) (InstalledPluginCopy, string) {
	rootPath := entry.rootPath
	if rootPath == "" {
		rootPath = filepath.Join(pluginCacheRoot(options, entry.host), entry.marketplace, entry.name, entry.version)
	}
	allowedRoot := filepath.Join(pluginCacheRoot(options, entry.host), entry.marketplace, entry.name)
	copy := buildPluginCopy(entry.host, PluginSourceManager, entry.pluginID, entry.name, entry.marketplace, entry.version, rootPath, allowedRoot, entry.enabled)
	reason := managedPluginUninstallReason(copy)
	copy.Capability = PluginCapability{CanUninstall: reason == "", Reason: reason}
	if reason != "" {
		return copy, pluginHostLabel(entry.host) + " reports " + entry.pluginID + " installed, but Zen cannot safely uninstall this copy."
	}
	return copy, ""
}

func walkPluginCaches(options InventoryOptions, managedRoots map[string]struct{}) ([]InstalledPluginCopy, []string) {
	rows := []InstalledPluginCopy(nil)
	warnings := []string(nil)
	for _, source := range []struct {
		host PluginHost
		root string
	}{
		{host: PluginHostClaude, root: filepath.Join(options.ClaudeHome, "plugins", "cache")},
		{host: PluginHostCodex, root: filepath.Join(options.CodexHome, "plugins", "cache")},
	} {
		marketplaces, err := os.ReadDir(source.root)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				warnings = append(warnings, "Could not read the "+pluginHostLabel(source.host)+" Plugin cache.")
			}
			continue
		}
		for _, marketplaceEntry := range marketplaces {
			if !marketplaceEntry.IsDir() || !pluginMarketplacePattern.MatchString(marketplaceEntry.Name()) {
				continue
			}
			marketplace := marketplaceEntry.Name()
			marketplaceRoot := filepath.Join(source.root, marketplace)
			plugins, readErr := os.ReadDir(marketplaceRoot)
			if readErr != nil {
				continue
			}
			for _, pluginEntry := range plugins {
				if !pluginEntry.IsDir() || !pluginNamePattern.MatchString(pluginEntry.Name()) {
					continue
				}
				name := pluginEntry.Name()
				pluginRoot := filepath.Join(marketplaceRoot, name)
				versions, versionErr := os.ReadDir(pluginRoot)
				if versionErr != nil {
					continue
				}
				remote := source.host == PluginHostCodex && fileExists(filepath.Join(pluginRoot, ".codex-remote-plugin-install.json"))
				for _, versionEntry := range versions {
					if !versionEntry.IsDir() || !pluginVersionPattern.MatchString(versionEntry.Name()) {
						continue
					}
					rootPath := filepath.Join(pluginRoot, versionEntry.Name())
					if _, managed := managedRoots[pluginRootKey(source.host, rootPath)]; managed {
						continue
					}
					pluginSource := PluginSourceCache
					if remote {
						pluginSource = PluginSourceRemoteCache
					}
					copy := buildPluginCopy(source.host, pluginSource, name+"@"+marketplace, name, marketplace, versionEntry.Name(), rootPath, pluginRoot, true)
					copy.Capability = PluginCapability{Reason: readonlyPluginReason(copy)}
					rows = append(rows, copy)
				}
			}
		}
	}
	return rows, warnings
}

func buildPluginCopy(host PluginHost, source PluginSource, pluginID, name, marketplace, version, rootPath, allowedRoot string, enabled bool) InstalledPluginCopy {
	rootPath = filepath.Clean(rootPath)
	allowedRoot = filepath.Clean(allowedRoot)
	canonicalPath := rootPath
	if resolved, err := filepath.EvalSymlinks(rootPath); err == nil {
		if absolute, absErr := filepath.Abs(resolved); absErr == nil {
			canonicalPath = filepath.Clean(absolute)
		}
	}
	displayName, description := readPluginPresentation(rootPath, name)
	components := collectPluginComponents(rootPath)
	location := pluginHostLabel(host) + " user Plugins"
	if source == PluginSourceRemoteCache {
		location = "Codex remote Plugin cache"
	}
	revision := pluginRevision(host, source, pluginID, version, rootPath, canonicalPath, allowedRoot)
	copyID := installedPluginCopyID(host, source, pluginID, rootPath, canonicalPath, allowedRoot)
	return InstalledPluginCopy{
		CopyID: copyID, PluginID: pluginID, Name: name, DisplayName: displayName, Description: description,
		Marketplace: marketplace, Version: version, Scope: "user", Enabled: enabled,
		Host: host, Source: source, RootPath: rootPath, CanonicalPath: canonicalPath,
		AllowedRoot: allowedRoot, Location: location, Revision: revision,
		Agents: []Agent{pluginHostAgent(host)}, Components: components,
	}
}

func managedPluginUninstallReason(copy InstalledPluginCopy) string {
	if copy.Source != PluginSourceManager {
		return readonlyPluginReason(copy)
	}
	if err := validatePluginCopyIdentity(copy); err != nil {
		return "This manager record cannot be uninstalled safely: " + err.Error() + "."
	}
	if info, err := os.Lstat(copy.RootPath); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "This manager record does not point to a safe local Plugin directory."
	}
	return ""
}

func readonlyPluginReason(copy InstalledPluginCopy) string {
	if copy.Source == PluginSourceRemoteCache {
		if copy.Host == PluginHostCodex && copy.Name == "plugin-management" {
			return "Provided by Codex to manage Plugins and connections; it cannot be removed from this page."
		}
		return "Provided by Codex's remote Plugin service and managed outside the local marketplace lifecycle."
	}
	return "This cache copy is not registered with its Agent's local Plugin manager, so Zen will not delete it."
}

func validatePluginCopyIdentity(copy InstalledPluginCopy) error {
	if ValidatePluginID(copy.PluginID) != nil || ValidatePluginHost(copy.Host) != nil || ValidatePluginSource(copy.Source) != nil {
		return errors.New("Plugin copy identity is invalid")
	}
	if copy.Name == "" || copy.Marketplace == "" || copy.PluginID != copy.Name+"@"+copy.Marketplace {
		return errors.New("Plugin copy name does not match its manager identity")
	}
	for _, value := range []string{copy.RootPath, copy.CanonicalPath, copy.AllowedRoot} {
		if !filepath.IsAbs(value) || filepath.Clean(value) != value {
			return errors.New("Plugin copy identity is not canonical")
		}
	}
	if copy.RootPath == copy.AllowedRoot {
		return errors.New("refusing to uninstall a Plugin inventory root")
	}
	relative, err := filepath.Rel(copy.AllowedRoot, copy.RootPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.Dir(relative) != "." {
		return errors.New("Plugin copy escaped its allowed root")
	}
	if relative != copy.Version || filepath.Base(copy.RootPath) != copy.Version {
		return errors.New("Plugin version does not match its exact root")
	}
	if !slices.Equal(copy.Agents, []Agent{pluginHostAgent(copy.Host)}) {
		return errors.New("Plugin Agent availability does not match its owning manager")
	}
	for _, path := range []string{filepath.Dir(copy.AllowedRoot), copy.AllowedRoot, copy.RootPath} {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Plugin copy path contains unsupported symlink traversal")
		}
	}
	if installedPluginCopyID(copy.Host, copy.Source, copy.PluginID, copy.RootPath, copy.CanonicalPath, copy.AllowedRoot) != copy.CopyID {
		return errors.New("Plugin copy ID does not match its roots")
	}
	if pluginRevision(copy.Host, copy.Source, copy.PluginID, copy.Version, copy.RootPath, copy.CanonicalPath, copy.AllowedRoot) != copy.Revision {
		return errors.New("Plugin copy revision changed after discovery")
	}
	resolved, err := filepath.EvalSymlinks(copy.RootPath)
	if err != nil {
		return fmt.Errorf("resolve selected Plugin root: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil || filepath.Clean(resolved) != copy.CanonicalPath {
		return errors.New("selected Plugin root changed after discovery")
	}
	resolvedAllowed, err := filepath.EvalSymlinks(copy.AllowedRoot)
	if err != nil {
		return fmt.Errorf("resolve allowed Plugin root: %w", err)
	}
	resolvedAllowed, err = filepath.Abs(resolvedAllowed)
	if err != nil || filepath.Dir(copy.CanonicalPath) != filepath.Clean(resolvedAllowed) {
		return errors.New("selected Plugin directory escaped its resolved allowed root")
	}
	return nil
}

func collectPluginComponents(rootPath string) []PluginComponent {
	components := make([]PluginComponent, 0)
	for _, directory := range []struct {
		name string
		kind string
	}{
		{name: "skills", kind: "skill"},
		{name: "agents", kind: "agent"},
		{name: "commands", kind: "command"},
		{name: "hooks", kind: "hook"},
	} {
		entries, err := os.ReadDir(filepath.Join(rootPath, directory.name))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if len(components) >= maxPluginComponents || strings.HasPrefix(entry.Name(), ".") {
				break
			}
			components = append(components, PluginComponent{
				Kind: directory.kind, Name: entry.Name(), Path: filepath.ToSlash(filepath.Join(directory.name, entry.Name())),
			})
		}
	}
	for _, file := range []struct {
		name  string
		kind  string
		label string
	}{
		{name: ".mcp.json", kind: "mcp", label: "MCP servers"},
		{name: ".app.json", kind: "app", label: "Apps"},
	} {
		if len(components) >= maxPluginComponents {
			break
		}
		if fileExists(filepath.Join(rootPath, file.name)) {
			components = append(components, PluginComponent{Kind: file.kind, Name: file.label, Path: file.name})
		}
	}
	sort.Slice(components, func(i, j int) bool {
		if components[i].Kind != components[j].Kind {
			return components[i].Kind < components[j].Kind
		}
		return components[i].Name < components[j].Name
	})
	return components
}

func readPluginPresentation(rootPath, fallback string) (string, string) {
	for _, relative := range []string{filepath.Join(".codex-plugin", "plugin.json"), filepath.Join(".claude-plugin", "plugin.json"), "plugin.json", "package.json"} {
		data, err := os.ReadFile(filepath.Join(rootPath, relative))
		if err != nil || len(data) > maxPluginManifestBytes {
			continue
		}
		var manifest struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Interface   struct {
				DisplayName string `json:"displayName"`
			} `json:"interface"`
		}
		if json.Unmarshal(data, &manifest) != nil {
			continue
		}
		displayName := cleanMetadata(manifest.Interface.DisplayName, maxSkillNameLength)
		if displayName == "" {
			displayName = cleanMetadata(manifest.Name, maxSkillNameLength)
		}
		if displayName == "" {
			displayName = fallback
		}
		return displayName, cleanMetadata(manifest.Description, maxPluginDescription)
	}
	return fallback, ""
}

func pluginRevision(host PluginHost, source PluginSource, pluginID, version, rootPath, canonicalPath, allowedRoot string) string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, strings.Join([]string{string(host), string(source), pluginID, version, rootPath, canonicalPath, allowedRoot}, "\x00"))
	for _, relative := range []string{
		filepath.Join(".codex-plugin", "plugin.json"),
		filepath.Join(".claude-plugin", "plugin.json"),
		"plugin.json", "package.json", ".mcp.json", ".app.json",
	} {
		data, err := os.ReadFile(filepath.Join(rootPath, relative))
		if err != nil || len(data) > maxPluginManifestBytes {
			continue
		}
		_, _ = io.WriteString(hash, "\x00"+filepath.ToSlash(relative)+"\x00")
		_, _ = hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func installedPluginCopyID(host PluginHost, source PluginSource, pluginID, rootPath, canonicalPath, allowedRoot string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{string(host), string(source), pluginID, rootPath, canonicalPath, allowedRoot}, "\x00")))
	return hex.EncodeToString(sum[:12])
}

func pluginRootKey(host PluginHost, rootPath string) string {
	return string(host) + "\x00" + filepath.Clean(rootPath)
}

func pluginCacheRoot(options InventoryOptions, host PluginHost) string {
	if host == PluginHostClaude {
		return filepath.Join(options.ClaudeHome, "plugins", "cache")
	}
	return filepath.Join(options.CodexHome, "plugins", "cache")
}

func pluginHostAgent(host PluginHost) Agent {
	if host == PluginHostClaude {
		return AgentClaudeCode
	}
	return AgentCodex
}

func pluginHostLabel(host PluginHost) string {
	if host == PluginHostClaude {
		return "Claude Code"
	}
	return "Codex"
}

func splitPluginID(id string) (string, string, bool) {
	if ValidatePluginID(id) != nil {
		return "", "", false
	}
	parts := strings.Split(id, "@")
	return parts[0], parts[1], true
}

func cleanPluginVersion(value string) string {
	value = cleanMetadata(value, 64)
	if value == "" || !pluginVersionPattern.MatchString(value) {
		return "unknown"
	}
	return value
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
