package skills

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Content hash semantics: sha256 over the package's canonical folder, over
// sorted relative paths (slash-separated) with a NUL separator before each
// file's bytes. Symlinks inside packages are resolved by materialization and
// never stored, so hashing only regular files is deterministic.

const (
	hashMaxFileBytes  = 8 << 20
	hashNameSeparator = "\x00"
)

var ErrHashLimit = errors.New("package content exceeds the hash bound")

// folderContentHash computes the canonical hash of a skill folder, bounded by
// the same limits materialization enforces. Missing dirs and empty folders
// produce errors so a caller can never hash the wrong tree silently.
func folderContentHash(root string) (string, error) {
	hash := sha256.New()
	files, err := collectRegularFiles(root)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", errors.New("skill folder contains no files")
	}
	var totalBytes int64
	for _, relative := range files {
		path := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("skill file %q is not a regular file", relative)
		}
		if info.Size() > hashMaxFileBytes {
			return "", ErrHashLimit
		}
		totalBytes += info.Size()
		if totalBytes > maxPackageBytes {
			return "", ErrHashLimit
		}
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		if _, err := hash.Write([]byte(relative)); err != nil {
			_ = file.Close()
			return "", err
		}
		if _, err := hash.Write([]byte(hashNameSeparator)); err != nil {
			_ = file.Close()
			return "", err
		}
		if _, err := io.Copy(hash, file); err != nil {
			_ = file.Close()
			return "", err
		}
		_ = file.Close()
		if _, err := hash.Write([]byte{0}); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// collectRegularFiles returns sorted slash-separated relative paths of every
// regular file under root, following the materialization bounds and skipping
// nothing silently except dot-entries at the top level of an Agent binding
// (those are never package content).
func collectRegularFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			depth := strings.Count(filepath.ToSlash(relative), "/")
			if depth >= maxPackageDepth {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return errors.New("symlink inside a skill package is not allowed")
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if len(files) >= maxPackageFiles {
			return ErrHashLimit
		}
		files = append(files, relative)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// readTextFileBounded reads a file with a byte cap, returning its trimmed
// content and whether it fit the cap.
func readTextFileBounded(path string, limit int64) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return "", false, err
	}
	if int64(len(data)) > limit {
		return "", false, nil
	}
	return string(data), true, nil
}
