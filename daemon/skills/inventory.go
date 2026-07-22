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
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	defaultMaxInstalledSkills = 600
	defaultMaxInventoryWork   = 5000
	maxSkillFrontmatterBytes  = 16 << 10
	maxLockFileBytes          = 1 << 20
	maxInventoryWarnings      = 12
	maxPluginWalkEntries      = 2500
	maxPluginRoots            = 48
)

type InventoryOptions struct {
	Context      context.Context
	CWD          string
	Home         string
	CodexHome    string
	ClaudeHome   string
	XDGStateHome string
	MaxSkills    int
	MaxWork      int
	Now          func() time.Time
}

type inventoryRoot struct {
	path       string
	label      string
	scope      Scope
	agents     []Agent
	manager    Manager
	provenance string
	plugin     string
	lock       map[string]lockEntry
}

type lockEntry struct {
	Source          string `json:"source"`
	SourceURL       string `json:"sourceUrl"`
	SourceType      string `json:"sourceType"`
	SkillPath       string `json:"skillPath"`
	SkillFolderHash string `json:"skillFolderHash"`
	ComputedHash    string `json:"computedHash"`
	PluginName      string `json:"pluginName"`
}

type lockFile struct {
	Version int                  `json:"version"`
	Skills  map[string]lockEntry `json:"skills"`
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type inventoryCollector struct {
	options    InventoryOptions
	byReal     map[string]*InstalledSkill
	blocked    map[string]string
	warnings   []string
	warned     map[string]struct{}
	count      int
	work       int
	incomplete bool
	stopped    bool
}

func DiscoverInventory(options InventoryOptions) (Inventory, error) {
	normalized, err := normalizeInventoryOptions(options)
	if err != nil {
		return Inventory{}, err
	}

	projectLock := map[string]lockEntry(nil)
	projectLockWarning := false
	if normalized.CWD != "" {
		projectLock, projectLockWarning = readOfficialLock(filepath.Join(normalized.CWD, "skills-lock.json"), 1)
	}
	globalLockPath := filepath.Join(normalized.Home, ".agents", ".skill-lock.json")
	if normalized.XDGStateHome != "" {
		globalLockPath = filepath.Join(normalized.XDGStateHome, "skills", ".skill-lock.json")
	}
	globalLock, globalLockWarning := readOfficialLock(globalLockPath, 3)

	collector := &inventoryCollector{
		options: normalized,
		byReal:  make(map[string]*InstalledSkill),
		blocked: make(map[string]string),
		warned:  make(map[string]struct{}),
	}
	if projectLockWarning {
		collector.markIncomplete()
		collector.warn("The project Skills lock could not be read or validated.")
	}
	if globalLockWarning {
		collector.markIncomplete()
		collector.warn("The global Skills lock could not be read or validated.")
	}
	for _, root := range inventoryRoots(normalized, projectLock, globalLock) {
		collector.scanRoot(root)
		if collector.stopped {
			break
		}
	}
	if !collector.stopped {
		collector.scanPluginCaches()
	}
	inventoryErr := normalized.Context.Err()
	if inventoryErr != nil {
		collector.stopIncomplete()
		collector.warn("Installed Skills inventory traversal was canceled.")
	}
	collector.rejectDuplicateIdentities()
	if collector.incomplete {
		collector.disableAllMutationAuthority()
	}

	installed := make([]InstalledSkill, 0, len(collector.byReal))
	for _, skill := range collector.byReal {
		sortAgents(skill.Agents)
		for index := range skill.Bindings {
			sortAgents(skill.Bindings[index].Agents)
		}
		sort.Slice(skill.Bindings, func(i, j int) bool { return skill.Bindings[i].SourcePath < skill.Bindings[j].SourcePath })
		if skill.Manager == ManagerSkillsCLI && len(skill.Agents) == 0 {
			skill.Capability = ManagementCapability{
				Reason: "The canonical CLI install has no linked supported agent target.",
			}
		}
		finalizeRemovalPlans(skill)
		installed = append(installed, *skill)
	}
	sort.Slice(installed, func(i, j int) bool {
		if installed[i].Scope != installed[j].Scope {
			return scopeRank(installed[i].Scope) < scopeRank(installed[j].Scope)
		}
		if installed[i].Name != installed[j].Name {
			return installed[i].Name < installed[j].Name
		}
		return installed[i].CanonicalPath < installed[j].CanonicalPath
	})

	now := time.Now
	if normalized.Now != nil {
		now = normalized.Now
	}
	inventory := Inventory{
		GeneratedAt: now().UTC(),
		CWD:         normalized.CWD,
		Skills:      installed,
		Agents:      SupportedAgents(),
		Warnings:    collector.warnings,
		incomplete:  collector.incomplete,
	}
	return inventory, inventoryErr
}

func normalizeInventoryOptions(options InventoryOptions) (InventoryOptions, error) {
	explicitHome := strings.TrimSpace(options.Home) != ""
	home := strings.TrimSpace(options.Home)
	if home == "" {
		resolved, err := os.UserHomeDir()
		if err != nil {
			return InventoryOptions{}, fmt.Errorf("resolve home directory: %w", err)
		}
		home = resolved
	}
	if !filepath.IsAbs(home) {
		return InventoryOptions{}, errors.New("home directory must be absolute")
	}
	home = filepath.Clean(home)

	cwd, err := ValidateCWD(options.CWD, false)
	if err != nil {
		return InventoryOptions{}, err
	}
	if cwd != "" {
		info, statErr := os.Stat(cwd)
		if statErr != nil || !info.IsDir() {
			return InventoryOptions{}, errors.New("working directory is unavailable")
		}
	}

	codexHome := strings.TrimSpace(options.CodexHome)
	if codexHome == "" && !explicitHome {
		codexHome = strings.TrimSpace(os.Getenv("CODEX_HOME"))
	}
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	claudeHome := strings.TrimSpace(options.ClaudeHome)
	if claudeHome == "" && !explicitHome {
		claudeHome = strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	}
	if claudeHome == "" {
		claudeHome = filepath.Join(home, ".claude")
	}
	xdgStateHome := strings.TrimSpace(options.XDGStateHome)
	if xdgStateHome == "" && !explicitHome {
		xdgStateHome = strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	}
	for _, value := range []string{codexHome, claudeHome} {
		if !filepath.IsAbs(value) {
			return InventoryOptions{}, errors.New("agent home directory must be absolute")
		}
	}
	if xdgStateHome != "" && !filepath.IsAbs(xdgStateHome) {
		return InventoryOptions{}, errors.New("XDG state directory must be absolute")
	}

	maxSkills := options.MaxSkills
	if maxSkills <= 0 || maxSkills > defaultMaxInstalledSkills {
		maxSkills = defaultMaxInstalledSkills
	}
	maxWork := options.MaxWork
	if maxWork <= 0 || maxWork > defaultMaxInventoryWork {
		maxWork = defaultMaxInventoryWork
	}
	if options.Context == nil {
		options.Context = context.Background()
	}
	options.Home = home
	options.CWD = cwd
	options.CodexHome = filepath.Clean(codexHome)
	options.ClaudeHome = filepath.Clean(claudeHome)
	options.XDGStateHome = filepath.Clean(xdgStateHome)
	if xdgStateHome == "" {
		options.XDGStateHome = ""
	}
	options.MaxSkills = maxSkills
	options.MaxWork = maxWork
	return options, nil
}

func inventoryRoots(options InventoryOptions, projectLock, globalLock map[string]lockEntry) []inventoryRoot {
	roots := make([]inventoryRoot, 0, 9)
	if options.CWD != "" {
		roots = append(roots,
			inventoryRoot{
				path:       filepath.Join(options.CWD, ".agents", "skills"),
				label:      "shared project Skills directory",
				scope:      ScopeProject,
				agents:     []Agent{AgentCodex, AgentCursor},
				manager:    ManagerUnknown,
				provenance: "shared project Skills directory",
				lock:       projectLock,
			},
			inventoryRoot{
				path:       filepath.Join(options.CWD, ".claude", "skills"),
				label:      "Claude Code project Skills directory",
				scope:      ScopeProject,
				agents:     []Agent{AgentClaudeCode},
				manager:    ManagerUnknown,
				provenance: "Claude Code project Skills directory",
				lock:       projectLock,
			},
		)
	}
	roots = append(roots,
		inventoryRoot{
			path:       filepath.Join(options.Home, ".agents", "skills"),
			label:      "skills-cli global canonical store",
			scope:      ScopeGlobal,
			agents:     nil,
			manager:    ManagerUnknown,
			provenance: "skills-cli global canonical store",
			lock:       globalLock,
		},
		inventoryRoot{
			path:       filepath.Join(options.CodexHome, "skills"),
			label:      "Codex global Skills directory",
			scope:      ScopeGlobal,
			agents:     []Agent{AgentCodex},
			manager:    ManagerUnknown,
			provenance: "Codex global Skills directory",
			lock:       globalLock,
		},
		inventoryRoot{
			path:       filepath.Join(options.ClaudeHome, "skills"),
			label:      "Claude Code global Skills directory",
			scope:      ScopeGlobal,
			agents:     []Agent{AgentClaudeCode},
			manager:    ManagerUnknown,
			provenance: "Claude Code global Skills directory",
			lock:       globalLock,
		},
		inventoryRoot{
			path:       filepath.Join(options.Home, ".cursor", "skills"),
			label:      "Cursor global Skills directory",
			scope:      ScopeGlobal,
			agents:     []Agent{AgentCursor},
			manager:    ManagerUnknown,
			provenance: "Cursor global Skills directory",
			lock:       globalLock,
		},
		inventoryRoot{
			path:       filepath.Join(options.CodexHome, "skills", ".system"),
			label:      "Codex builtin Skills directory",
			scope:      ScopeBuiltin,
			agents:     []Agent{AgentCodex},
			manager:    ManagerBuiltin,
			provenance: "Codex builtin",
		},
	)
	return roots
}

func readOfficialLock(path string, minimumVersion int) (map[string]lockEntry, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, !errors.Is(err, fs.ErrNotExist)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxLockFileBytes+1))
	if err != nil || len(data) > maxLockFileBytes {
		return nil, true
	}
	var parsed lockFile
	if json.Unmarshal(data, &parsed) != nil || parsed.Version < minimumVersion || parsed.Skills == nil || len(parsed.Skills) > defaultMaxInstalledSkills {
		return nil, true
	}
	valid := make(map[string]lockEntry, len(parsed.Skills))
	invalid := false
	for name, entry := range parsed.Skills {
		if ValidateSkillName(name) != nil || !validLockEntry(entry) {
			invalid = true
			continue
		}
		valid[name] = entry
	}
	return valid, invalid
}

func validLockEntry(entry lockEntry) bool {
	if !safeLiteral(entry.SourceType, 32) || !safeDisplayLiteral(entry.Source, 512) || !safeDisplayLiteral(entry.SourceURL, 1024) {
		return false
	}
	if entry.SkillPath != "" && (!safeDisplayLiteral(entry.SkillPath, 512) || filepath.IsAbs(entry.SkillPath) || hasParentTraversal(entry.SkillPath)) {
		return false
	}
	return safeDisplayLiteral(entry.PluginName, 128)
}

func (collector *inventoryCollector) scanRoot(root inventoryRoot) {
	if !collector.consumeWork() {
		return
	}
	directory, err := os.Open(root.path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			collector.markIncomplete()
			collector.warn(fmt.Sprintf("Could not read the %s.", root.label))
		}
		return
	}
	defer directory.Close()
	for {
		if !collector.consumeWork() {
			return
		}
		entries, readErr := directory.ReadDir(64)
		if !collector.checkContext() {
			return
		}
		for _, entry := range entries {
			if !collector.consumeWork() {
				return
			}
			if collector.count >= collector.options.MaxSkills {
				collector.stopIncomplete()
				collector.warn("Installed Skills inventory reached its bounded result limit.")
				return
			}
			collector.scanRootEntry(root, entry)
			if collector.count >= collector.options.MaxSkills {
				collector.stopIncomplete()
				collector.warn("Installed Skills inventory reached its bounded result limit.")
				return
			}
		}
		if errors.Is(readErr, io.EOF) {
			return
		}
		if readErr != nil {
			collector.markIncomplete()
			collector.warn(fmt.Sprintf("Could not finish reading the %s.", root.label))
			return
		}
	}
}

func (collector *inventoryCollector) scanRootEntry(root inventoryRoot, entry fs.DirEntry) {
	if strings.HasPrefix(entry.Name(), ".") {
		return
	}
	sourcePath := filepath.Join(root.path, entry.Name())
	isDirectory, directoryErr := isSkillDirectory(entry, sourcePath)
	if directoryErr != nil {
		collector.markIncomplete()
		collector.warn(fmt.Sprintf("Could not inspect a Skill binding in the %s.", root.label))
		return
	}
	if !isDirectory {
		return
	}
	metadataPath := filepath.Join(sourcePath, "SKILL.md")
	frontmatter, ok, metadataErr := readSkillFrontmatter(metadataPath)
	if metadataErr != nil {
		collector.markIncomplete()
		collector.warn(fmt.Sprintf("Could not read or validate Skill metadata in the %s.", root.label))
	}
	if !ok {
		return
	}
	name := cleanMetadata(entry.Name(), maxSkillNameLength)
	if name == "" {
		return
	}
	frontmatterName := cleanMetadata(frontmatter.Name, maxSkillNameLength)
	identityMismatch := frontmatterName != "" && frontmatterName != name
	realPath, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		collector.markIncomplete()
		collector.warn(fmt.Sprintf("Could not resolve a Skill binding in the %s.", root.label))
		return
	}
	realPath, err = filepath.Abs(realPath)
	if err != nil {
		collector.markIncomplete()
		collector.warn(fmt.Sprintf("Could not resolve a Skill binding in the %s.", root.label))
		return
	}
	realPath = filepath.Clean(realPath)
	binding := SkillBinding{SourcePath: filepath.Clean(sourcePath), Scope: root.scope, Agents: append([]Agent{}, root.agents...)}
	if existing := collector.byReal[realPath]; existing != nil {
		appendBinding(&existing.Bindings, binding)
		mergeAgents(&existing.Agents, root.agents)
		if identityMismatch {
			collector.blockManagement(existing, "Skill metadata does not match its installed directory identity.")
		}
		collector.promoteOwnership(existing, root, name)
		return
	}

	skill := &InstalledSkill{
		ID:            installedSkillID(realPath),
		Name:          name,
		Description:   cleanMetadata(frontmatter.Description, 240),
		CanonicalPath: realPath,
		SourcePath:    filepath.Clean(sourcePath),
		Scope:         root.scope,
		Agents:        append([]Agent{}, root.agents...),
		Bindings:      []SkillBinding{binding},
		Manager:       root.manager,
		Provenance:    root.provenance,
		Plugin:        cleanMetadata(root.plugin, 128),
		Capability: ManagementCapability{
			Reason: unmanagedReason(root.manager),
		},
	}
	if identityMismatch {
		collector.blockManagement(skill, "Skill metadata does not match its installed directory identity.")
	} else {
		collector.applyLock(skill, root.lock, name)
	}
	collector.byReal[realPath] = skill
	collector.count++
}

func (collector *inventoryCollector) promoteOwnership(skill *InstalledSkill, root inventoryRoot, directoryName string) {
	if skill.Name != directoryName {
		collector.blockManagement(skill, "One canonical Skill path is installed under multiple directory identities.")
		return
	}
	if skill.Scope != root.scope {
		skill.Scope = ScopeMixed
		collector.blockManagement(skill, "This canonical Skill has bindings in multiple scopes, so no exact removal command is provable.")
		return
	}
	if root.scope == ScopeBuiltin || root.scope == ScopePlugin {
		if skill.Manager != root.manager {
			collector.blockManagement(skill, "This canonical Skill has conflicting management owners.")
		}
		return
	}
	collector.applyLock(skill, root.lock, directoryName)
}

func (collector *inventoryCollector) applyLock(skill *InstalledSkill, entries map[string]lockEntry, directoryName string) {
	if len(entries) == 0 || skill.Manager == ManagerBuiltin || skill.Manager == ManagerPlugin || collector.blocked[skill.CanonicalPath] != "" {
		return
	}
	entry, found := entries[directoryName]
	if !found || skill.Name != directoryName || ValidateSkillName(directoryName) != nil {
		return
	}
	if plugin := cleanMetadata(entry.PluginName, 128); plugin != "" {
		skill.Scope = ScopePlugin
		skill.Manager = ManagerPlugin
		skill.Plugin = plugin
		skill.Provenance = "skills-cli plugin provenance"
		skill.Capability = ManagementCapability{Reason: "Plugin-owned Skills must be managed by their plugin owner."}
		return
	}
	if !lockEntryMatchesDirectory(entry, directoryName) {
		collector.blockManagement(skill, "The official lock source does not match this installed directory identity.")
		return
	}
	skill.Manager = ManagerSkillsCLI
	skill.Provenance = "official skills-cli lock"
	skill.SourceType = entry.SourceType
	if ValidateRepository(entry.Source) == nil {
		skill.Source = entry.Source
	}
	skill.Capability = ManagementCapability{
		CanRemove: true,
	}
}

func lockEntryMatchesDirectory(entry lockEntry, directoryName string) bool {
	if entry.SkillPath != "" {
		clean := filepath.Clean(filepath.FromSlash(entry.SkillPath))
		if filepath.Base(clean) != "SKILL.md" || filepath.Base(filepath.Dir(clean)) != directoryName {
			return false
		}
	}
	if entry.SourceType != "github" {
		return true
	}
	if ValidateRepository(entry.Source) != nil {
		return false
	}
	if entry.SourceURL == "" || entry.SourceURL == entry.Source {
		return true
	}
	parsed, err := url.Parse(entry.SourceURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return false
	}
	repository := parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")
	return repository == entry.Source && (len(parts) == 2 || (len(parts) > 3 && parts[2] == "tree"))
}

func (collector *inventoryCollector) scanPluginCaches() {
	collector.scanPluginCache(filepath.Join(collector.options.CodexHome, "plugins", "cache"), AgentCodex, "Codex plugin")
	if !collector.stopped {
		collector.scanPluginCache(filepath.Join(collector.options.ClaudeHome, "plugins", "cache"), AgentClaudeCode, "Claude Code plugin")
	}
}

func (collector *inventoryCollector) scanPluginCache(cachePath string, agent Agent, provenance string) {
	visited := 0
	roots := 0
	type pendingDirectory struct {
		path  string
		depth int
	}
	pending := []pendingDirectory{{path: cachePath}}
	for len(pending) > 0 {
		if !collector.consumeWork() {
			return
		}
		if visited >= maxPluginWalkEntries || roots >= maxPluginRoots || collector.count >= collector.options.MaxSkills {
			collector.stopIncomplete()
			collector.warn("Installed Skills inventory stopped at its bounded plugin traversal limit.")
			return
		}
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		directory, err := os.Open(current.path)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) || current.depth > 0 {
				collector.markIncomplete()
				collector.warn(fmt.Sprintf("Could not read the %s cache.", provenance))
			}
			continue
		}
		for {
			if !collector.consumeWork() {
				_ = directory.Close()
				return
			}
			entries, readErr := directory.ReadDir(64)
			if !collector.checkContext() {
				_ = directory.Close()
				return
			}
			for _, entry := range entries {
				if !collector.consumeWork() {
					_ = directory.Close()
					return
				}
				if visited >= maxPluginWalkEntries || roots >= maxPluginRoots || collector.count >= collector.options.MaxSkills {
					collector.stopIncomplete()
					collector.warn("Installed Skills inventory stopped at its bounded plugin traversal limit.")
					_ = directory.Close()
					return
				}
				visited++
				if !entry.IsDir() {
					if entry.Type()&fs.ModeSymlink != 0 {
						collector.markIncomplete()
						collector.warn("Installed Skills inventory skipped a symbolic link in a plugin cache.")
					}
					continue
				}
				path := filepath.Join(current.path, entry.Name())
				nextDepth := current.depth + 1
				if entry.Name() == "skills" {
					if nextDepth > 7 {
						collector.markIncomplete()
						collector.warn("Installed Skills inventory skipped an over-depth plugin cache directory.")
						continue
					}
					roots++
					collector.scanRoot(inventoryRoot{
						path:       path,
						label:      provenance + " cache",
						scope:      ScopePlugin,
						agents:     []Agent{agent},
						manager:    ManagerPlugin,
						provenance: provenance,
						plugin:     pluginNameForRoot(path),
					})
					if collector.stopped {
						_ = directory.Close()
						return
					}
					continue
				}
				if nextDepth >= 7 {
					collector.markIncomplete()
					collector.warn("Installed Skills inventory skipped an over-depth plugin cache directory.")
					continue
				}
				pending = append(pending, pendingDirectory{path: path, depth: nextDepth})
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				collector.markIncomplete()
				collector.warn(fmt.Sprintf("Could not finish reading the %s cache.", provenance))
				break
			}
		}
		_ = directory.Close()
	}
}

func isSkillDirectory(entry fs.DirEntry, path string) (bool, error) {
	if entry.IsDir() {
		return true, nil
	}
	if entry.Type()&fs.ModeSymlink == 0 {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func readSkillFrontmatter(path string) (skillFrontmatter, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return skillFrontmatter{}, false, nil
		}
		return skillFrontmatter{}, false, err
	}
	defer file.Close()
	first, consumed, ok := readMetadataLine(file, maxSkillFrontmatterBytes)
	if !ok || strings.TrimSuffix(strings.TrimPrefix(first, "\ufeff"), "\r") != "---" {
		return skillFrontmatter{}, false, errors.New("invalid Skill frontmatter")
	}
	var frontmatter strings.Builder
	for consumed < maxSkillFrontmatterBytes {
		line, read, lineOK := readMetadataLine(file, maxSkillFrontmatterBytes-consumed)
		consumed += read
		if !lineOK {
			return skillFrontmatter{}, false, errors.New("unterminated Skill frontmatter")
		}
		if strings.TrimSuffix(line, "\r") == "---" {
			value := frontmatter.String()
			if !utf8.ValidString(value) {
				return skillFrontmatter{}, false, errors.New("invalid Skill frontmatter encoding")
			}
			var parsed skillFrontmatter
			if yaml.Unmarshal([]byte(value), &parsed) != nil {
				return skillFrontmatter{}, false, errors.New("invalid Skill frontmatter YAML")
			}
			return parsed, true, nil
		}
		frontmatter.WriteString(line)
		frontmatter.WriteByte('\n')
	}
	return skillFrontmatter{}, false, errors.New("Skill frontmatter exceeded its size limit")
}

// readMetadataLine deliberately reads one byte at a time so discovery never
// buffers any Skill body bytes beyond the closing frontmatter fence.
func readMetadataLine(reader io.Reader, remaining int) (string, int, bool) {
	if remaining <= 0 {
		return "", 0, false
	}
	line := make([]byte, 0, 128)
	var current [1]byte
	for len(line) < remaining {
		read, err := reader.Read(current[:])
		if read == 1 {
			if current[0] == '\n' {
				return string(line), len(line) + 1, true
			}
			line = append(line, current[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				return string(line), len(line), true
			}
			return "", len(line), false
		}
	}
	return "", len(line), false
}

func installedSkillID(realPath string) string {
	sum := sha256.Sum256([]byte(realPath))
	return hex.EncodeToString(sum[:12])
}

func cleanMetadata(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return strings.TrimSpace(string(runes))
}

func safeLiteral(value string, limit int) bool {
	if value == "" || len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	return true
}

func safeDisplayLiteral(value string, limit int) bool {
	if len(value) > limit || !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func hasParentTraversal(path string) bool {
	for _, part := range strings.FieldsFunc(filepath.ToSlash(path), func(r rune) bool { return r == '/' }) {
		if part == ".." {
			return true
		}
	}
	return false
}

func mergeAgents(target *[]Agent, additions []Agent) {
	for _, agent := range additions {
		found := false
		for _, current := range *target {
			if current == agent {
				found = true
				break
			}
		}
		if !found {
			*target = append(*target, agent)
		}
	}
}

func appendBinding(target *[]SkillBinding, binding SkillBinding) {
	for _, current := range *target {
		if current.SourcePath == binding.SourcePath && current.Scope == binding.Scope {
			return
		}
	}
	*target = append(*target, binding)
}

func sortAgents(agents []Agent) {
	rank := map[Agent]int{AgentCodex: 0, AgentClaudeCode: 1, AgentCursor: 2, AgentGrok: 3}
	sort.Slice(agents, func(i, j int) bool { return rank[agents[i]] < rank[agents[j]] })
}

func scopeRank(scope Scope) int {
	switch scope {
	case ScopeProject:
		return 0
	case ScopeGlobal:
		return 1
	case ScopeMixed:
		return 2
	case ScopePlugin:
		return 3
	case ScopeBuiltin:
		return 4
	default:
		return 4
	}
}

func unmanagedReason(manager Manager) string {
	switch manager {
	case ManagerBuiltin:
		return "Builtin Skills are managed by their provider."
	case ManagerPlugin:
		return "Plugin-owned Skills must be managed by their plugin owner."
	default:
		return "No official skills-cli provenance proves a safe management command."
	}
}

// finalizeRemovalPlans makes the CLI's actual binding asymmetry explicit.
// Codex and Cursor resolve project Skills through the shared .agents/skills
// canonical store, so targeting either one cannot prove an isolated removal.
// Claude Code can be detached from an exact project symlink without deleting
// that shared store. Global partial removal is deliberately not granted because
// skills-cli removes the one shared global lock entry while bindings remain.
func finalizeRemovalPlans(skill *InstalledSkill) {
	skill.Capability.RemovalPlans = nil
	if !skill.Capability.CanRemove {
		return
	}
	if len(skill.Agents) == 0 {
		skill.Capability = ManagementCapability{Reason: "The installed Skill has no supported Agent binding."}
		return
	}

	allAgents := append([]Agent{}, skill.Agents...)
	for _, agent := range skill.Agents {
		affected := append([]Agent{}, allAgents...)
		if len(allAgents) == 1 || hasDetachableProjectBinding(*skill, agent) {
			affected = []Agent{agent}
		}
		skill.Capability.RemovalPlans = append(skill.Capability.RemovalPlans, AgentRemovalPlan{
			Agent:          agent,
			AffectedAgents: affected,
		})
	}
}

func hasDetachableProjectBinding(skill InstalledSkill, agent Agent) bool {
	if skill.Scope != ScopeProject || agent != AgentClaudeCode {
		return false
	}
	found := false
	for _, binding := range skill.Bindings {
		containsAgent := false
		for _, boundAgent := range binding.Agents {
			if boundAgent == agent {
				containsAgent = true
				break
			}
		}
		if !containsAgent {
			continue
		}
		if binding.Scope != ScopeProject || len(binding.Agents) != 1 || binding.SourcePath == skill.CanonicalPath {
			return false
		}
		found = true
	}
	return found
}

func pluginNameForRoot(root string) string {
	versionDir := filepath.Dir(root)
	pluginDir := filepath.Dir(versionDir)
	return cleanMetadata(filepath.Base(pluginDir), 128)
}

func (collector *inventoryCollector) warn(message string) {
	if len(collector.warnings) >= maxInventoryWarnings {
		return
	}
	if _, exists := collector.warned[message]; exists {
		return
	}
	collector.warned[message] = struct{}{}
	collector.warnings = append(collector.warnings, message)
}

func (collector *inventoryCollector) checkContext() bool {
	if collector.stopped {
		return false
	}
	if collector.options.Context != nil && collector.options.Context.Err() != nil {
		collector.stopIncomplete()
		collector.warn("Installed Skills inventory traversal was canceled.")
		return false
	}
	return true
}

func (collector *inventoryCollector) consumeWork() bool {
	if !collector.checkContext() {
		return false
	}
	if collector.work >= collector.options.MaxWork {
		collector.stopIncomplete()
		collector.warn("Installed Skills inventory reached its bounded total-work limit.")
		return false
	}
	collector.work++
	return collector.checkContext()
}

func (collector *inventoryCollector) markIncomplete() {
	if collector.incomplete {
		return
	}
	collector.incomplete = true
	collector.warn("Installed Skills inventory is incomplete; removal is disabled for this snapshot.")
}

func (collector *inventoryCollector) stopIncomplete() {
	collector.markIncomplete()
	collector.stopped = true
}

func (collector *inventoryCollector) disableAllMutationAuthority() {
	const reason = "Removal is disabled because this inventory snapshot is incomplete."
	for _, skill := range collector.byReal {
		if skill.Manager == ManagerSkillsCLI {
			collector.blockManagement(skill, reason)
			continue
		}
		skill.Capability = ManagementCapability{Reason: reason}
	}
}

func (collector *inventoryCollector) blockManagement(skill *InstalledSkill, reason string) {
	collector.blocked[skill.CanonicalPath] = reason
	skill.Manager = ManagerUnknown
	skill.Provenance = "ambiguous installed binding"
	skill.Source = ""
	skill.SourceType = ""
	skill.Plugin = ""
	skill.Capability = ManagementCapability{Reason: reason}
}

func (collector *inventoryCollector) rejectDuplicateIdentities() {
	groups := make(map[string][]*InstalledSkill)
	for _, skill := range collector.byReal {
		if skill.Scope == ScopeMixed {
			continue
		}
		key := string(skill.Scope) + "\x00" + skill.Name
		groups[key] = append(groups[key], skill)
	}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		for _, skill := range group {
			collector.blockManagement(skill, "Multiple installed directories share this Skill identity, so removal is ambiguous.")
		}
	}
}
