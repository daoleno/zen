package skills

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const deleteTrashDir = ".zen-trash"

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
	if err := ctx.Err(); err != nil {
		return err
	}
	allowed, err := os.OpenRoot(copy.AllowedRoot)
	if err != nil {
		return fmt.Errorf("open allowed Skills root: %w", err)
	}
	defer allowed.Close()
	entryName := filepath.Base(copy.RootPath)
	before, err := allowed.Lstat(entryName)
	if err != nil {
		return fmt.Errorf("the selected Skill copy is no longer available: %w", err)
	}
	if !before.IsDir() && before.Mode()&os.ModeSymlink == 0 {
		return errors.New("the selected Skill root is neither a directory nor a directory link")
	}
	resolved, err := filepath.EvalSymlinks(copy.RootPath)
	if err != nil {
		return fmt.Errorf("resolve selected Skill root: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil || filepath.Clean(resolved) != copy.CanonicalPath {
		return errors.New("the selected Skill root changed after discovery")
	}
	if before.Mode()&os.ModeSymlink == 0 {
		resolvedAllowed, rootErr := filepath.EvalSymlinks(copy.AllowedRoot)
		if rootErr != nil {
			return fmt.Errorf("resolve allowed Skills root: %w", rootErr)
		}
		resolvedAllowed, rootErr = filepath.Abs(resolvedAllowed)
		if rootErr != nil || filepath.Dir(copy.CanonicalPath) != filepath.Clean(resolvedAllowed) {
			return errors.New("the selected Skill directory escaped its resolved allowed root")
		}
	}
	if hooks != nil && hooks.beforeRename != nil {
		if err := hooks.beforeRename(copy); err != nil {
			return err
		}
	}
	if err := ensureTrashDirectory(allowed); err != nil {
		return err
	}
	quarantine, err := quarantinePath(copy)
	if err != nil {
		return err
	}
	if err := allowed.Rename(entryName, quarantine); err != nil {
		return fmt.Errorf("move selected Skill for deletion: %w", err)
	}
	rollback := func(cause error) error {
		if _, statErr := allowed.Lstat(entryName); errors.Is(statErr, os.ErrNotExist) {
			if renameErr := allowed.Rename(quarantine, entryName); renameErr != nil {
				return fmt.Errorf("%v; restore selected Skill: %w", cause, renameErr)
			}
		}
		return cause
	}
	moved, err := allowed.Lstat(quarantine)
	if err != nil || !os.SameFile(before, moved) {
		return rollback(errors.New("selected Skill identity changed during deletion"))
	}
	if hooks != nil && hooks.afterRename != nil {
		if err := hooks.afterRename(copy); err != nil {
			return rollback(err)
		}
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	removeAll := func(root *os.Root, name string) error { return root.RemoveAll(name) }
	if hooks != nil && hooks.removeAll != nil {
		removeAll = hooks.removeAll
	}
	if err := removeAll(allowed, quarantine); err != nil {
		return rollback(fmt.Errorf("permanently delete selected Skill: %w", err))
	}
	_ = allowed.Remove(deleteTrashDir)
	return nil
}

func ensureTrashDirectory(root *os.Root) error {
	info, err := root.Lstat(deleteTrashDir)
	if errors.Is(err, os.ErrNotExist) {
		if err := root.Mkdir(deleteTrashDir, 0o700); err != nil {
			return fmt.Errorf("create Skills deletion staging directory: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Skills deletion staging directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Skills deletion staging path is not a safe directory")
	}
	return nil
}

func quarantinePath(copy InstalledSkill) (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("create deletion identity: %w", err)
	}
	return filepath.Join(deleteTrashDir, copy.Name+"-"+copy.ID+"-"+hex.EncodeToString(random)), nil
}
