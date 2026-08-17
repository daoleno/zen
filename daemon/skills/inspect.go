package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// fetchGitSkill stages a catalog Skill by cloning its pinned provenance into
// stageDir and locating the skill directory by name. It is the default
// SourceFetcher; tests inject hermetic writers instead.
func fetchGitSkill(ctx context.Context, request MutationRequest, stageDir string) error {
	if err := ValidateCatalogIdentity(request.SkillID, request.Source, request.SkillName); err != nil {
		return err
	}
	ref := strings.TrimSpace(request.Ref)
	if ref != "" {
		if err := ValidateRef(ref); err != nil {
			return err
		}
	}
	if _, err := os.Stat(stageDir); err != nil {
		return err
	}
	cloneArgs := []string{"clone", "--depth", "1"}
	if ref != "" {
		cloneArgs = append(cloneArgs, "--branch", ref)
	}
	cloneArgs = append(cloneArgs, "https://github.com/"+request.Source+".git", stageDir)
	if err := runGit(ctx, cloneArgs...); err != nil {
		return fmt.Errorf("fetch skill source: %w", err)
	}
	// Locate the skill directory: <repo>/skills/<name> (the skills-sh
	// convention) or <repo>/<name> when the whole repo is the skill.
	found := filepath.Join(stageDir, "skills", request.SkillName)
	if _, err := os.Stat(filepath.Join(found, "SKILL.md")); err != nil {
		found = filepath.Join(stageDir, request.SkillName)
	}
	if _, err := os.Stat(filepath.Join(found, "SKILL.md")); err != nil {
		return fmt.Errorf("repository %s contains no skill named %q", request.Source, request.SkillName)
	}
	return nil
}

func runGit(ctx context.Context, args ...string) error {
	path, err := exec.LookPath("git")
	if err != nil {
		return ErrMutationBinaryMissing
	}
	command := exec.CommandContext(ctx, path, args...)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 240 {
			message = message[:240]
		}
		return errors.New(message)
	}
	return nil
}

// PackageFile is one entry of the bounded file listing used by inspect.
type PackageFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Mode string `json:"mode"`
}

// PackageDetail is the full inspection surface for one Skill: rendered
// SKILL.md content, bounded file listing, provenance, hash, bindings, enabled
// state, and static risk signals. It is the payload behind the inspector UI.
type PackageDetail struct {
	SkillName   string               `json:"skill_name"`
	Description string               `json:"description,omitempty"`
	Manager     Manager              `json:"manager"`
	Owned       bool                 `json:"owned"`
	Tracked     bool                 `json:"tracked"`
	Enabled     bool                 `json:"enabled"`
	Canonical   string               `json:"canonical_path,omitempty"`
	SourcePath  string               `json:"source_path,omitempty"`
	Source      string               `json:"source,omitempty"`
	SourceType  string               `json:"source_type,omitempty"`
	SourceURL   string               `json:"source_url,omitempty"`
	Ref         string               `json:"ref,omitempty"`
	ContentHash string               `json:"content_hash,omitempty"`
	InstalledAt string               `json:"installed_at,omitempty"`
	UpdatedAt   string               `json:"updated_at,omitempty"`
	Scope       Scope                `json:"scope"`
	Agents      []Agent              `json:"agents"`
	Bindings    []SkillBinding       `json:"bindings"`
	Files       []PackageFile        `json:"files,omitempty"`
	SKILLMD     string               `json:"skill_md,omitempty"`
	FilePath    string               `json:"file_path,omitempty"`
	FileContent string               `json:"file_content,omitempty"`
	Risk        []RiskSignal         `json:"risk,omitempty"`
	Warnings    []string             `json:"warnings,omitempty"`
	Capability  ManagementCapability `json:"capability"`
}

// InspectPackageFile extends package detail with one bounded, read-only text
// file. Traversal and symlinks are rejected before content is read.
func InspectPackageFile(options InventoryOptions, name, relative string) (PackageDetail, error) {
	detail, err := InspectPackage(options, name)
	if err != nil {
		return PackageDetail{}, err
	}
	relative = filepath.ToSlash(filepath.Clean(relative))
	if relative == "." || filepath.IsAbs(relative) || strings.HasPrefix(relative, "../") {
		return PackageDetail{}, errors.New("invalid Skill file path")
	}
	root := detail.Canonical
	if root == "" {
		root = detail.SourcePath
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return PackageDetail{}, errors.New("Skill file is unavailable or not a regular file")
	}
	content, ok, err := readTextFileBounded(path, maxInspectSKILLBytes)
	if err != nil {
		return PackageDetail{}, fmt.Errorf("read Skill file: %w", err)
	}
	if !ok {
		return PackageDetail{}, errors.New("Skill file exceeds the inspection size limit")
	}
	detail.FilePath = relative
	detail.FileContent = content
	return detail, nil
}

const (
	maxInspectSKILLBytes = 64 << 10
	maxInspectFiles      = 128
)

// InspectPackage builds the inspection detail for one Skill name. It reads
// the central inventory plus the agent surfaces and never mutates state.
func InspectPackage(options InventoryOptions, name string) (PackageDetail, error) {
	if err := ValidateSkillName(name); err != nil {
		return PackageDetail{}, err
	}
	normalized, err := normalizeInventoryOptions(options)
	if err != nil {
		return PackageDetail{}, err
	}
	store := Store{StateDir: normalized.ZenStateDir, Home: normalized.Home, Now: normalized.Now}
	inventory, err := store.LoadInventory(false)
	if err != nil {
		return PackageDetail{}, err
	}
	detail := PackageDetail{SkillName: name}
	if entry, ok := inventory.Packages[name]; ok {
		detail.Tracked = true
		detail.Owned = entry.Owned
		detail.Source = entry.Source
		detail.SourceType = entry.SourceType
		detail.SourceURL = entry.SourceURL
		detail.Ref = entry.Ref
		detail.ContentHash = entry.ContentHash
		detail.InstalledAt = entry.InstalledAt
		detail.UpdatedAt = entry.UpdatedAt
		detail.Description = entry.Description
		detail.Agents = entryBindingAgents(entry)
		detail.Scope = entry.Scope()
		for _, binding := range entry.Bindings {
			detail.Bindings = append(detail.Bindings, SkillBinding{
				Agent: binding.Agent, Scope: binding.Scope,
				Mode: string(binding.Mode), TargetPath: binding.TargetPath,
				SourcePath: binding.TargetPath, Enabled: binding.Enabled,
				BoundAt: binding.BoundAt, Note: binding.Note,
				Operations: bindingOperations(binding),
			})
			if binding.Enabled {
				detail.Enabled = true
			}
		}
		if entry.Owned {
			detail.Manager = ManagerZen
			detail.Canonical = store.PackageDir(name)
			detail.Capability = ManagementCapability{CanManage: true, Operations: ownedPackageOperations(entry)}
		} else {
			detail.Manager = ManagerExternal
			detail.SourcePath = entry.Source
			if _, sourceErr := trackedExternalDir(entry); sourceErr != nil {
				detail.Capability = ManagementCapability{
					CanManage: true, Operations: []MutationOperation{OperationForget}, Reason: sourceErr.Error(),
				}
				detail.Warnings = append(detail.Warnings, sourceErr.Error())
			} else {
				detail.Capability = ManagementCapability{CanManage: true, Operations: trackedExternalCapabilities()}
			}
		}
	} else {
		// Untracked external: discover from agent surfaces.
		surface, found := discoverExternalSurface(normalized, name)
		if !found {
			return PackageDetail{}, fmt.Errorf("no Skill named %q is installed or tracked", name)
		}
		detail.Manager = ManagerExternal
		detail.SourcePath = surface.dir
		detail.Source = surface.dir
		detail.SourceType = string(SourceTypeExternal)
		detail.Scope = surface.scope
		detail.Agents = append([]Agent{}, surface.agents...)
		hash, err := folderContentHash(surface.dir)
		if err != nil {
			return PackageDetail{}, err
		}
		detail.ContentHash = hash
		detail.Capability = ManagementCapability{CanManage: true, Operations: []MutationOperation{OperationAdopt, OperationMigrate}}
	}
	contentRoot := detail.Canonical
	if contentRoot == "" {
		contentRoot = detail.SourcePath
	}
	if contentRoot != "" {
		content, ok, err := readTextFileBounded(filepath.Join(contentRoot, "SKILL.md"), maxInspectSKILLBytes)
		if err == nil && ok {
			detail.SKILLMD = content
		} else if err != nil {
			detail.Warnings = append(detail.Warnings, "Could not read SKILL.md: "+err.Error())
		} else {
			detail.Warnings = append(detail.Warnings, "SKILL.md exceeds the inspection size limit.")
		}
		detail.Files = scanPackageFiles(contentRoot)
		detail.Risk = scanRiskSignals(contentRoot)
	}
	return detail, nil
}

func trackedExternalCapabilities() []MutationOperation {
	return []MutationOperation{OperationAdopt, OperationForget}
}
func scanPackageFiles(root string) []PackageFile {
	files, err := collectRegularFiles(root)
	if err != nil {
		return nil
	}
	if len(files) > maxInspectFiles {
		files = files[:maxInspectFiles]
	}
	out := make([]PackageFile, 0, len(files))
	for _, relative := range files {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			continue
		}
		out = append(out, PackageFile{
			Path: relative, Size: info.Size(), Mode: fmt.Sprintf("%04o", info.Mode().Perm()),
		})
	}
	return out
}

// discoverExternalSurface finds an untracked external Skill across the six
// Agent surfaces (global, then project when known).
func discoverExternalSurface(normalized InventoryOptions, name string) (migrationSource, bool) {
	for _, agent := range []Agent{AgentCodex, AgentClaudeCode, AgentCursor, AgentGrok, AgentOpenCode, AgentPi} {
		adapter, err := adapterFor(agent)
		if err != nil {
			continue
		}
		globalDir := filepath.Join(globalSkillsDir(adapter, normalized.Home, envResolverFor(normalized)), name)
		if _, err := os.Stat(filepath.Join(globalDir, "SKILL.md")); err == nil {
			return migrationSource{dir: globalDir, scope: ScopeGlobal, agents: []Agent{agent}}, true
		}
		if normalized.CWD != "" {
			projectDir := filepath.Join(projectSkillsDir(adapter, normalized.CWD), name)
			if _, err := os.Stat(filepath.Join(projectDir, "SKILL.md")); err == nil {
				return migrationSource{dir: projectDir, scope: ScopeProject, agents: []Agent{agent}}, true
			}
		}
	}
	return migrationSource{}, false
}
