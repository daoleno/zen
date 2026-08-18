package skills

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const deleteTrashDir = ".zen-trash"

type exactDirectoryEntry struct {
	Kind          string
	RootPath      string
	CanonicalPath string
	AllowedRoot   string
	EntryName     string
	Identity      string
	AllowSymlink  bool
}

type exactDeleteHooks struct {
	beforeRename func() error
	afterRename  func() error
	removeAll    func(*os.Root, string) error
}

func deleteExactDirectoryEntry(ctx context.Context, entry exactDirectoryEntry, hooks *exactDeleteHooks) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	allowed, err := os.OpenRoot(entry.AllowedRoot)
	if err != nil {
		return fmt.Errorf("open allowed %s root: %w", entry.Kind, err)
	}
	defer allowed.Close()

	before, err := allowed.Lstat(entry.EntryName)
	if err != nil {
		return fmt.Errorf("the selected %s copy is no longer available: %w", entry.Kind, err)
	}
	isSymlink := before.Mode()&os.ModeSymlink != 0
	if !before.IsDir() && (!entry.AllowSymlink || !isSymlink) {
		return fmt.Errorf("the selected %s root is not a safe directory", entry.Kind)
	}
	if isSymlink && !entry.AllowSymlink {
		return fmt.Errorf("the selected %s root contains unsupported symlink traversal", entry.Kind)
	}

	resolved, err := filepath.EvalSymlinks(entry.RootPath)
	if err != nil {
		return fmt.Errorf("resolve selected %s root: %w", entry.Kind, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil || filepath.Clean(resolved) != entry.CanonicalPath {
		return fmt.Errorf("the selected %s root changed after discovery", entry.Kind)
	}
	if !isSymlink {
		resolvedAllowed, rootErr := filepath.EvalSymlinks(entry.AllowedRoot)
		if rootErr != nil {
			return fmt.Errorf("resolve allowed %s root: %w", entry.Kind, rootErr)
		}
		resolvedAllowed, rootErr = filepath.Abs(resolvedAllowed)
		if rootErr != nil || filepath.Dir(entry.CanonicalPath) != filepath.Clean(resolvedAllowed) {
			return fmt.Errorf("the selected %s directory escaped its resolved allowed root", entry.Kind)
		}
	}

	if hooks != nil && hooks.beforeRename != nil {
		if err := hooks.beforeRename(); err != nil {
			return err
		}
	}
	if err := ensureExactTrashDirectory(allowed, entry.Kind); err != nil {
		return err
	}
	quarantine, err := exactQuarantinePath(entry)
	if err != nil {
		return err
	}
	if err := allowed.Rename(entry.EntryName, quarantine); err != nil {
		return fmt.Errorf("move selected %s for deletion: %w", entry.Kind, err)
	}
	rollback := func(cause error) error {
		if _, statErr := allowed.Lstat(entry.EntryName); errors.Is(statErr, os.ErrNotExist) {
			if renameErr := allowed.Rename(quarantine, entry.EntryName); renameErr != nil {
				return fmt.Errorf("%v; restore selected %s: %w", cause, entry.Kind, renameErr)
			}
		}
		return cause
	}
	moved, err := allowed.Lstat(quarantine)
	if err != nil || !os.SameFile(before, moved) {
		return rollback(fmt.Errorf("selected %s identity changed during deletion", entry.Kind))
	}
	if hooks != nil && hooks.afterRename != nil {
		if err := hooks.afterRename(); err != nil {
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
		return rollback(fmt.Errorf("permanently delete selected %s: %w", entry.Kind, err))
	}
	_ = allowed.Remove(deleteTrashDir)
	return nil
}

func ensureExactTrashDirectory(root *os.Root, kind string) error {
	info, err := root.Lstat(deleteTrashDir)
	if errors.Is(err, os.ErrNotExist) {
		if err := root.Mkdir(deleteTrashDir, 0o700); err != nil {
			return fmt.Errorf("create %s deletion staging directory: %w", kind, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s deletion staging directory: %w", kind, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s deletion staging path is not a safe directory", kind)
	}
	return nil
}

func exactQuarantinePath(entry exactDirectoryEntry) (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("create deletion identity: %w", err)
	}
	return filepath.Join(deleteTrashDir, entry.EntryName+"-"+entry.Identity+"-"+hex.EncodeToString(random)), nil
}
