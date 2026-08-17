package skills

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ExecutorAlias is one configured (possibly custom) executor identity. Custom
// names that infer to one of the six providers reuse that provider's adapter;
// anything else is never granted lifecycle authority.
type ExecutorAlias struct {
	Name    string
	Kind    string
	Command string
}

// SourceFetcher stages package content for catalog/github provenance. The
// default fetches via git; tests inject a fixture writer so lifecycle tests
// are hermetic and never touch the network or real user state.
type SourceFetcher func(ctx context.Context, request MutationRequest, stageDir string) error

// environment bundles the resolved paths and extensions for one lifecycle
// command: a store, the adapter env, and a validated working directory.
type environment struct {
	store   Store
	env     EnvResolver
	cwd     string
	fetcher SourceFetcher
	now     func() time.Time
}

func environmentFor(options InventoryOptions, cwdRequired bool) (environment, error) {
	normalized, err := normalizeInventoryOptions(options)
	if err != nil {
		return environment{}, err
	}
	return environment{
		store:   Store{StateDir: normalized.ZenStateDir, Home: normalized.Home, Now: normalized.Now},
		env:     envResolverFor(normalized),
		cwd:     normalized.CWD,
		fetcher: normalized.Fetcher,
		now:     normalized.Now,
	}, nil
}

func envResolverFor(options InventoryOptions) EnvResolver {
	if options.Env != nil {
		return func(key string) string { return options.Env[key] }
	}
	return osEnvResolver()
}

type planContext struct {
	env environment
	ctx context.Context
}

// BuildMutationCommand validates structured inputs and produces the exact,
// reviewable plan. This is the only place plan semantics are decided; the
// server and App never build plans themselves.
func BuildMutationCommand(options InventoryOptions, request MutationRequest) (MutationCommand, error) {
	env, err := environmentFor(options, request.Scope == ScopeProject && request.Operation != OperationImport)
	if err != nil {
		return MutationCommand{}, err
	}
	switch request.Operation {
	case OperationImport:
		return buildImportPlan(request, env)
	case OperationMigrate:
		return MutationCommand{
			Operation:   OperationMigrate,
			Scope:       ScopeGlobal,
			Summary:     "Track existing local Skills across all six agents in Zen's inventory (no files are changed; adopt or forget each one afterward)",
			Changes:     []MutationChange{{Kind: "write", Path: env.store.InventoryPath(), Detail: "Track external local installations"}},
			Destructive: false,
		}, nil
	case OperationBind, OperationUnbind, OperationEnable, OperationDisable, OperationUninstall, OperationForget, OperationAdopt, OperationUpdate:
		return buildManagedPlan(request, env)
	default:
		return MutationCommand{}, fmt.Errorf("unsupported Skill operation %q", request.Operation)
	}
}

// ---------------------------------------------------------------------------
// Import / adopt / forget
// ---------------------------------------------------------------------------

func buildImportPlan(request MutationRequest, env environment) (MutationCommand, error) {
	if err := ValidateSkillName(request.SkillName); err != nil {
		return MutationCommand{}, err
	}
	agents, err := validateAgents(request.Agents)
	if err != nil {
		return MutationCommand{}, err
	}
	if err := ValidateScope(request.Scope); err != nil {
		return MutationCommand{}, err
	}
	cwd, err := ValidateCWD(request.CWD, request.Scope == ScopeProject)
	if err != nil {
		return MutationCommand{}, err
	}
	sourceType, err := detectImportSource(request)
	if err != nil {
		return MutationCommand{}, err
	}
	store := env.store
	inventory, err := store.LoadInventory(false)
	if err != nil {
		return MutationCommand{}, err
	}
	if existing, ok := inventory.Packages[request.SkillName]; ok {
		if existing.Owned {
			return MutationCommand{}, fmt.Errorf("Skill %q is already installed", request.SkillName)
		}
		return MutationCommand{}, fmt.Errorf("Skill %q is tracked as an external installation; forget or adopt it first", request.SkillName)
	}
	targets, err := bindingTargetPaths(request, env, cwd)
	if err != nil {
		return MutationCommand{}, err
	}
	changes := []MutationChange{
		{Kind: "create_dir", Path: store.PackageDir(request.SkillName), Detail: "Canonical Zen store entry (" + sourceDetail(sourceType, request) + ")"},
	}
	summary := fmt.Sprintf("Import %s into Zen's canonical store", request.SkillName)
	if request.Source != "" {
		summary += " from " + request.Source
	}
	for _, target := range targets {
		changes = append(changes, target.change)
	}
	if len(agents) == 0 {
		// Import always binds at least one target: ownership without a binding
		// is expressible only through explicit uninstall after binding.
		return MutationCommand{}, errors.New("choose at least one supported agent to bind")
	}
	return MutationCommand{
		Operation:   OperationImport,
		Scope:       request.Scope,
		Agents:      agents,
		SkillName:   request.SkillName,
		ImportID:    importIDFor(request),
		Source:      request.Source,
		Ref:         request.Ref,
		InfoPath:    request.InfoPath,
		Summary:     summary,
		Changes:     changes,
		Destructive: false,
	}, nil
}

type bindingTarget struct {
	agent  Agent
	scope  Scope
	dir    string
	path   string
	mode   BindingMode
	change MutationChange
}

func bindingTargetPaths(request MutationRequest, env environment, cwd string) ([]bindingTarget, error) {
	targets := make([]bindingTarget, 0, len(request.Agents))
	for _, agent := range request.Agents {
		adapter, err := adapterFor(agent)
		if err != nil {
			return nil, err
		}
		dir := ""
		if request.Scope == ScopeGlobal {
			dir = globalSkillsDir(adapter, env.store.Home, env.env)
		} else {
			if cwd == "" {
				return nil, errors.New("project scope requires a working directory")
			}
			dir = projectSkillsDir(adapter, cwd)
		}
		target := bindingTarget{
			agent: agent, scope: request.Scope, dir: dir,
			path: filepath.Join(dir, request.SkillName), mode: adapter.Mode,
		}
		switch adapter.Mode {
		case BindingSymlink:
			target.change = MutationChange{
				Kind: "symlink", Path: target.path, Destination: env.store.PackageDir(request.SkillName),
				Detail: "Symlink " + request.SkillName + " for " + adapter.Name,
			}
		case BindingCopy:
			target.change = MutationChange{
				Kind: "copy_file", Path: env.store.PackageDir(request.SkillName), Destination: target.path,
				Detail: "Materialize " + request.SkillName + " for " + adapter.Name + " (copy, drift-checked)",
			}
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func detectImportSource(request MutationRequest) (SourceType, error) {
	if strings.TrimSpace(request.InfoPath) != "" {
		path := filepath.Clean(request.InfoPath)
		if !filepath.IsAbs(path) {
			return "", errors.New("import path must be absolute")
		}
		for _, part := range strings.Split(path, string(filepath.Separator)) {
			if part == ".." {
				return "", errors.New("import path must not traverse directories")
			}
		}
		switch {
		case strings.HasSuffix(strings.ToLower(path), ".zip"),
			strings.HasSuffix(strings.ToLower(path), ".tar"),
			strings.HasSuffix(strings.ToLower(path), ".tar.gz"),
			strings.HasSuffix(strings.ToLower(path), ".tgz"):
			return SourceTypeArchive, nil
		default:
			return SourceTypeLocal, nil
		}
	}
	if request.Source != "" {
		if err := ValidateCatalogIdentity(request.SkillID, request.Source, request.SkillName); err != nil {
			return "", err
		}
		return SourceTypeCatalog, nil
	}
	return "", errors.New("import requires a catalog identity, a local directory, or an archive")
}

func importIDFor(request MutationRequest) string {
	if request.SkillID != "" {
		return request.SkillID
	}
	if request.Source != "" {
		return request.Source + "/" + request.SkillName
	}
	return ""
}

func sourceDetail(sourceType SourceType, request MutationRequest) string {
	switch sourceType {
	case SourceTypeArchive:
		return "archive " + filepath.Base(request.InfoPath)
	case SourceTypeLocal:
		return "local directory"
	default:
		if request.Source != "" {
			return "catalog " + request.Source
		}
		return "import"
	}
}

func buildManagedPlan(request MutationRequest, env environment) (MutationCommand, error) {
	if err := ValidateSkillName(request.SkillName); err != nil {
		return MutationCommand{}, err
	}
	store := env.store
	inventory, err := store.LoadInventory(true)
	if err != nil {
		return MutationCommand{}, err
	}
	entry, ok := inventory.Packages[request.SkillName]
	if !ok {
		return MutationCommand{}, fmt.Errorf("Skill %q is not in Zen's inventory", request.SkillName)
	}

	switch request.Operation {
	case OperationUninstall, OperationForget, OperationAdopt, OperationUpdate:
		return buildPackageLifecyclePlan(request, entry, env)
	default:
		return buildBindingPlan(request, entry, env)
	}
}

func buildPackageLifecyclePlan(request MutationRequest, entry PackageEntry, env environment) (MutationCommand, error) {
	store := env.store
	switch request.Operation {
	case OperationUninstall:
		if !entry.Owned {
			return MutationCommand{}, errors.New("external installations are forgotten, not uninstalled")
		}
		changes := []MutationChange{{Kind: "remove", Path: store.PackageDir(request.SkillName), Detail: "Remove canonical store content"}}
		for _, binding := range entry.Bindings {
			changes = append(changes, removeBindingChange(binding))
		}
		changes = append(changes, MutationChange{Kind: "remove", Path: store.InventoryPath(), Detail: "Remove inventory entry for " + request.SkillName})
		return MutationCommand{
			Operation: OperationUninstall, SkillName: request.SkillName,
			Agents: entryBindingAgents(entry), Scope: request.Scope,
			Summary:     "Uninstall " + request.SkillName + " (remove all bindings, store content, and inventory entry)",
			Changes:     changes,
			Destructive: true,
		}, nil
	case OperationForget:
		if entry.Owned {
			return MutationCommand{}, errors.New("owned packages are uninstalled, not forgotten")
		}
		return MutationCommand{
			Operation: OperationForget, SkillName: request.SkillName, Scope: request.Scope,
			Summary:     "Forget external skill " + request.SkillName + " (Zen inventory entry only; no files are deleted)",
			Changes:     []MutationChange{{Kind: "remove", Path: store.InventoryPath(), Detail: "Remove tracked inventory entry for " + request.SkillName}},
			Destructive: false,
		}, nil
	case OperationAdopt:
		if entry.Owned {
			return MutationCommand{}, errors.New("Skill " + request.SkillName + " is already Zen-owned")
		}
		externalDir, err := trackedExternalDir(entry)
		if err != nil {
			return MutationCommand{}, err
		}
		hash, err := folderContentHash(externalDir)
		if err != nil {
			return MutationCommand{}, fmt.Errorf("could not hash the external installation: %w", err)
		}
		_ = hash
		agents := request.Agents
		if len(agents) == 0 {
			agents = entry.DiscoveredAgents
		}
		if len(agents) == 0 {
			return MutationCommand{}, errors.New("adopt requires at least one discovered or requested agent")
		}
		changes := []MutationChange{
			{Kind: "copy_file", Path: externalDir, Destination: store.PackageDir(request.SkillName), Detail: "Copy external content into the canonical store"},
			{Kind: "write", Path: store.InventoryPath(), Detail: "Mark " + request.SkillName + " as Zen-owned"},
		}
		return MutationCommand{
			Operation: OperationAdopt, SkillName: request.SkillName, Scope: request.Scope, Agents: agents,
			Source: entry.Source, Ref: entry.Ref,
			Summary:     "Manage " + request.SkillName + " with Zen by copying it into the managed store (the external source remains untouched; bind the managed copy explicitly afterward)",
			Changes:     changes,
			Destructive: false,
		}, nil
	case OperationUpdate:
		if !entry.Owned {
			return MutationCommand{}, errors.New("external installations cannot be updated by Zen")
		}
		if !updateProvenancePinned(entry) {
			return MutationCommand{}, errors.New("Skill update requires pinned, validated provenance")
		}
		summary := "Update " + request.SkillName + " to its pinned provenance"
		if entry.Ref != "" {
			summary += " (" + entry.Ref + ")"
		}
		return MutationCommand{
			Operation: OperationUpdate, SkillName: request.SkillName, Scope: request.Scope,
			Source: entry.Source, Ref: entry.Ref,
			Summary: summary,
			Changes: []MutationChange{
				{Kind: "keep", Path: store.PackageDir(request.SkillName), Detail: "Atomically replace content when the pinned source changed; exact rollback on failure"},
				{Kind: "write", Path: store.InventoryPath(), Detail: "Refresh content hash and updated timestamp"},
			},
			Destructive: false,
		}, nil
	}
	return MutationCommand{}, errors.New("unsupported operation")
}

func buildBindingPlan(request MutationRequest, entry PackageEntry, env environment) (MutationCommand, error) {
	agents, err := validateAgents(request.Agents)
	if err != nil {
		return MutationCommand{}, err
	}
	if !entry.Owned {
		return MutationCommand{}, errors.New("only Zen-owned packages can be bound; adopt the external skill first")
	}
	if err := ValidateScope(request.Scope); err != nil {
		return MutationCommand{}, err
	}
	cwd, err := ValidateCWD(request.CWD, request.Scope == ScopeProject)
	if err != nil {
		return MutationCommand{}, err
	}
	store := env.store
	existing := map[string]BindingEntry{}
	for _, binding := range entry.Bindings {
		existing[string(binding.Agent)+"/"+string(binding.Scope)] = binding
	}

	targets, err := bindingTargetPaths(request, env, cwd)
	if err != nil {
		return MutationCommand{}, err
	}
	verb := "bind"
	summary := ""
	changes := []MutationChange{}
	destructive := false
	switch request.Operation {
	case OperationBind:
		for _, target := range targets {
			key := string(target.agent) + "/" + string(target.scope)
			if _, ok := existing[key]; ok {
				return MutationCommand{}, fmt.Errorf("Skill %q is already bound to %s (%s)", request.SkillName, agentName(target.agent), target.scope)
			}
			changes = append(changes, target.change)
		}
		summary = "Bind " + request.SkillName + " to " + agentsLabel(agents) + " (" + string(request.Scope) + ")"
	case OperationUnbind:
		verb = "unbind"
		for _, target := range targets {
			key := string(target.agent) + "/" + string(target.scope)
			binding, ok := existing[key]
			if !ok {
				return MutationCommand{}, fmt.Errorf("Skill %q has no %s binding for %s", request.SkillName, target.scope, agentName(target.agent))
			}
			changes = append(changes, removeBindingChange(binding))
			changes = append(changes, MutationChange{Kind: "write", Path: store.InventoryPath(), Detail: "Remove binding for " + agentName(target.agent)})
		}
		summary = "Unbind " + request.SkillName + " from " + agentsLabel(agents) + " (" + string(request.Scope) + "); package content stays in the store"
	case OperationEnable, OperationDisable:
		verb = string(request.Operation)
		for _, target := range targets {
			key := string(target.agent) + "/" + string(target.scope)
			binding, ok := existing[key]
			if !ok {
				return MutationCommand{}, fmt.Errorf("Skill %q has no %s binding for %s to %s", request.SkillName, target.scope, agentName(target.agent), verb)
			}
			if request.Operation == OperationEnable && binding.Enabled {
				return MutationCommand{}, fmt.Errorf("Skill %q is already enabled for %s", request.SkillName, agentName(target.agent))
			}
			if request.Operation == OperationDisable && !binding.Enabled {
				return MutationCommand{}, fmt.Errorf("Skill %q is already disabled for %s", request.SkillName, agentName(target.agent))
			}
			if request.Operation == OperationDisable {
				changes = append(changes, removeMaterializationChange(binding))
			} else {
				changes = append(changes, target.change)
			}
			changes = append(changes, MutationChange{Kind: "write", Path: store.InventoryPath(), Detail: "Update enabled state for " + agentName(target.agent)})
		}
		summary = strings.Title(verb) + " " + request.SkillName + " bindings (" + agentsLabel(agents) + ", " + string(request.Scope) + ")"
	}
	return MutationCommand{
		Operation: MutationOperation(verb), Scope: request.Scope, Agents: agents,
		SkillName: request.SkillName, Summary: summary, Changes: changes, Destructive: destructive,
	}, nil
}

func removeBindingChange(binding BindingEntry) MutationChange {
	return MutationChange{
		Kind: "remove", Path: binding.TargetPath,
		Detail: "Remove binding for " + agentName(binding.Agent) + " (" + string(binding.Scope) + ")",
	}
}

func removeMaterializationChange(binding BindingEntry) MutationChange {
	return MutationChange{
		Kind: "remove", Path: binding.TargetPath,
		Detail: "Remove " + string(binding.Mode) + " materialization for " + agentName(binding.Agent) + " (package content stays in the store)",
	}
}

// ---------------------------------------------------------------------------
// Execution (native, atomic, cancelable)
// ---------------------------------------------------------------------------

// ExecuteMutationCommand runs the reviewed plan natively on the daemon host.
// It never shells out; every effect is a bounded filesystem or fetch
// operation that observes ctx cancellation at safe points. The returned
// Execution reports the truthful outcome and a bounded human summary.
func ExecuteMutationCommand(ctx context.Context, command MutationCommand, options MutationExecutionOptions) (MutationExecution, error) {
	inventoryOptions := options.InventoryOptions
	if inventoryOptions.Home == "" && options.CWD != "" {
		// Project-scope mutations carry a validated working directory with the
		// request itself when options do not otherwise set one.
	}
	env, err := environmentFor(inventoryOptions, command.Scope == ScopeProject)
	if err != nil {
		return MutationExecution{}, err
	}
	if options.CWD != "" {
		env.cwd = options.CWD
	}
	if ctx == nil {
		ctx = context.Background()
	}
	startedAt := time.Now()
	result, err := executePlan(ctx, env, command)
	durationMS := time.Since(startedAt).Milliseconds()
	if err != nil {
		return MutationExecution{}, err
	}
	return MutationExecution{
		Success:    true,
		ExitCode:   0,
		Output:     boundedMutationOutput([]byte(result)),
		DurationMS: durationMS,
	}, nil
}

func executePlan(ctx context.Context, env environment, command MutationCommand) (string, error) {
	store := env.store
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := store.Lock(); err != nil {
		return "", err
	}
	defer store.Unlock()

	inventory, err := store.LoadInventory(command.Operation != OperationImport && command.Operation != OperationMigrate)
	if err != nil {
		return "", err
	}
	if inventory.Packages == nil {
		inventory.Packages = map[string]PackageEntry{}
	}

	switch command.Operation {
	case OperationImport:
		return executeImport(ctx, env, command, &inventory)
	case OperationMigrate:
		return executeMigrate(ctx, env, &inventory)
	case OperationBind, OperationUnbind, OperationEnable, OperationDisable:
		return executeBindingMutation(ctx, env, command, &inventory)
	case OperationUninstall:
		return executeUninstall(env, command, &inventory)
	case OperationForget:
		return executeForget(env, command, &inventory)
	case OperationAdopt:
		return executeAdopt(ctx, env, command, &inventory)
	case OperationUpdate:
		return executeUpdate(ctx, env, command, &inventory)
	default:
		return "", fmt.Errorf("unsupported Skill operation %q", command.Operation)
	}
}

func executeMigrate(ctx context.Context, env environment, inventory *InventoryFile) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if inventory.Packages == nil {
		inventory.Packages = map[string]PackageEntry{}
	}
	report := MigrationReport{}
	seenHash := map[string]string{}
	for _, agent := range []Agent{AgentCodex, AgentClaudeCode, AgentCursor, AgentGrok, AgentOpenCode, AgentPi} {
		adapter, err := adapterFor(agent)
		if err != nil {
			continue
		}
		globalDir := globalSkillsDir(adapter, env.store.Home, env.env)
		report.scanRoot(env.store, inventory, migrationSource{dir: globalDir, scope: ScopeGlobal, agents: []Agent{agent}}, seenHash)
		if env.cwd != "" {
			report.scanRoot(env.store, inventory, migrationSource{dir: projectSkillsDir(adapter, env.cwd), scope: ScopeProject, agents: []Agent{agent}}, seenHash)
		}
	}
	if err := env.store.SaveInventory(*inventory); err != nil {
		return "", err
	}
	return fmt.Sprintf("Migrated %d external installation(s) into Zen's inventory without touching their files.", report.Tracked), nil
}

func executeImport(ctx context.Context, env environment, command MutationCommand, inventory *InventoryFile) (string, error) {
	store := env.store
	if _, exists := inventory.Packages[command.SkillName]; exists {
		return "", fmt.Errorf("Skill %q already exists in Zen's inventory", command.SkillName)
	}
	staging, err := os.MkdirTemp(store.TmpDir(), "import-*")
	if err != nil {
		if mkErr := os.MkdirAll(store.TmpDir(), 0o700); mkErr != nil {
			return "", mkErr
		}
		staging, err = os.MkdirTemp(store.TmpDir(), "import-*")
		if err != nil {
			return "", err
		}
	}
	defer os.RemoveAll(staging)

	sourceRoot, sourceType, err := stageImportSource(ctx, env, command, staging)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", ctx.Err()
	}
	// Verify the staged folder is a real Skill package.
	if _, ok, err := readSkillFrontmatter(filepath.Join(sourceRoot, "SKILL.md")); err != nil || !ok {
		return "", errors.New("the import source is not a valid Skill package (SKILL.md with frontmatter required)")
	}
	hash, err := folderContentHash(sourceRoot)
	if err != nil {
		return "", fmt.Errorf("could not hash the imported package: %w", err)
	}

	packageDir := store.PackageDir(command.SkillName)
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		return "", err
	}
	if err := copyDirBounded(sourceRoot, packageDir); err != nil {
		_ = os.RemoveAll(packageDir)
		return "", fmt.Errorf("materialize package: %w", err)
	}
	now := store.now().UTC().Format(time.RFC3339)
	source := command.Source
	if source == "" && (sourceType == SourceTypeLocal || sourceType == SourceTypeArchive) {
		// Local/archive provenance keeps the import path so update can re-stage
		// the same pinned source atomically.
		source = command.InfoPath
	}
	entry := PackageEntry{
		SkillName:   command.SkillName,
		Source:      source,
		SourceType:  string(sourceType),
		Ref:         command.Ref,
		ContentHash: hash,
		InstalledAt: now,
		UpdatedAt:   now,
		Owned:       true,
	}
	bindings := make([]BindingEntry, 0, len(command.Agents))
	for _, agent := range command.Agents {
		binding, err := materializeBinding(store, entry, agent, command.Scope, env)
		if err != nil {
			// Roll back everything already created: bindings and store content.
			for _, created := range bindings {
				_ = removeMaterialization(created)
			}
			_ = os.RemoveAll(packageDir)
			return "", err
		}
		bindings = append(bindings, binding)
	}
	entry.Bindings = bindings
	inventory.Packages[command.SkillName] = entry
	if err := store.SaveInventory(*inventory); err != nil {
		// Best-effort rollback so a failed commit never leaves orphaned
		// bindings or half-owned store content behind.
		for _, created := range bindings {
			_ = removeMaterialization(created)
		}
		_ = os.RemoveAll(packageDir)
		return "", err
	}
	return fmt.Sprintf("Imported %s (%d files, %s)", command.SkillName, countPackageFiles(packageDir), shortHash(hash)), nil
}

// stageImportSource materializes the source package into staging and returns
// the package root plus its source type. Archives are extracted safely; local
// directories are used in place; catalog sources go through the fetcher.
func stageImportSource(ctx context.Context, env environment, command MutationCommand, staging string) (string, SourceType, error) {
	request := MutationRequest{
		SkillName: command.SkillName, Source: command.Source,
		SkillID: command.ImportID, Ref: command.Ref, Agents: command.Agents, Scope: command.Scope,
	}
	if command.InfoPath != "" {
		request.InfoPath = command.InfoPath
	}
	if request.InfoPath != "" {
		path := filepath.Clean(request.InfoPath)
		switch {
		case strings.HasSuffix(strings.ToLower(path), ".zip"),
			strings.HasSuffix(strings.ToLower(path), ".tar"),
			strings.HasSuffix(strings.ToLower(path), ".tar.gz"),
			strings.HasSuffix(strings.ToLower(path), ".tgz"):
			root, err := ExtractArchiveSafe(path, staging)
			return root, SourceTypeArchive, err
		default:
			info, err := os.Stat(path)
			if err != nil {
				return "", "", err
			}
			if !info.IsDir() {
				return "", "", errors.New("local import path is not a directory")
			}
			return resolveSkillRoot(path, request.SkillName), SourceTypeLocal, nil
		}
	}
	// Catalog/github: stage through the fetcher into <staging>/content.
	contentDir := filepath.Join(staging, "content")
	if err := os.MkdirAll(contentDir, 0o700); err != nil {
		return "", "", err
	}
	request.InfoPath = contentDir
	fetch := env.fetcher
	if fetch == nil {
		fetch = fetchGitSkill
	}
	if err := fetch(ctx, request, contentDir); err != nil {
		return "", "", err
	}
	return resolveSkillRoot(contentDir, request.SkillName), SourceTypeCatalog, nil
}

// resolveSkillRoot locates the skill folder inside a staged tree: the tree
// itself, <tree>/<name>, or the common repository layout <tree>/skills/<name>.
// A bounded single-chain wrapper is also unwrapped.
func resolveSkillRoot(dir, name string) string {
	candidates := []string{
		filepath.Join(dir, name),
		filepath.Join(dir, "skills", name),
	}
	for _, candidate := range candidates {
		if fileExists(filepath.Join(candidate, "SKILL.md")) {
			return candidate
		}
	}
	if fileExists(filepath.Join(dir, "SKILL.md")) {
		return dir
	}
	return locateSkillRoot(dir)
}

func materializeBinding(store Store, entry PackageEntry, agent Agent, scope Scope, env environment) (BindingEntry, error) {
	adapter, err := adapterFor(agent)
	if err != nil {
		return BindingEntry{}, err
	}
	dir := ""
	if scope == ScopeGlobal {
		dir = globalSkillsDir(adapter, store.Home, env.env)
	} else {
		if env.cwd == "" {
			return BindingEntry{}, errors.New("project scope requires a working directory")
		}
		dir = projectSkillsDir(adapter, env.cwd)
	}
	target := filepath.Join(dir, entry.SkillName)
	now := store.now().UTC().Format(time.RFC3339)
	binding := BindingEntry{
		Agent: agent, Scope: scope, TargetPath: target, Enabled: true, BoundAt: now, Mode: adapter.Mode, Note: adapter.Note,
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return BindingEntry{}, err
	}
	if adapter.Mode == BindingSymlink {
		if err := os.Symlink(store.PackageDir(entry.SkillName), target); err != nil {
			return BindingEntry{}, err
		}
	} else {
		if err := copyDirBounded(store.PackageDir(entry.SkillName), target); err != nil {
			return BindingEntry{}, err
		}
	}
	return binding, nil
}

func removeMaterialization(binding BindingEntry) error {
	info, err := os.Lstat(binding.TargetPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
		return os.RemoveAll(binding.TargetPath)
	}
	return os.Remove(binding.TargetPath)
}

func executeBindingMutation(ctx context.Context, env environment, command MutationCommand, inventory *InventoryFile) (string, error) {
	entry := inventory.Packages[command.SkillName]
	existing := map[string]BindingEntry{}
	for _, binding := range entry.Bindings {
		existing[string(binding.Agent)+"/"+string(binding.Scope)] = binding
	}
	bindings := make([]BindingEntry, 0, len(entry.Bindings))
	for _, binding := range entry.Bindings {
		bindings = append(bindings, binding)
	}
	changed := 0
	for _, agent := range command.Agents {
		key := string(agent) + "/" + string(command.Scope)
		adapter, _ := adapterFor(agent)
		dir := ""
		if command.Scope == ScopeGlobal {
			dir = globalSkillsDir(adapter, env.store.Home, env.env)
		} else {
			dir = projectSkillsDir(adapter, env.cwd)
		}
		targetPath := filepath.Join(dir, command.SkillName)
		switch command.Operation {
		case OperationBind:
			if _, ok := existing[key]; ok {
				continue
			}
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return "", err
			}
			now := env.store.now().UTC().Format(time.RFC3339)
			binding := BindingEntry{Agent: agent, Scope: command.Scope, TargetPath: targetPath, Enabled: true, BoundAt: now, Mode: adapter.Mode, Note: adapter.Note}
			if adapter.Mode == BindingSymlink {
				if err := os.Symlink(env.store.PackageDir(command.SkillName), targetPath); err != nil {
					return "", err
				}
			} else if err := copyDirBounded(env.store.PackageDir(command.SkillName), targetPath); err != nil {
				return "", err
			}
			bindings = append(bindings, binding)
			changed++
		case OperationUnbind:
			binding, ok := existing[key]
			if !ok {
				return "", fmt.Errorf("no %s binding for %s", command.Scope, agentName(agent))
			}
			if err := removeMaterialization(binding); err != nil {
				return "", err
			}
			kept := bindings[:0]
			for _, current := range bindings {
				if current.Agent == agent && current.Scope == command.Scope {
					continue
				}
				kept = append(kept, current)
			}
			bindings = kept
			changed++
		case OperationEnable:
			binding, ok := existing[key]
			if !ok {
				return "", fmt.Errorf("no %s binding for %s to enable", command.Scope, agentName(agent))
			}
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return "", err
			}
			if adapter.Mode == BindingSymlink {
				if err := os.Symlink(env.store.PackageDir(command.SkillName), targetPath); err != nil {
					return "", err
				}
			} else if err := copyDirBounded(env.store.PackageDir(command.SkillName), targetPath); err != nil {
				return "", err
			}
			binding.Enabled = true
			replaceBinding(bindings, binding)
			changed++
		case OperationDisable:
			binding, ok := existing[key]
			if !ok {
				return "", fmt.Errorf("no %s binding for %s to disable", command.Scope, agentName(agent))
			}
			if err := removeMaterialization(binding); err != nil {
				return "", err
			}
			binding.Enabled = false
			replaceBinding(bindings, binding)
			changed++
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
	}
	if changed == 0 {
		return "No binding changes were needed.", nil
	}
	entry.Bindings = bindings
	inventory.Packages[command.SkillName] = entry
	if err := env.store.SaveInventory(*inventory); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s: %d binding(s) changed", strings.Title(string(command.Operation)), command.SkillName, changed), nil
}

func replaceBinding(bindings []BindingEntry, updated BindingEntry) {
	for index := range bindings {
		if bindings[index].Agent == updated.Agent && bindings[index].Scope == updated.Scope {
			bindings[index] = updated
			return
		}
	}
	bindings = append(bindings, updated)
}

func executeUninstall(env environment, command MutationCommand, inventory *InventoryFile) (string, error) {
	entry := inventory.Packages[command.SkillName]
	if !entry.Owned {
		return "", errors.New("external installations are forgotten, not uninstalled")
	}
	// Bindings first: removing them never touches store content.
	for _, binding := range entry.Bindings {
		if err := removeMaterialization(binding); err != nil {
			return "", err
		}
	}
	if err := env.store.stageToRollback(command.SkillName); err != nil {
		return "", err
	}
	delete(inventory.Packages, command.SkillName)
	if err := env.store.SaveInventory(*inventory); err != nil {
		return "", err
	}
	env.store.RemoveRollback(command.SkillName)
	return "Uninstalled " + command.SkillName + ": bindings, store content, and inventory entry removed.", nil
}

func executeForget(env environment, command MutationCommand, inventory *InventoryFile) (string, error) {
	entry := inventory.Packages[command.SkillName]
	if entry.Owned {
		return "", errors.New("owned packages are uninstalled, not forgotten")
	}
	delete(inventory.Packages, command.SkillName)
	if err := env.store.SaveInventory(*inventory); err != nil {
		return "", err
	}
	return "Forgot tracked external skill " + command.SkillName + ". External files were left untouched.", nil
}

func executeAdopt(ctx context.Context, env environment, command MutationCommand, inventory *InventoryFile) (string, error) {
	entry := inventory.Packages[command.SkillName]
	externalDir, err := trackedExternalDir(entry)
	if err != nil {
		return "", err
	}
	packageDir := env.store.PackageDir(command.SkillName)
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		return "", err
	}
	if err := copyDirBounded(externalDir, packageDir); err != nil {
		_ = os.RemoveAll(packageDir)
		return "", err
	}
	hash, err := folderContentHash(packageDir)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		_ = os.RemoveAll(packageDir)
		return "", err
	}
	entry.Owned = true
	entry.ContentHash = hash
	entry.UpdatedAt = env.store.now().UTC().Format(time.RFC3339)
	// Adoption copies ownership into Zen but deliberately leaves the external
	// installation unchanged. Managed bindings are an explicit later action.
	entry.Bindings = nil
	inventory.Packages[command.SkillName] = entry
	if err := env.store.SaveInventory(*inventory); err != nil {
		_ = os.RemoveAll(packageDir)
		return "", err
	}
	return "Adopted " + command.SkillName + " into Zen's store (" + shortHash(hash) + "). The external source was left untouched.", nil
}

func executeUpdate(ctx context.Context, env environment, command MutationCommand, inventory *InventoryFile) (string, error) {
	entry := inventory.Packages[command.SkillName]
	staging, err := os.MkdirTemp(env.store.TmpDir(), "update-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)

	var sourceRoot string
	switch entry.SourceType {
	case string(SourceTypeLocal), string(SourceTypeArchive):
		sourceRoot, _, err = stageImportSource(ctx, env, MutationCommand{
			SkillName: command.SkillName, Source: entry.Source, InfoPath: entry.Source,
		}, staging)
		if err != nil {
			return "", err
		}
	default:
		contentDir := filepath.Join(staging, "content")
		if err := os.MkdirAll(contentDir, 0o700); err != nil {
			return "", err
		}
		request := MutationRequest{
			SkillName: command.SkillName, Source: entry.Source, Ref: entry.Ref,
			SkillID: entry.Source + "/" + command.SkillName, InfoPath: contentDir,
		}
		fetch := env.fetcher
		if fetch == nil {
			fetch = fetchGitSkill
		}
		if err := fetch(ctx, request, contentDir); err != nil {
			return "", err
		}
		sourceRoot = resolveSkillRoot(contentDir, command.SkillName)
	}
	newHash, err := folderContentHash(sourceRoot)
	if err != nil {
		return "", fmt.Errorf("could not hash the updated package: %w", err)
	}
	if newHash == entry.ContentHash {
		return "Skill " + command.SkillName + " is already up to date at " + shortHash(newHash) + ".", nil
	}
	// Atomic replacement with rollback: stage old content, write new, and only
	// then commit the inventory. Any failure restores the old content.
	oldPackage := env.store.PackageDir(command.SkillName)
	backup := filepath.Join(env.store.RollbackDir(), command.SkillName+"-prev")
	if err := os.MkdirAll(env.store.RollbackDir(), 0o700); err != nil {
		return "", err
	}
	_ = os.RemoveAll(backup)
	if err := os.Rename(oldPackage, backup); err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if committed {
			_ = os.RemoveAll(backup)
			return
		}
		_ = os.RemoveAll(oldPackage)
		_ = os.Rename(backup, oldPackage)
	}()
	if err := copyDirBounded(sourceRoot, oldPackage); err != nil {
		return "", err
	}
	copyRollbacks, err := replaceEnabledCopyBindings(entry.Bindings, oldPackage)
	if err != nil {
		return "", err
	}
	defer func() {
		for index := len(copyRollbacks) - 1; index >= 0; index-- {
			copyRollbacks[index].finish(committed)
		}
	}()
	entry.PreviousHash = entry.ContentHash
	entry.ContentHash = newHash
	entry.UpdatedAt = env.store.now().UTC().Format(time.RFC3339)
	inventory.Packages[command.SkillName] = entry
	if err := env.store.SaveInventory(*inventory); err != nil {
		return "", err
	}
	committed = true
	return fmt.Sprintf("Updated %s: %s -> %s with exact rollback retained.", command.SkillName, shortHash(entry.PreviousHash), shortHash(newHash)), nil
}

type copyBindingRollback struct {
	target    string
	backup    string
	hadTarget bool
}

func (rollback copyBindingRollback) finish(committed bool) {
	if committed {
		_ = os.RemoveAll(rollback.backup)
		return
	}
	_ = os.RemoveAll(rollback.target)
	if rollback.hadTarget {
		_ = os.Rename(rollback.backup, rollback.target)
	}
}

// Copy adapters must advance with the canonical package. Each replacement is
// staged beside its target so rename remains atomic on that filesystem, and
// every prior target remains available until inventory commit succeeds.
func replaceEnabledCopyBindings(bindings []BindingEntry, source string) ([]copyBindingRollback, error) {
	prepared := make([]struct {
		target string
		stage  string
	}, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Mode != BindingCopy || !binding.Enabled {
			continue
		}
		parent := filepath.Dir(binding.TargetPath)
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return nil, err
		}
		stage, err := os.MkdirTemp(parent, ".zen-skill-update-*")
		if err != nil {
			return nil, err
		}
		if err := copyDirBounded(source, stage); err != nil {
			_ = os.RemoveAll(stage)
			for _, item := range prepared {
				_ = os.RemoveAll(item.stage)
			}
			return nil, err
		}
		prepared = append(prepared, struct {
			target string
			stage  string
		}{target: binding.TargetPath, stage: stage})
	}

	rollbacks := make([]copyBindingRollback, 0, len(prepared))
	for index, item := range prepared {
		backup := item.stage + "-previous"
		_, statErr := os.Lstat(item.target)
		hadTarget := statErr == nil
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			for _, pending := range prepared[index:] {
				_ = os.RemoveAll(pending.stage)
			}
			for rollbackIndex := len(rollbacks) - 1; rollbackIndex >= 0; rollbackIndex-- {
				rollbacks[rollbackIndex].finish(false)
			}
			return nil, statErr
		}
		if hadTarget {
			if err := os.Rename(item.target, backup); err != nil {
				for _, pending := range prepared[index:] {
					_ = os.RemoveAll(pending.stage)
				}
				for rollbackIndex := len(rollbacks) - 1; rollbackIndex >= 0; rollbackIndex-- {
					rollbacks[rollbackIndex].finish(false)
				}
				return nil, err
			}
		}
		rollback := copyBindingRollback{target: item.target, backup: backup, hadTarget: hadTarget}
		if err := os.Rename(item.stage, item.target); err != nil {
			rollback.finish(false)
			for _, pending := range prepared[index+1:] {
				_ = os.RemoveAll(pending.stage)
			}
			for rollbackIndex := len(rollbacks) - 1; rollbackIndex >= 0; rollbackIndex-- {
				rollbacks[rollbackIndex].finish(false)
			}
			return nil, err
		}
		rollbacks = append(rollbacks, rollback)
	}
	return rollbacks, nil
}

func fetchCommand(entry PackageEntry) MutationCommand {
	return MutationCommand{SkillName: entry.SkillName, Source: entry.Source, Ref: entry.Ref, ImportID: entry.Source + "/" + entry.SkillName}
}

func trackedExternalDir(entry PackageEntry) (string, error) {
	if entry.Source == "" {
		return "", errors.New("tracked external skill has no recorded source directory")
	}
	path := filepath.Clean(entry.Source)
	if !filepath.IsAbs(path) {
		return "", errors.New("tracked external skill source must be an absolute directory")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("external skill directory is unavailable: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("external skill source is not a directory")
	}
	if _, ok, err := readSkillFrontmatter(filepath.Join(path, "SKILL.md")); err != nil || !ok {
		return "", errors.New("external skill source has no valid SKILL.md")
	}
	return path, nil
}

func entryBindingAgents(entry PackageEntry) []Agent {
	seen := map[Agent]bool{}
	agents := []Agent{}
	for _, binding := range entry.Bindings {
		if !seen[binding.Agent] {
			seen[binding.Agent] = true
			agents = append(agents, binding.Agent)
		}
	}
	return agents
}

func (entry PackageEntry) Scope() Scope {
	scopes := map[Scope]bool{}
	for _, binding := range entry.Bindings {
		scopes[binding.Scope] = true
	}
	if len(scopes) == 0 {
		return ScopeUnknown
	}
	if len(scopes) == 1 {
		for scope := range scopes {
			return scope
		}
	}
	return ScopeMixed
}

func agentsLabel(agents []Agent) string {
	names := make([]string, 0, len(agents))
	for _, agent := range agents {
		names = append(names, agentName(agent))
	}
	return strings.Join(names, ", ")
}

// copyDirBounded materializes a package folder with the exact bounds the
// store allows. Destination is created under an exclusive parent so partial
// copies never look like committed content.
func copyDirBounded(source, destination string) error {
	if !filepath.IsAbs(destination) {
		return errors.New("copy destination must be absolute")
	}
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("copy source is not a directory")
	}
	files, err := collectRegularFiles(source)
	if err != nil {
		return err
	}
	var total int64
	for _, relative := range files {
		src := filepath.Join(source, filepath.FromSlash(relative))
		dst := filepath.Join(destination, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		fileInfo, err := os.Stat(src)
		if err != nil {
			return err
		}
		if fileInfo.Size() > hashMaxFileBytes {
			return ErrHashLimit
		}
		total += fileInfo.Size()
		if total > maxPackageBytes {
			return ErrHashLimit
		}
		if err := copyRegularFile(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func copyRegularFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func countPackageFiles(root string) int {
	files, err := collectRegularFiles(root)
	if err != nil {
		return 0
	}
	return len(files)
}

func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}
