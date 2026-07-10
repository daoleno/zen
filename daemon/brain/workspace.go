package brain

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxWorkspaceEntries   = 2000
	maxWorkspaceFileBytes = 2 << 20
)

func (s *Store) WorkspaceTree(paths ...string) (WorkspaceTree, error) {
	if s == nil {
		return WorkspaceTree{}, fmt.Errorf("brain store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	root := s.WorkspacePath()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return WorkspaceTree{}, err
	}
	path := ""
	if len(paths) > 0 {
		path = paths[0]
	}
	relativePath, absolutePath, err := s.resolveWorkspacePathLocked(path)
	if err != nil {
		return WorkspaceTree{}, err
	}
	info, err := os.Stat(absolutePath)
	if err != nil {
		return WorkspaceTree{}, fmt.Errorf("open brain workspace directory: %w", err)
	}
	if !info.IsDir() {
		return WorkspaceTree{}, fmt.Errorf("brain workspace path is not a directory: %s", relativePath)
	}
	entries, err := s.workspaceEntriesLocked(absolutePath, relativePath)
	if err != nil {
		return WorkspaceTree{}, err
	}
	return WorkspaceTree{
		Workspace:   root,
		Path:        relativePath,
		Entries:     entries,
		GeneratedAt: time.Now().UTC(),
	}, nil
}

func (s *Store) ReadWorkspaceFile(path string) (WorkspaceFile, error) {
	if s == nil {
		return WorkspaceFile{}, fmt.Errorf("brain store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	relativePath, absolutePath, err := s.resolveWorkspacePathLocked(path)
	if err != nil {
		return WorkspaceFile{}, err
	}
	if relativePath == "" {
		return WorkspaceFile{}, fmt.Errorf("brain workspace file path is required")
	}

	info, err := os.Lstat(absolutePath)
	if err != nil {
		return WorkspaceFile{}, fmt.Errorf("open brain workspace file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return WorkspaceFile{}, fmt.Errorf("brain workspace file is a symlink: %s", relativePath)
	}
	if info.IsDir() {
		return WorkspaceFile{}, fmt.Errorf("brain workspace path is a directory: %s", relativePath)
	}
	if info.Size() > maxWorkspaceFileBytes {
		return WorkspaceFile{}, fmt.Errorf("brain workspace file is too large to preview")
	}

	content, err := os.ReadFile(absolutePath)
	if err != nil {
		return WorkspaceFile{}, fmt.Errorf("read brain workspace file: %w", err)
	}
	if !isTextWorkspaceContent(content) {
		return WorkspaceFile{}, fmt.Errorf("brain workspace file is not a text file")
	}

	return WorkspaceFile{
		Name:       filepath.Base(relativePath),
		Path:       relativePath,
		Kind:       "file",
		Language:   workspaceFileLanguage(relativePath),
		Content:    string(content),
		Size:       info.Size(),
		ModifiedAt: info.ModTime().UTC(),
	}, nil
}

func (s *Store) workspaceEntriesLocked(absolutePath, relativePath string) ([]WorkspaceEntry, error) {
	rawEntries, err := os.ReadDir(absolutePath)
	if err != nil {
		return nil, fmt.Errorf("list brain workspace path: %w", err)
	}

	entries := make([]WorkspaceEntry, 0, len(rawEntries))
	for _, entry := range rawEntries {
		if len(entries) >= maxWorkspaceEntries {
			return nil, fmt.Errorf("brain workspace contains too many entries to display")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		name := entry.Name()
		entryPath := name
		if relativePath != "" {
			entryPath = relativePath + "/" + name
		}
		entryPath = filepath.ToSlash(entryPath)

		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect brain workspace path %s: %w", entryPath, err)
		}

		item := WorkspaceEntry{
			Name:       name,
			Path:       entryPath,
			Kind:       "file",
			Size:       info.Size(),
			ModifiedAt: info.ModTime().UTC(),
		}
		if entry.IsDir() {
			item.Kind = "directory"
			item.Size = 0
		}
		entries = append(entries, item)
	}

	sort.Slice(entries, func(left, right int) bool {
		if entries[left].Kind != entries[right].Kind {
			return entries[left].Kind == "directory"
		}
		return strings.ToLower(entries[left].Name) < strings.ToLower(entries[right].Name)
	})

	return entries, nil
}

func (s *Store) resolveWorkspacePathLocked(rawPath string) (relativePath string, absolutePath string, err error) {
	root := filepath.Clean(s.WorkspacePath())
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" || trimmed == "." || trimmed == "/" {
		return "", root, nil
	}

	candidate := filepath.FromSlash(trimmed)
	if filepath.IsAbs(candidate) {
		return "", "", fmt.Errorf("brain workspace path must be relative")
	}

	absolutePath = filepath.Join(root, candidate)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve brain workspace root: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(absolutePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", "", fmt.Errorf("resolve brain workspace path: %w", err)
		}
	} else {
		resolvedRelative, err := filepath.Rel(resolvedRoot, resolvedPath)
		if err != nil {
			return "", "", fmt.Errorf("resolve brain workspace path: %w", err)
		}
		if resolvedRelative == ".." || strings.HasPrefix(resolvedRelative, ".."+string(filepath.Separator)) {
			return "", "", fmt.Errorf("brain workspace path escapes root")
		}
	}

	relativePath, err = filepath.Rel(root, absolutePath)
	if err != nil {
		return "", "", fmt.Errorf("resolve brain workspace path: %w", err)
	}
	relativePath = filepath.Clean(relativePath)
	if relativePath == "." {
		return "", root, nil
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("brain workspace path escapes root")
	}

	return filepath.ToSlash(relativePath), absolutePath, nil
}

func workspaceFileLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return "markdown"
	default:
		return "text"
	}
}

func isTextWorkspaceContent(content []byte) bool {
	if len(content) == 0 {
		return true
	}
	if bytes.Contains(content[:min(len(content), 4096)], []byte{0}) {
		return false
	}
	return utf8.Valid(content)
}
