package skills

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	maxInventoryWarnings      = 12
	maxPluginWalkEntries      = 2500
	maxPluginRoots            = 48
	maxDiscoveryHashBytes     = 4 << 20
)

// InventoryOptions are the resolved inputs for discovery, migration, and
// lifecycle operations. Home/StateDir can point at fixtures so tests never
// touch the real user installation; Env overrides adapter environment.
type InventoryOptions struct {
	Context      context.Context
	CWD          string
	Home         string
	ZenStateDir  string
	CodexHome    string
	ClaudeHome   string
	XDGStateHome string
	Env          map[string]string
	Executors    []ExecutorAlias
	MaxSkills    int
	MaxWork      int
	Now          func() time.Time
	Fetcher      SourceFetcher
}

type inventoryRoot struct {
	path       string
	label      string
	scope      Scope
	agents     []Agent
	manager    Manager
	provenance string
	plugin     string
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type inventoryCollector struct {
	options    InventoryOptions
	byReal     map[string]*InstalledSkill
	byName     map[string][]*InstalledSkill
	blocked    map[string]string
	warnings   []string
	warned     map[string]struct{}
	count      int
	work       int
	incomplete bool
	stopped    bool
}

// DiscoverInventory builds the authoritative single Skills list: Zen-owned
// packages, tracked external installations, and untracked external skills
// discovered across the six Agent surfaces. Discovery never mutates disk.
func DiscoverInventory(options InventoryOptions) (Inventory, error) {
	normalized, err := normalizeInventoryOptions(options)
	if err != nil {
		return Inventory{}, err
	}
	store := Store{StateDir: normalized.ZenStateDir, Home: normalized.Home, Now: normalized.Now}
	collector := &inventoryCollector{
		options: normalized,
		byReal:  make(map[string]*InstalledSkill),
		byName:  make(map[string][]*InstalledSkill),
		blocked: make(map[string]string),
		warned:  make(map[string]struct{}),
	}
	collector.loadZenInventory(store)
	if !collector.stopped {
		collector.scanBuiltinSurfaces(store)
	}
	if !collector.stopped {
		collector.scanExternalSurfaces(store)
	}
	if !collector.stopped {
		collector.scanPluginCaches()
	}
	if err := normalized.Context.Err(); err != nil {
		collector.stopIncomplete()
		collector.warn("Installed Skills inventory traversal was canceled.")
	} else if collector.work >= normalized.MaxWork {
		collector.stopIncomplete()
		collector.warn("Installed Skills inventory reached its bounded total-work limit.")
	}
	collector.classifyConflicts()
	if collector.incomplete {
		collector.disableAllMutationAuthority()
	}

	installed := collector.rows()
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
		GeneratedAt:        now().UTC(),
		CWD:                normalized.CWD,
		Skills:             installed,
		Agents:             AgentSupportEntries(),
		Executors:          resolveExecutors(normalized.Executors),
		Warnings:           collector.warnings,
		MutationOperations: SupportedMutationOperations(),
		Migration:          collector.migrationStatus(),
		incomplete:         collector.incomplete,
	}
	return inventory, normalized.Context.Err()
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
	zenStateDir := strings.TrimSpace(options.ZenStateDir)
	if zenStateDir == "" {
		zenStateDir = strings.TrimSpace(os.Getenv("ZEN_STATE_DIR"))
	}
	if zenStateDir == "" {
		zenStateDir = filepath.Join(home, ".zen")
	}
	for _, value := range []string{codexHome, claudeHome} {
		if !filepath.IsAbs(value) {
			return InventoryOptions{}, errors.New("agent home directory must be absolute")
		}
	}
	if zenStateDir != "" && !filepath.IsAbs(zenStateDir) {
		return InventoryOptions{}, errors.New("Zen state directory must be absolute")
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
	options.ZenStateDir = filepath.Clean(zenStateDir)
	if xdgStateHome == "" {
		options.XDGStateHome = ""
	} else {
		options.XDGStateHome = filepath.Clean(xdgStateHome)
	}
	options.MaxSkills = maxSkills
	options.MaxWork = maxWork
	return options, nil
}

// ---------------------------------------------------------------------------
// Zen-owned + tracked rows
// ---------------------------------------------------------------------------

func (collector *inventoryCollector) loadZenInventory(store Store) {
	file, err := store.LoadInventory(false)
	if err != nil {
		collector.markIncomplete()
		collector.warn("The Zen Skills inventory could not be read; management is disabled for this snapshot.")
		return
	}
	for _, warning := range file.Warnings {
		collector.warn(warning)
	}
	names := make([]string, 0, len(file.Packages))
	for name := range file.Packages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !collector.consumeWork() {
			return
		}
		entry := file.Packages[name]
		if entry.Owned {
			collector.addOwnedRow(store, entry)
		} else {
			collector.addTrackedExternalRow(store, entry)
		}
		if collector.count >= collector.options.MaxSkills {
			collector.stopIncomplete()
			collector.warn("Installed Skills inventory reached its bounded result limit.")
			return
		}
	}
}

func (collector *inventoryCollector) addOwnedRow(store Store, entry PackageEntry) {
	packageDir := store.PackageDir(entry.SkillName)
	info, err := os.Stat(packageDir)
	if err != nil || !info.IsDir() {
		collector.warn(fmt.Sprintf("Zen-owned Skill %q is missing from the store.", entry.SkillName))
		return
	}
	hash, err := folderContentHash(packageDir)
	if err != nil {
		collector.warn(fmt.Sprintf("Zen-owned Skill %q could not be hashed: %v", entry.SkillName, err))
	}
	drifted := hash != "" && entry.ContentHash != "" && hash != entry.ContentHash
	skill := &InstalledSkill{
		ID:            installedSkillID(packageDir),
		Name:          entry.SkillName,
		Description:   entry.Description,
		Manager:       ManagerZen,
		Owned:         true,
		Tracked:       true,
		Enabled:       entry.AnyEnabled(),
		CanonicalPath: packageDir,
		SourcePath:    packageDir,
		Scope:         entry.Scope(),
		Agents:        entryBindingAgents(entry),
		Provenance:    "Zen canonical store",
		Source:        entry.Source,
		SourceType:    entry.SourceType,
		SourceURL:     entry.SourceURL,
		Ref:           entry.Ref,
		ContentHash:   entry.ContentHash,
		InstalledAt:   entry.InstalledAt,
		UpdatedAt:     entry.UpdatedAt,
		Capability: ManagementCapability{
			CanManage:  true,
			Operations: ownedPackageOperations(entry),
		},
	}
	if drifted {
		skill.Warnings = append(skill.Warnings, "Store content hash differs from the inventory record; update or reinstall before trusting state.")
		collector.warn(fmt.Sprintf("Zen-owned Skill %q drifted from its recorded content hash.", entry.SkillName))
	}
	for _, binding := range entry.Bindings {
		extended := SkillBinding{
			Agent: binding.Agent, Scope: binding.Scope, Mode: string(binding.Mode),
			TargetPath: binding.TargetPath, SourcePath: binding.TargetPath,
			Enabled: binding.Enabled, BoundAt: binding.BoundAt, Note: binding.Note,
			Operations: bindingOperations(binding),
		}
		if binding.Mode == BindingCopy && binding.Enabled {
			if driftHash := copyBindingDriftHash(binding, hash); driftHash != "" {
				extended.DriftHash = driftHash
				if driftHash == "drifted" {
					extended.Note = "Materialized copy has drifted from the store; re-enable to refresh."
					skill.Warnings = append(skill.Warnings, "A materialized copy binding has drifted from the store content; re-enable or rebind to refresh it.")
				}
			}
		}
		skill.Bindings = append(skill.Bindings, extended)
	}
	collector.add(skill)
}

func bindingOperations(binding BindingEntry) []MutationOperation {
	operations := []MutationOperation{OperationUnbind}
	if binding.Enabled {
		return append(operations, OperationDisable)
	}
	return append(operations, OperationEnable)
}

func ownedPackageOperations(entry PackageEntry) []MutationOperation {
	operations := []MutationOperation{OperationUninstall}
	if len(entry.Bindings) < len(Adapters)*2 {
		operations = append(operations, OperationBind)
	}
	if updateProvenancePinned(entry) {
		operations = append(operations, OperationUpdate)
	}
	return operations
}

func updateProvenancePinned(entry PackageEntry) bool {
	switch SourceType(entry.SourceType) {
	case SourceTypeCatalog, SourceTypeGithub:
		return entry.Ref != ""
	case SourceTypeLocal, SourceTypeArchive:
		return filepath.IsAbs(entry.Source) && entry.ContentHash != ""
	default:
		return false
	}
}

func (collector *inventoryCollector) addTrackedExternalRow(store Store, entry PackageEntry) {
	dir, err := trackedExternalDir(entry)
	if err != nil {
		collector.warn(fmt.Sprintf("Tracked external Skill %q is no longer available: %v", entry.SkillName, err))
		skill := &InstalledSkill{
			ID:            installedSkillID(entry.Source + "\x00" + entry.SkillName),
			Name:          entry.SkillName,
			Manager:       ManagerExternal,
			Owned:         false,
			Tracked:       true,
			CanonicalPath: entry.Source,
			SourcePath:    entry.Source,
			Provenance:    "Tracked external installation",
			Source:        entry.Source,
			SourceType:    entry.SourceType,
			ContentHash:   entry.ContentHash,
			Migration:     "external",
			Capability: ManagementCapability{
				CanManage:  true,
				Operations: []MutationOperation{OperationForget},
				Reason:     err.Error(),
			},
		}
		collector.add(skill)
		return
	}
	skill := &InstalledSkill{
		ID:            installedSkillID(dir),
		Name:          entry.SkillName,
		Description:   entry.Description,
		Manager:       ManagerExternal,
		Owned:         false,
		Tracked:       true,
		Enabled:       true,
		CanonicalPath: dir,
		SourcePath:    dir,
		Scope:         entry.DiscoveredScope,
		Agents:        append([]Agent{}, entry.DiscoveredAgents...),
		Provenance:    "Tracked external installation",
		Source:        entry.Source,
		SourceType:    entry.SourceType,
		ContentHash:   entry.ContentHash,
		InstalledAt:   entry.InstalledAt,
		UpdatedAt:     entry.UpdatedAt,
		Migration:     "external",
		Risk:          scanRiskSignals(dir),
		Capability: ManagementCapability{
			CanManage:  true,
			Operations: []MutationOperation{OperationAdopt, OperationForget},
		},
	}
	collector.add(skill)
}

func copyBindingDriftHash(binding BindingEntry, expectedHash string) string {
	if binding.TargetPath == "" {
		return binding.TargetPath
	}
	hash, err := folderContentHash(binding.TargetPath)
	if err != nil {
		return ""
	}
	if expectedHash != "" && hash != expectedHash {
		return "drifted"
	}
	return hash
}

// ---------------------------------------------------------------------------
// External surface discovery
// ---------------------------------------------------------------------------

func (collector *inventoryCollector) scanBuiltinSurfaces(store Store) {
	collector.scanRoot(inventoryRoot{
		path:       filepath.Join(collector.options.CodexHome, "skills", ".system"),
		label:      "Codex builtin Skills directory",
		scope:      ScopeBuiltin,
		agents:     []Agent{AgentCodex},
		manager:    ManagerBuiltin,
		provenance: "Codex builtin Skills directory",
	}, store)
}

func (collector *inventoryCollector) scanExternalSurfaces(store Store) {
	for _, agent := range []Agent{AgentCodex, AgentClaudeCode, AgentCursor, AgentGrok, AgentOpenCode, AgentPi} {
		adapter, err := adapterFor(agent)
		if err != nil {
			continue
		}
		if !collector.consumeWork() {
			return
		}
		collector.scanRoot(inventoryRoot{
			path:       globalSkillsDir(adapter, collector.options.Home, envResolverFor(collector.options)),
			label:      adapter.Name + " global Skills directory",
			scope:      ScopeGlobal,
			agents:     []Agent{agent},
			manager:    ManagerExternal,
			provenance: adapter.Name + " global Skills directory",
		}, store)
		if collector.stopped {
			return
		}
		if collector.options.CWD != "" {
			collector.scanRoot(inventoryRoot{
				path:       projectSkillsDir(adapter, collector.options.CWD),
				label:      adapter.Name + " project Skills directory",
				scope:      ScopeProject,
				agents:     []Agent{agent},
				manager:    ManagerExternal,
				provenance: adapter.Name + " project Skills directory",
			}, store)
			if collector.stopped {
				return
			}
		}
	}
}

func (collector *inventoryCollector) scanRoot(root inventoryRoot, store Store) {
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
			collector.scanRootEntry(root, entry, store)
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

func (collector *inventoryCollector) scanRootEntry(root inventoryRoot, entry fs.DirEntry, store Store) {
	if strings.HasPrefix(entry.Name(), ".") {
		return
	}
	sourcePath := filepath.Join(root.path, entry.Name())
	isDirectory, directoryErr := isSkillDirectory(entry, sourcePath)
	if directoryErr != nil {
		collector.warn(fmt.Sprintf("Could not inspect a Skill binding in the %s.", root.label))
		return
	}
	if !isDirectory {
		return
	}
	if collector.isManagedTarget(sourcePath, store) {
		return
	}
	metadataPath := filepath.Join(sourcePath, "SKILL.md")
	frontmatter, ok, metadataErr := readSkillFrontmatter(metadataPath)
	if metadataErr != nil {
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
		collector.warn(fmt.Sprintf("Could not resolve a Skill binding in the %s.", root.label))
		return
	}
	realPath, err = filepath.Abs(realPath)
	if err != nil {
		collector.warn(fmt.Sprintf("Could not resolve a Skill binding in the %s.", root.label))
		return
	}
	realPath = filepath.Clean(realPath)

	hash := ""
	if hashValue, hashErr := boundedDiscoveryHash(sourcePath); hashErr == nil {
		hash = hashValue
	}
	bindings := make([]SkillBinding, 0, len(root.agents))
	for _, agent := range root.agents {
		bindings = append(bindings, SkillBinding{
			Agent:      agent,
			Scope:      root.scope,
			Mode:       string(BindingSymlink),
			TargetPath: filepath.Clean(sourcePath),
			SourcePath: filepath.Clean(sourcePath),
			Enabled:    true,
		})
	}
	if existing := collector.byReal[realPath]; existing != nil {
		for _, binding := range bindings {
			mergeInstruction(existing, root, binding)
		}
		if identityMismatch {
			collector.blockManagement(existing, "Skill metadata does not match its installed directory identity.")
		}
		return
	}
	skill := &InstalledSkill{
		ID:            installedSkillID(realPath),
		Name:          name,
		Description:   cleanMetadata(frontmatter.Description, 240),
		Manager:       root.manager,
		Owned:         false,
		Tracked:       false,
		Enabled:       true,
		CanonicalPath: realPath,
		SourcePath:    filepath.Clean(sourcePath),
		Scope:         root.scope,
		Agents:        append([]Agent{}, root.agents...),
		SourceType:    string(SourceTypeExternal),
		ContentHash:   hash,
		Migration:     "external",
		Risk:          scanRiskSignals(sourcePath),
		Capability: ManagementCapability{
			CanManage:  true,
			Operations: []MutationOperation{OperationAdopt, OperationMigrate},
		},
	}
	if root.manager == ManagerPlugin {
		skill.Scope = ScopePlugin
		skill.Plugin = cleanMetadata(root.plugin, 128)
		skill.Provenance = root.provenance
		skill.Provenance += ":" + skill.Plugin
		skill.Capability = ManagementCapability{Reason: "Plugin-owned Skills must be managed by their plugin owner."}
	} else if root.manager == ManagerBuiltin {
		skill.Provenance = root.provenance
		skill.Capability = ManagementCapability{Reason: "Builtin Skills are managed by their provider."}
	} else {
		skill.Provenance = root.provenance
	}
	if identityMismatch {
		collector.blockManagement(skill, "Skill metadata does not match its installed directory identity.")
	}
	skill.Bindings = bindings
	collector.byReal[realPath] = skill
	collector.byName[name] = append(collector.byName[name], skill)
	collector.count++
}

// isManagedTarget returns true when the on-disk directory is a Zen-owned
// binding materialization (or a symlink into the store), so discovery never
// reports managed content as an external copy.
func (collector *inventoryCollector) isManagedTarget(sourcePath string, store Store) bool {
	if store.StateDir == "" {
		return false
	}
	resolved, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return false
	}
	storesDir := store.StoreDir()
	info, err := os.Stat(storesDir)
	if err == nil && info.IsDir() {
		storesDir, _ = filepath.Abs(storesDir)
		if strings.HasPrefix(filepath.Clean(resolved), storesDir+string(filepath.Separator)) {
			return true
		}
	}
	for _, skill := range collector.byReal {
		if skill.Owned && skill.SourceType == string(SourceTypeExternal) &&
			filepath.Clean(sourcePath) == filepath.Clean(skill.Source) {
			// Adoption intentionally preserves this external origin. It remains
			// inspectable as package provenance and must not become a duplicate row.
			return true
		}
		for _, binding := range skill.Bindings {
			if filepath.Clean(sourcePath) == filepath.Clean(binding.TargetPath) {
				return true
			}
		}
	}
	return false
}

func mergeInstruction(skill *InstalledSkill, root inventoryRoot, binding SkillBinding) {
	mergeAgents(&skill.Agents, root.agents)
	for index := range skill.Bindings {
		if skill.Bindings[index].Agent == binding.Agent &&
			skill.Bindings[index].TargetPath == binding.TargetPath &&
			skill.Bindings[index].Scope == binding.Scope {
			return
		}
	}
	skill.Bindings = append(skill.Bindings, binding)
}

func boundedDiscoveryHash(sourcePath string) (string, error) {
	files, err := collectRegularFiles(sourcePath)
	if err != nil {
		return "", err
	}
	if len(files) > maxDiscoveryHashFiles {
		return "", errors.New("folder exceeds discovery hash bound")
	}
	total := int64(0)
	for _, relative := range files {
		info, err := os.Stat(filepath.Join(sourcePath, filepath.FromSlash(relative)))
		if err != nil {
			return "", err
		}
		total += info.Size()
		if total > maxDiscoveryHashBytes {
			return "", errors.New("folder content exceeds discovery hash bound")
		}
	}
	if total == 0 {
		return "", errors.New("folder is empty")
	}
	return folderContentHash(sourcePath)
}

// ---------------------------------------------------------------------------
// Plugin cache scanning (preserved from the skills-cli model)
// ---------------------------------------------------------------------------

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
					// Plugin managers use cachebuster symlinks as internal cache
					// metadata. They are not Skill roots and do not make discovery
					// incomplete.
					continue
				}
				path := filepath.Join(current.path, entry.Name())
				nextDepth := current.depth + 1
				if entry.Name() == "skills" {
					if nextDepth > 7 {
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
					}, Store{})
					if collector.stopped {
						_ = directory.Close()
						return
					}
					continue
				}
				if nextDepth >= 7 {
					collector.warn("Installed Skills inventory skipped an over-depth plugin cache directory.")
					continue
				}
				pending = append(pending, pendingDirectory{path: path, depth: nextDepth})
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				collector.warn(fmt.Sprintf("Could not finish reading the %s cache.", provenance))
				break
			}
		}
		_ = directory.Close()
	}
}

func pluginNameForRoot(root string) string {
	versionDir := filepath.Dir(root)
	pluginDir := filepath.Dir(versionDir)
	return cleanMetadata(filepath.Base(pluginDir), 128)
}

// ---------------------------------------------------------------------------
// Classification, capabilities, helpers
// ---------------------------------------------------------------------------

func (collector *inventoryCollector) classifyConflicts() {
	groups := make(map[string][]*InstalledSkill)
	for _, skill := range collector.byReal {
		if skill.Scope == ScopeMixed || skill.Scope == ScopePlugin || skill.Scope == ScopeBuiltin {
			continue
		}
		key := string(skill.Scope) + "\x00" + skill.Name
		groups[key] = append(groups[key], skill)
	}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		hashes := map[string]bool{}
		for _, skill := range group {
			if skill.ContentHash != "" {
				hashes[skill.ContentHash] = true
			}
		}
		for _, skill := range group {
			if len(hashes) > 1 {
				skill.Migration = "duplicate"
				collector.blockManagement(skill, "Multiple installed directories share this Skill identity with different content; resolve the duplicate before managing it.")
				continue
			}
			// Same content in several agent dirs is one shared identity.
			if skill.Manager == ManagerZen {
				continue
			}
			skill.Migration = "duplicate"
		}
	}
	// Conflict: an external copy whose name matches a Zen-owned package but
	// whose content differs. Never silently overwrite.
	for _, skill := range collector.byReal {
		if skill.Manager == ManagerZen || skill.Owned {
			continue
		}
		for _, other := range collector.byReal {
			if other == skill || !other.Owned || other.Name != skill.Name {
				continue
			}
			if skill.ContentHash != "" && other.ContentHash != "" && skill.ContentHash != other.ContentHash {
				skill.Migration = "conflict"
				collector.blockManagement(skill, "An external installation conflicts with the Zen-owned package of the same name; adopt only after resolving the difference.")
			}
			break
		}
	}
}

func (collector *inventoryCollector) rows() []InstalledSkill {
	out := make([]InstalledSkill, 0, len(collector.byReal))
	for _, skill := range collector.byReal {
		sortAgents(skill.Agents)
		sort.Slice(skill.Bindings, func(i, j int) bool {
			if skill.Bindings[i].Agent != skill.Bindings[j].Agent {
				return agentRank(skill.Bindings[i].Agent) < agentRank(skill.Bindings[j].Agent)
			}
			return scopeRank(skill.Bindings[i].Scope) < scopeRank(skill.Bindings[j].Scope)
		})
		finalizeRow(skill)
		out = append(out, *skill)
	}
	return out
}

func finalizeRow(skill *InstalledSkill) {
	if skill.CanonicalPath == "" {
		skill.CanonicalPath = skill.SourcePath
	}
	if skill.Capability.Reason == "" && !skill.Capability.CanManage {
		skill.Capability = ManagementCapability{
			Reason: unmanagedReason(skill.Manager),
		}
	}
	if skill.Manager == ManagerZen && len(skill.Agents) == 0 {
		skill.Capability = ManagementCapability{
			CanManage:  true,
			Operations: []MutationOperation{OperationBind, OperationUninstall, OperationUpdate},
			Reason:     "The canonical package has no linked supported Agent target.",
		}
	}
}

func unmanagedReason(manager Manager) string {
	switch manager {
	case ManagerBuiltin:
		return "Builtin Skills are managed by its provider."
	case ManagerPlugin:
		return "Plugin-owned Skills must be managed by their plugin owner."
	default:
		return "Zen does not manage this installation; adopt it to take ownership."
	}
}

func (collector *inventoryCollector) migrationStatus() MigrationStatus {
	status := MigrationStatus{}
	for _, skill := range collector.byReal {
		switch skill.Migration {
		case "conflict":
			status.Conflict++
		case "duplicate":
			status.Duplicate++
		default:
			if skill.Owned {
				status.Owned++
			} else {
				status.External++
			}
		}
		if skill.Tracked {
			status.Tracked++
		}
	}
	return status
}

func resolveExecutors(aliases []ExecutorAlias) []ExecutorSupport {
	out := make([]ExecutorSupport, 0, len(aliases))
	seen := map[string]bool{}
	for _, alias := range aliases {
		agent := resolveExecutorAgent(alias.Kind, alias.Command, alias.Name)
		name := strings.TrimSpace(alias.Name)
		if agent == "" || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, ExecutorSupport{
			Name: name, Kind: alias.Kind, Agent: agent, Command: alias.Command,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

const maxDiscoveryHashFiles = 256

// ---------------------------------------------------------------------------
// Common scanning helpers (shared with migrate and old inventory behavior)
// ---------------------------------------------------------------------------

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

func sortAgents(agents []Agent) {
	sort.Slice(agents, func(i, j int) bool { return agentRank(agents[i]) < agentRank(agents[j]) })
}

func agentRank(agent Agent) int {
	switch agent {
	case AgentCodex:
		return 0
	case AgentClaudeCode:
		return 1
	case AgentCursor:
		return 2
	case AgentGrok:
		return 3
	case AgentOpenCode:
		return 4
	case AgentPi:
		return 5
	default:
		return 6
	}
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
		return 5
	}
}

func (collector *inventoryCollector) add(skill *InstalledSkill) {
	collector.byReal[skill.CanonicalPath] = skill
	if skill.Name != "" {
		collector.byName[skill.Name] = append(collector.byName[skill.Name], skill)
	}
	collector.count++
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
	collector.warn("Installed Skills inventory is incomplete; management is disabled for this snapshot.")
}

func (collector *inventoryCollector) stopIncomplete() {
	collector.markIncomplete()
	collector.stopped = true
}

func (collector *inventoryCollector) disableAllMutationAuthority() {
	const reason = "Management is disabled because this inventory snapshot is incomplete."
	for _, skill := range collector.byReal {
		if !skill.Capability.CanManage {
			continue
		}
		skill.Capability = ManagementCapability{Reason: reason}
	}
}

func (collector *inventoryCollector) blockManagement(skill *InstalledSkill, reason string) {
	collector.blocked[skill.CanonicalPath] = reason
	skill.Capability = ManagementCapability{Reason: reason}
}

// AnyEnabled reports whether at least one binding is enabled.
func (entry PackageEntry) AnyEnabled() bool {
	for _, binding := range entry.Bindings {
		if binding.Enabled {
			return true
		}
	}
	return false
}
