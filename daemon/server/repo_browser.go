package server

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type gitRepoBrowserPayload struct {
	RepoRoot string                `json:"repo_root"`
	Path     string                `json:"path"`
	Entries  []gitRepoBrowserEntry `json:"entries"`
}

type gitRepoBrowserEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type gitRepoFileContentPayload struct {
	RepoRoot string                 `json:"repo_root"`
	Path     string                 `json:"path"`
	Snapshot gitDiffContentSnapshot `json:"snapshot"`
}

func (s *Server) buildGitRepoEntries(targetID, cwd, path string) (gitRepoBrowserPayload, error) {
	repoRoot, reason, err := s.resolveGitRepoRoot(targetID, cwd)
	if err != nil {
		return gitRepoBrowserPayload{}, err
	}
	if reason == gitDiffReasonNoCwd {
		return gitRepoBrowserPayload{}, fmt.Errorf("repository browser is unavailable because this terminal has no cwd")
	}
	if reason == gitDiffReasonNotGitRepo {
		return gitRepoBrowserPayload{}, fmt.Errorf("current cwd is not inside a git repository")
	}

	relativePath, absolutePath, err := resolveRepoBrowserPath(repoRoot, path)
	if err != nil {
		return gitRepoBrowserPayload{}, err
	}

	info, err := os.Stat(absolutePath)
	if err != nil {
		return gitRepoBrowserPayload{}, fmt.Errorf("open repository path: %w", err)
	}
	if !info.IsDir() {
		return gitRepoBrowserPayload{}, fmt.Errorf("repository path is not a directory: %s", relativePath)
	}

	rawEntries, err := os.ReadDir(absolutePath)
	if err != nil {
		return gitRepoBrowserPayload{}, fmt.Errorf("list repository path: %w", err)
	}

	entries := make([]gitRepoBrowserEntry, 0, len(rawEntries))
	for _, entry := range rawEntries {
		name := entry.Name()
		if name == ".git" {
			continue
		}

		entryPath := name
		if relativePath != "" {
			entryPath = relativePath + "/" + name
		}

		kind := "file"
		if entry.IsDir() {
			kind = "directory"
		}
		entries = append(entries, gitRepoBrowserEntry{
			Name: name,
			Path: entryPath,
			Kind: kind,
		})
	}

	sort.Slice(entries, func(left, right int) bool {
		if entries[left].Kind != entries[right].Kind {
			return entries[left].Kind == "directory"
		}
		return strings.ToLower(entries[left].Name) < strings.ToLower(entries[right].Name)
	})

	return gitRepoBrowserPayload{
		RepoRoot: repoRoot,
		Path:     relativePath,
		Entries:  entries,
	}, nil
}

func (s *Server) buildGitRepoFileContent(targetID, cwd, path string) (gitRepoFileContentPayload, error) {
	repoRoot, reason, err := s.resolveGitRepoRoot(targetID, cwd)
	if err != nil {
		return gitRepoFileContentPayload{}, err
	}
	if reason == gitDiffReasonNoCwd {
		return gitRepoFileContentPayload{}, fmt.Errorf("repository browser is unavailable because this terminal has no cwd")
	}
	if reason == gitDiffReasonNotGitRepo {
		return gitRepoFileContentPayload{}, fmt.Errorf("current cwd is not inside a git repository")
	}

	relativePath, absolutePath, err := resolveRepoBrowserPath(repoRoot, path)
	if err != nil {
		return gitRepoFileContentPayload{}, err
	}
	if relativePath == "" {
		return gitRepoFileContentPayload{}, fmt.Errorf("repository file path is required")
	}

	info, err := os.Stat(absolutePath)
	if err != nil {
		return gitRepoFileContentPayload{}, fmt.Errorf("open repository file: %w", err)
	}
	if info.IsDir() {
		return gitRepoFileContentPayload{}, fmt.Errorf("repository path is a directory: %s", relativePath)
	}

	content, err := os.ReadFile(absolutePath)
	if err != nil {
		return gitRepoFileContentPayload{}, fmt.Errorf("read repository file: %w", err)
	}

	return gitRepoFileContentPayload{
		RepoRoot: repoRoot,
		Path:     relativePath,
		Snapshot: buildGitDiffContentSnapshot("Working tree", content),
	}, nil
}

func resolveRepoBrowserPath(repoRoot, rawPath string) (relativePath string, absolutePath string, err error) {
	cleanRoot := filepath.Clean(repoRoot)
	trimmed := strings.TrimSpace(rawPath)

	if trimmed == "" || trimmed == "." || trimmed == "/" {
		return "", cleanRoot, nil
	}

	candidate := filepath.FromSlash(trimmed)
	if filepath.IsAbs(candidate) {
		absolutePath = filepath.Clean(candidate)
	} else {
		absolutePath = filepath.Join(cleanRoot, candidate)
	}

	relativePath, err = filepath.Rel(cleanRoot, absolutePath)
	if err != nil {
		return "", "", fmt.Errorf("resolve repository path: %w", err)
	}

	relativePath = filepath.Clean(relativePath)
	if relativePath == "." {
		return "", cleanRoot, nil
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("repository path escapes root")
	}

	return filepath.ToSlash(relativePath), absolutePath, nil
}
