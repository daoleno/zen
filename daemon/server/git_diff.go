package server

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	gitDiffReasonNoCwd      = "no_cwd"
	gitDiffReasonNotGitRepo = "not_git_repo"
)

var shortstatCountPattern = regexp.MustCompile(`(\d+)\s+(insertion|deletion)`)

type gitDiffStatusPayload struct {
	Available          bool              `json:"available"`
	Reason             string            `json:"reason,omitempty"`
	RepoRoot           string            `json:"repo_root,omitempty"`
	RepoName           string            `json:"repo_name,omitempty"`
	Branch             string            `json:"branch,omitempty"`
	Clean              bool              `json:"clean"`
	FileCount          int               `json:"file_count"`
	StagedFileCount    int               `json:"staged_file_count"`
	UnstagedFileCount  int               `json:"unstaged_file_count"`
	UntrackedFileCount int               `json:"untracked_file_count"`
	Additions          int               `json:"additions"`
	Deletions          int               `json:"deletions"`
	Files              []gitDiffFileInfo `json:"files,omitempty"`
}

type gitDiffFileInfo struct {
	Path      string `json:"path"`
	OldPath   string `json:"old_path,omitempty"`
	Status    string `json:"status"`
	Staged    bool   `json:"staged"`
	Unstaged  bool   `json:"unstaged"`
	Untracked bool   `json:"untracked"`
}

type gitDiffPatchPayload struct {
	RepoRoot string                `json:"repo_root"`
	Path     string                `json:"path"`
	Sections []gitDiffPatchSection `json:"sections"`
}

type gitDiffPatchSection struct {
	Scope string `json:"scope"`
	Title string `json:"title"`
	Patch string `json:"patch"`
}

func (s *Server) buildGitDiffStatus(targetID, cwd string) (gitDiffStatusPayload, error) {
	repoRoot, reason, err := s.resolveGitRepoRoot(targetID, cwd)
	if err != nil {
		return gitDiffStatusPayload{}, err
	}
	if reason != "" {
		return gitDiffStatusPayload{
			Available: false,
			Reason:    reason,
			Clean:     true,
		}, nil
	}

	files, err := listGitDiffFiles(repoRoot)
	if err != nil {
		return gitDiffStatusPayload{}, err
	}

	additions, deletions, err := gitDiffTotals(repoRoot)
	if err != nil {
		return gitDiffStatusPayload{}, err
	}

	payload := gitDiffStatusPayload{
		Available: true,
		RepoRoot:  repoRoot,
		RepoName:  filepath.Base(repoRoot),
		Branch:    gitBranchName(repoRoot),
		Clean:     len(files) == 0,
		FileCount: len(files),
		Additions: additions,
		Deletions: deletions,
		Files:     files,
	}

	for _, file := range files {
		if file.Staged {
			payload.StagedFileCount += 1
		}
		if file.Unstaged {
			payload.UnstagedFileCount += 1
		}
		if file.Untracked {
			payload.UntrackedFileCount += 1
		}
	}

	return payload, nil
}

func (s *Server) buildGitDiffPatch(targetID, cwd, path string) (gitDiffPatchPayload, error) {
	repoRoot, reason, err := s.resolveGitRepoRoot(targetID, cwd)
	if err != nil {
		return gitDiffPatchPayload{}, err
	}
	if reason == gitDiffReasonNoCwd {
		return gitDiffPatchPayload{}, fmt.Errorf("git diff is unavailable because this terminal has no cwd")
	}
	if reason == gitDiffReasonNotGitRepo {
		return gitDiffPatchPayload{}, fmt.Errorf("current cwd is not inside a git repository")
	}

	targetPath := strings.TrimSpace(path)
	if targetPath == "" {
		return gitDiffPatchPayload{}, fmt.Errorf("git diff file path is required")
	}

	files, err := listGitDiffFiles(repoRoot)
	if err != nil {
		return gitDiffPatchPayload{}, err
	}

	var file *gitDiffFileInfo
	for index := range files {
		if files[index].Path == targetPath {
			file = &files[index]
			break
		}
	}
	if file == nil {
		return gitDiffPatchPayload{}, fmt.Errorf("git diff file not found: %s", targetPath)
	}

	sections := make([]gitDiffPatchSection, 0, 3)
	if file.Staged {
		patch, err := gitCommandOutput(
			repoRoot,
			false,
			"diff",
			"--cached",
			"--no-ext-diff",
			"--find-renames",
			"--submodule=diff",
			"--",
			file.Path,
		)
		if err != nil {
			return gitDiffPatchPayload{}, err
		}
		if strings.TrimSpace(patch) != "" {
			sections = append(sections, gitDiffPatchSection{
				Scope: "staged",
				Title: "Staged",
				Patch: strings.TrimRight(patch, "\n"),
			})
		}
	}

	if file.Unstaged {
		patch, err := gitCommandOutput(
			repoRoot,
			false,
			"diff",
			"--no-ext-diff",
			"--find-renames",
			"--submodule=diff",
			"--",
			file.Path,
		)
		if err != nil {
			return gitDiffPatchPayload{}, err
		}
		if strings.TrimSpace(patch) != "" {
			sections = append(sections, gitDiffPatchSection{
				Scope: "unstaged",
				Title: "Unstaged",
				Patch: strings.TrimRight(patch, "\n"),
			})
		}
	}

	if file.Untracked {
		patch, err := gitCommandOutput(
			repoRoot,
			true,
			"diff",
			"--no-index",
			"--no-ext-diff",
			"--",
			"/dev/null",
			file.Path,
		)
		if err != nil {
			return gitDiffPatchPayload{}, err
		}
		if strings.TrimSpace(patch) != "" {
			sections = append(sections, gitDiffPatchSection{
				Scope: "untracked",
				Title: "Untracked",
				Patch: strings.TrimRight(patch, "\n"),
			})
		}
	}

	if len(sections) == 0 {
		return gitDiffPatchPayload{}, fmt.Errorf("no diff available for %s", targetPath)
	}

	return gitDiffPatchPayload{
		RepoRoot: repoRoot,
		Path:     file.Path,
		Sections: sections,
	}, nil
}

func (s *Server) resolveGitRepoRoot(targetID, cwd string) (repoRoot string, reason string, err error) {
	resolvedCwd := strings.TrimSpace(cwd)
	if resolvedCwd == "" && targetID != "" {
		if agent := s.watcher.GetAgent(targetID); agent != nil {
			resolvedCwd = strings.TrimSpace(agent.Cwd)
		}
	}
	if resolvedCwd == "" {
		return "", gitDiffReasonNoCwd, nil
	}

	root, gitErr := gitOutput(resolvedCwd, "rev-parse", "--show-toplevel")
	if gitErr != nil {
		if strings.Contains(strings.ToLower(gitErr.Error()), "not a git repository") {
			return "", gitDiffReasonNotGitRepo, nil
		}
		return "", "", fmt.Errorf("resolve git repo root: %w", gitErr)
	}
	return strings.TrimSpace(root), "", nil
}

func gitBranchName(repoRoot string) string {
	if branch, err := gitOutput(repoRoot, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		if branch = strings.TrimSpace(branch); branch != "" {
			return branch
		}
	}
	if sha, err := gitOutput(repoRoot, "rev-parse", "--short", "HEAD"); err == nil {
		return strings.TrimSpace(sha)
	}
	return ""
}

func gitDiffTotals(repoRoot string) (additions int, deletions int, err error) {
	stagedAdditions, stagedDeletions, err := gitShortstat(repoRoot, true)
	if err != nil {
		return 0, 0, err
	}
	unstagedAdditions, unstagedDeletions, err := gitShortstat(repoRoot, false)
	if err != nil {
		return 0, 0, err
	}
	return stagedAdditions + unstagedAdditions, stagedDeletions + unstagedDeletions, nil
}

func gitShortstat(repoRoot string, staged bool) (additions int, deletions int, err error) {
	args := []string{"diff", "--shortstat", "--no-ext-diff"}
	if staged {
		args = append(args, "--cached")
	}

	out, err := gitOutput(repoRoot, args...)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no changes") {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	return parseShortstatCounts(out), parseShortstatDeletions(out), nil
}

func parseShortstatCounts(raw string) int {
	count := 0
	for _, match := range shortstatCountPattern.FindAllStringSubmatch(raw, -1) {
		if len(match) < 3 || !strings.HasPrefix(match[2], "insertion") {
			continue
		}
		value, _ := strconv.Atoi(match[1])
		count += value
	}
	return count
}

func parseShortstatDeletions(raw string) int {
	count := 0
	for _, match := range shortstatCountPattern.FindAllStringSubmatch(raw, -1) {
		if len(match) < 3 || !strings.HasPrefix(match[2], "deletion") {
			continue
		}
		value, _ := strconv.Atoi(match[1])
		count += value
	}
	return count
}

func listGitDiffFiles(repoRoot string) ([]gitDiffFileInfo, error) {
	out, err := gitOutput(repoRoot, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}

	files := make([]gitDiffFileInfo, 0)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 3 {
			continue
		}

		x := line[0]
		y := line[1]
		if x == '!' && y == '!' {
			continue
		}

		path, oldPath := parseGitStatusPath(line[3:])
		if path == "" {
			continue
		}

		untracked := x == '?' && y == '?'
		files = append(files, gitDiffFileInfo{
			Path:      path,
			OldPath:   oldPath,
			Status:    gitDiffStatusName(x, y),
			Staged:    !untracked && x != ' ',
			Unstaged:  !untracked && y != ' ',
			Untracked: untracked,
		})
	}

	sort.Slice(files, func(left, right int) bool {
		return files[left].Path < files[right].Path
	})

	return files, nil
}

func parseGitStatusPath(raw string) (path string, oldPath string) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", ""
	}

	if strings.Contains(value, " -> ") {
		parts := strings.SplitN(value, " -> ", 2)
		if len(parts) == 2 {
			return unquoteGitPath(parts[1]), unquoteGitPath(parts[0])
		}
	}

	return unquoteGitPath(value), ""
}

func unquoteGitPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "\"") {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
	}
	return value
}

func gitDiffStatusName(x, y byte) string {
	if x == '?' && y == '?' {
		return "untracked"
	}
	if x == 'U' || y == 'U' {
		return "conflict"
	}
	if x == 'R' || y == 'R' {
		return "renamed"
	}
	if x == 'C' || y == 'C' {
		return "copied"
	}
	if x == 'A' || y == 'A' {
		return "added"
	}
	if x == 'D' || y == 'D' {
		return "deleted"
	}
	if x == 'M' || y == 'M' {
		return "modified"
	}
	return "changed"
}

func gitCommandOutput(repoRoot string, allowDiffExitCode bool, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if allowDiffExitCode && errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return string(out), nil
		}
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
