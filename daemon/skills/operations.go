package skills

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type deleteTestHooks struct {
	beforeRename func(InstalledSkill) error
	afterRename  func(InstalledSkill) error
	removeAll    func(*os.Root, string) error
}

func BuildMutationCommand(options InventoryOptions, request MutationRequest) (MutationCommand, error) {
	if request.Operation != OperationDelete {
		return MutationCommand{}, fmt.Errorf("unsupported Skill operation %q", request.Operation)
	}
	copy, err := resolveDeleteCopy(options, request)
	if err != nil {
		return MutationCommand{}, err
	}
	return commandForCopy(copy), nil
}

func commandForCopy(copy InstalledSkill) MutationCommand {
	return MutationCommand{
		Operation: OperationDelete, CopyID: copy.ID, SkillName: copy.Name,
		RootPath: copy.RootPath, CanonicalPath: copy.CanonicalPath,
		AllowedRoot: copy.AllowedRoot, Location: copy.Location,
		Scope: copy.Scope, Agents: append([]Agent{}, copy.Agents...),
		Summary:     "Delete " + copy.Name + " from " + copy.Location,
		Destructive: true,
	}
}

func resolveDeleteCopy(options InventoryOptions, request MutationRequest) (InstalledSkill, error) {
	if err := ValidateSkillName(request.SkillName); err != nil {
		return InstalledSkill{}, err
	}
	if !validInstalledSkillID(request.CopyID) {
		return InstalledSkill{}, errors.New("invalid Skill copy ID")
	}
	for field, value := range map[string]string{
		"root path":      request.RootPath,
		"canonical path": request.CanonicalPath,
		"allowed root":   request.AllowedRoot,
	} {
		if !filepath.IsAbs(value) || filepath.Clean(value) != value {
			return InstalledSkill{}, fmt.Errorf("invalid Skill %s", field)
		}
	}
	inventory, err := DiscoverInventory(options)
	if err != nil {
		return InstalledSkill{}, err
	}
	for _, copy := range inventory.Skills {
		if copy.ID != request.CopyID || copy.Name != request.SkillName {
			continue
		}
		if copy.RootPath != request.RootPath || copy.CanonicalPath != request.CanonicalPath || copy.AllowedRoot != request.AllowedRoot {
			return InstalledSkill{}, fmt.Errorf("the selected copy of Skill %q changed after it was loaded", request.SkillName)
		}
		if !copy.Capability.CanDelete {
			reason := strings.TrimSpace(copy.Capability.Reason)
			if reason == "" {
				reason = "this Skill copy cannot be deleted from here"
			}
			return InstalledSkill{}, errors.New(reason)
		}
		if err := validateDeleteIdentity(copy); err != nil {
			return InstalledSkill{}, err
		}
		return copy, nil
	}
	return InstalledSkill{}, fmt.Errorf("the selected copy of Skill %q is stale or no longer installed", request.SkillName)
}

func validateDeleteIdentity(copy InstalledSkill) error {
	if !filepath.IsAbs(copy.RootPath) || !filepath.IsAbs(copy.CanonicalPath) || !filepath.IsAbs(copy.AllowedRoot) {
		return errors.New("Skill copy identity is not absolute")
	}
	if filepath.Clean(copy.RootPath) != copy.RootPath || filepath.Clean(copy.CanonicalPath) != copy.CanonicalPath || filepath.Clean(copy.AllowedRoot) != copy.AllowedRoot {
		return errors.New("Skill copy identity is not canonical")
	}
	if copy.RootPath == copy.AllowedRoot {
		return errors.New("refusing to delete a Skills inventory root")
	}
	relative, err := filepath.Rel(copy.AllowedRoot, copy.RootPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.Dir(relative) != "." {
		return errors.New("Skill copy escaped its allowed root")
	}
	if relative != copy.Name || filepath.Base(copy.RootPath) != copy.Name {
		return errors.New("Skill copy name does not match its exact root")
	}
	if installedSkillID(copy.Name, copy.RootPath, copy.CanonicalPath, copy.AllowedRoot) != copy.ID {
		return errors.New("Skill copy identity does not match its roots")
	}
	return nil
}

func ExecuteMutationCommand(ctx context.Context, command MutationCommand, options MutationExecutionOptions) (MutationExecution, error) {
	if command.Operation != OperationDelete {
		return MutationExecution{}, fmt.Errorf("unsupported Skill operation %q", command.Operation)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultRemovalTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	startedAt := time.Now()
	request := MutationRequest{
		Operation: OperationDelete, CopyID: command.CopyID, SkillName: command.SkillName,
		RootPath: command.RootPath, CanonicalPath: command.CanonicalPath, AllowedRoot: command.AllowedRoot,
	}
	copy, err := resolveDeleteCopy(options.InventoryOptions, request)
	if err != nil {
		return MutationExecution{}, err
	}
	if err := deleteExactCopy(runCtx, copy, options.InventoryOptions.deleteHooks); err != nil {
		if errors.Is(runCtx.Err(), context.Canceled) {
			return MutationExecution{}, ErrMutationCancelled
		}
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return MutationExecution{}, ErrMutationTimedOut
		}
		return MutationExecution{}, err
	}
	return MutationExecution{
		Success: true, ExitCode: 0,
		Output:     "Deleted " + copy.Name + ".",
		DurationMS: time.Since(startedAt).Milliseconds(),
	}, nil
}

func deleteExactCopy(ctx context.Context, copy InstalledSkill, hooks *deleteTestHooks) error {
	if err := validateDeleteIdentity(copy); err != nil {
		return err
	}
	var exactHooks *exactDeleteHooks
	if hooks != nil {
		exactHooks = &exactDeleteHooks{
			beforeRename: func() error {
				if hooks.beforeRename == nil {
					return nil
				}
				return hooks.beforeRename(copy)
			},
			afterRename: func() error {
				if hooks.afterRename == nil {
					return nil
				}
				return hooks.afterRename(copy)
			},
			removeAll: hooks.removeAll,
		}
	}
	return deleteExactDirectoryEntry(ctx, exactDirectoryEntry{
		Kind: "Skill", RootPath: copy.RootPath, CanonicalPath: copy.CanonicalPath,
		AllowedRoot: copy.AllowedRoot, EntryName: filepath.Base(copy.RootPath),
		Identity: copy.ID, AllowSymlink: true,
	}, exactHooks)
}
