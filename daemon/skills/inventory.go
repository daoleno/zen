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
	maxDiscoveryHashFiles     = 256
)

type ExecutorAlias struct {
	Name    string
	Kind    string
	Command string
}

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
	deleteHooks  *deleteTestHooks
}

type inventoryRoot struct {
	path      string
	label     string
	scope     Scope
	agents    []Agent
	deletable bool
	provider  string
	plugin    string
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type inventoryCollector struct {
	options    InventoryOptions
	byEntry    map[string]*InstalledSkill
	warnings   []string
	warned     map[string]struct{}
	count      int
	work       int
	incomplete bool
	stopped    bool
}

// DiscoverInventory scans every supported Agent root plus read-only built-in
// and plugin roots. Discovery itself is the management boundary; no separate
// ownership enrollment step exists.
func DiscoverInventory(options InventoryOptions) (Inventory, error) {
	normalized, err := normalizeInventoryOptions(options)
	if err != nil {
		return Inventory{}, err
	}
	collector := &inventoryCollector{
		options: normalized,
		byEntry: make(map[string]*InstalledSkill),
		warned:  make(map[string]struct{}),
	}
	collector.scanAllRoots()
	if err := normalized.Context.Err(); err != nil {
		collector.stopIncomplete()
		collector.warn("Installed Skills inventory traversal was canceled.")
	}
	if collector.incomplete {
		collector.disableDeleteAuthority()
	}

	rows := collector.rows()
	now := time.Now
	if normalized.Now != nil {
		now = normalized.Now
	}
	return Inventory{
		GeneratedAt:        now().UTC(),
		CWD:                normalized.CWD,
		Skills:             rows,
		Agents:             AgentSupportEntries(normalized),
		Executors:          resolveExecutors(normalized.Executors),
		Warnings:           collector.warnings,
		MutationOperations: SupportedMutationOperations(),
		incomplete:         collector.incomplete,
	}, normalized.Context.Err()
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
	zenStateDir := strings.TrimSpace(options.ZenStateDir)
	if zenStateDir == "" {
		zenStateDir = strings.TrimSpace(os.Getenv("ZEN_STATE_DIR"))
	}
	if zenStateDir == "" {
		zenStateDir = filepath.Join(home, ".zen")
	}
	for _, value := range []string{codexHome, claudeHome, zenStateDir} {
		if !filepath.IsAbs(value) {
			return InventoryOptions{}, errors.New("Skills root configuration must be absolute")
		}
	}

	if options.Context == nil {
		options.Context = context.Background()
	}
	if options.MaxSkills <= 0 || options.MaxSkills > defaultMaxInstalledSkills {
		options.MaxSkills = defaultMaxInstalledSkills
	}
	if options.MaxWork <= 0 || options.MaxWork > defaultMaxInventoryWork {
		options.MaxWork = defaultMaxInventoryWork
	}
	options.Home = home
	options.CWD = cwd
	options.CodexHome = filepath.Clean(codexHome)
	options.ClaudeHome = filepath.Clean(claudeHome)
	options.ZenStateDir = filepath.Clean(zenStateDir)
	return options, nil
}

func envResolverFor(options InventoryOptions) EnvResolver {
	if options.Env != nil {
		return func(key string) string { return options.Env[key] }
	}
	return osEnvResolver()
}

func (collector *inventoryCollector) scanAllRoots() {
	collector.scanRoot(inventoryRoot{
		path:  filepath.Join(collector.options.CodexHome, "skills", ".system"),
		label: "Codex built-in", scope: ScopeBuiltin, agents: []Agent{AgentCodex},
		provider: "Codex",
	})
	for _, agent := range supportedAgents {
		if collector.stopped {
			return
		}
		adapter := Adapters[agent]
		collector.scanRoot(inventoryRoot{
			path:  globalRootForAgent(agent, collector.options),
			label: adapter.Name + " global Skills", scope: ScopeGlobal,
			agents: []Agent{agent}, deletable: true,
		})
		if collector.options.CWD != "" && agent != AgentCodex {
			collector.scanRoot(inventoryRoot{
				path:  projectSkillsDir(adapter, collector.options.CWD),
				label: adapter.Name + " project Skills", scope: ScopeProject,
				agents: []Agent{agent}, deletable: true,
			})
		}
	}
	if collector.stopped {
		return
	}
	collector.scanRoot(inventoryRoot{
		path:  filepath.Join(collector.options.Home, ".agents", "skills"),
		label: "Shared user Skills", scope: ScopeGlobal,
		agents: []Agent{AgentCodex, AgentPi}, deletable: true,
	})
	if collector.options.CWD != "" {
		for _, root := range sharedProjectSkillRoots(collector.options.CWD, collector.options.Home) {
			collector.scanRoot(inventoryRoot{
				path: root, label: "Shared project Skills", scope: ScopeProject,
				agents: []Agent{AgentCodex, AgentPi}, deletable: true,
			})
			if collector.stopped {
				return
			}
		}
	}
	// V4's managed store remains discoverable only as an ordinary local root.
	collector.scanRoot(inventoryRoot{
		path:  filepath.Join(collector.options.ZenStateDir, "skills", "store"),
		label: "Local Skills storage", scope: ScopeUnknown, deletable: true,
	})
	collector.scanPluginCaches()
}

func sharedProjectSkillRoots(cwd, home string) []string {
	roots := []string{}
	current := filepath.Clean(cwd)
	normalizedHome := filepath.Clean(home)
	for {
		if current == normalizedHome || filepath.Dir(current) == current {
			break
		}
		roots = append(roots, filepath.Join(current, ".agents", "skills"))
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			break
		}
		current = filepath.Dir(current)
	}
	return roots
}

func (collector *inventoryCollector) scanRoot(root inventoryRoot) {
	if !collector.consumeWork() || strings.TrimSpace(root.path) == "" {
		return
	}
	allowedRoot, err := filepath.Abs(root.path)
	if err != nil {
		collector.markIncomplete()
		collector.warn("Could not resolve the " + root.label + " root.")
		return
	}
	root.path = filepath.Clean(allowedRoot)
	directory, err := os.Open(root.path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			collector.markIncomplete()
			collector.warn("Could not read " + root.label + ".")
		}
		return
	}
	defer directory.Close()
	for {
		if !collector.consumeWork() {
			return
		}
		entries, readErr := directory.ReadDir(64)
		for _, entry := range entries {
			if !collector.consumeWork() {
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
			collector.warn("Could not finish reading " + root.label + ".")
			return
		}
	}
}

func (collector *inventoryCollector) scanRootEntry(root inventoryRoot, entry fs.DirEntry) {
	if strings.HasPrefix(entry.Name(), ".") {
		return
	}
	rootPath := filepath.Clean(filepath.Join(root.path, entry.Name()))
	isDirectory, err := isSkillDirectory(entry, rootPath)
	if err != nil {
		collector.warn("Could not inspect a Skill in " + root.label + ".")
		return
	}
	if !isDirectory {
		return
	}
	frontmatter, ok, metadataErr := readSkillFrontmatter(filepath.Join(rootPath, "SKILL.md"))
	if metadataErr != nil {
		collector.warn("Could not read or validate Skill metadata in " + root.label + ".")
	}
	if !ok {
		return
	}
	name := cleanMetadata(entry.Name(), maxSkillNameLength)
	if name == "" {
		return
	}
	canonical, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		collector.warn("Could not resolve a Skill in " + root.label + ".")
		return
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return
	}
	canonical = filepath.Clean(canonical)
	if existing := collector.byEntry[rootPath]; existing != nil {
		mergeAgents(&existing.Agents, root.agents)
		existing.Enabled = existing.Enabled || collector.copyEnabled(root.agents, rootPath)
		return
	}

	capability := DeleteCapability{CanDelete: root.deletable}
	frontmatterName := cleanMetadata(frontmatter.Name, maxSkillNameLength)
	if frontmatterName != "" && frontmatterName != name {
		capability = DeleteCapability{Reason: "Skill metadata does not match its installed directory name."}
	}
	if !root.deletable {
		provider := strings.TrimSpace(root.provider)
		if provider == "" {
			provider = "its provider"
		}
		capability = DeleteCapability{Reason: "Provided by " + provider + " and cannot be deleted from here."}
	}
	hash, hashErr := boundedDiscoveryHash(canonical)
	warnings := []string{}
	if hashErr != nil {
		warnings = append(warnings, "Content fingerprint is unavailable: "+hashErr.Error())
	}
	skill := &InstalledSkill{
		ID:            installedSkillID(name, rootPath, canonical, root.path),
		Name:          name,
		Description:   cleanMetadata(frontmatter.Description, 240),
		Enabled:       collector.copyEnabled(root.agents, rootPath),
		RootPath:      rootPath,
		CanonicalPath: canonical,
		AllowedRoot:   root.path,
		Location:      root.label,
		Scope:         root.scope,
		Agents:        append([]Agent{}, root.agents...),
		ContentHash:   hash,
		Plugin:        cleanMetadata(root.plugin, 128),
		Risk:          scanRiskSignals(canonical),
		Warnings:      warnings,
		Capability:    capability,
	}
	collector.byEntry[rootPath] = skill
	collector.count++
}

func (collector *inventoryCollector) copyEnabled(agents []Agent, rootPath string) bool {
	// A copy is available when at least one Agent sees this root. Provider
	// enable/disable registries are intentionally not a lifecycle gate: Zen's
	// management action is exact-copy delete only.
	return len(agents) > 0 || rootPath != ""
}

func (collector *inventoryCollector) scanPluginCaches() {
	collector.scanPluginCache(filepath.Join(collector.options.CodexHome, "plugins", "cache"), AgentCodex, "Codex")
	if !collector.stopped {
		collector.scanPluginCache(filepath.Join(collector.options.ClaudeHome, "plugins", "cache"), AgentClaudeCode, "Claude Code")
	}
}

func (collector *inventoryCollector) scanPluginCache(cachePath string, agent Agent, provider string) {
	visited, roots := 0, 0
	errStop := errors.New("stop plugin traversal")
	err := filepath.WalkDir(cachePath, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) && current == cachePath {
				return fs.SkipDir
			}
			collector.warn("Could not read the " + provider + " plugin cache.")
			return fs.SkipDir
		}
		if !collector.consumeWork() {
			return errStop
		}
		visited++
		if visited > maxPluginWalkEntries || roots >= maxPluginRoots {
			collector.stopIncomplete()
			collector.warn("Installed Skills inventory stopped at its bounded plugin traversal limit.")
			return errStop
		}
		if !entry.IsDir() || entry.Name() != "skills" {
			return nil
		}
		roots++
		plugin := pluginNameForRoot(current)
		label := provider + " plugin"
		providedBy := provider + " plugin"
		if plugin != "" {
			label += " " + plugin
			providedBy += " " + plugin
		}
		collector.scanRoot(inventoryRoot{
			path: current, label: label, scope: ScopePlugin, agents: []Agent{agent},
			provider: providedBy, plugin: plugin,
		})
		return fs.SkipDir
	})
	if err != nil && !errors.Is(err, errStop) && !errors.Is(err, fs.ErrNotExist) {
		collector.warn("Could not finish reading the " + provider + " plugin cache.")
	}
}

func pluginNameForRoot(root string) string {
	return cleanMetadata(filepath.Base(filepath.Dir(filepath.Dir(root))), 128)
}

func (collector *inventoryCollector) rows() []InstalledSkill {
	rows := make([]InstalledSkill, 0, len(collector.byEntry))
	for _, skill := range collector.byEntry {
		sortAgents(skill.Agents)
		rows = append(rows, *skill)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].RootPath < rows[j].RootPath
	})
	return rows
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

func installedSkillID(name, rootPath, canonicalPath, allowedRoot string) string {
	sum := sha256.Sum256([]byte(name + "\x00" + rootPath + "\x00" + canonicalPath + "\x00" + allowedRoot))
	return hex.EncodeToString(sum[:12])
}

func validInstalledSkillID(value string) bool {
	if len(value) != 24 {
		return false
	}
	for _, current := range value {
		if (current < '0' || current > '9') && (current < 'a' || current > 'f') {
			return false
		}
	}
	return true
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
	for index, current := range supportedAgents {
		if current == agent {
			return index
		}
	}
	return len(supportedAgents)
}

func boundedDiscoveryHash(root string) (string, error) {
	files, err := collectRegularFiles(root)
	if err != nil {
		return "", err
	}
	if len(files) > maxDiscoveryHashFiles {
		return "", errors.New("folder exceeds discovery hash bound")
	}
	var total int64
	for _, relative := range files {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
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
	return folderContentHash(root)
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

func (collector *inventoryCollector) consumeWork() bool {
	if collector.stopped {
		return false
	}
	if collector.options.Context.Err() != nil {
		collector.stopIncomplete()
		return false
	}
	if collector.work >= collector.options.MaxWork {
		collector.stopIncomplete()
		collector.warn("Installed Skills inventory reached its bounded total-work limit.")
		return false
	}
	collector.work++
	return true
}

func (collector *inventoryCollector) markIncomplete() { collector.incomplete = true }

func (collector *inventoryCollector) stopIncomplete() {
	collector.incomplete = true
	collector.stopped = true
}

func (collector *inventoryCollector) disableDeleteAuthority() {
	for _, skill := range collector.byEntry {
		if skill.Capability.CanDelete {
			skill.Capability = DeleteCapability{Reason: "Delete is unavailable because this inventory snapshot is incomplete."}
		}
	}
}
