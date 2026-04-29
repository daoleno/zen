package server

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	gitDiffReasonNoCwd      = "no_cwd"
	gitDiffReasonNotGitRepo = "not_git_repo"
	gitDiffContentMaxBytes  = 256 * 1024
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

type gitDiffFileContentPayload struct {
	RepoRoot string                 `json:"repo_root"`
	Path     string                 `json:"path"`
	Current  gitDiffContentSnapshot `json:"current"`
	Base     gitDiffContentSnapshot `json:"base"`
}

type gitDiffContentSnapshot struct {
	Label     string `json:"label"`
	Exists    bool   `json:"exists"`
	Binary    bool   `json:"binary,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Reason    string `json:"reason,omitempty"`
	ByteCount int    `json:"byte_count"`
	LineCount int    `json:"line_count"`
	Content   string `json:"content,omitempty"`
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
		patch, err := gitDiffPatchForFile(repoRoot, file.Path, true)
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
		patch, err := gitDiffPatchForFile(repoRoot, file.Path, false)
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

	return gitDiffPatchPayload{
		RepoRoot: repoRoot,
		Path:     file.Path,
		Sections: sections,
	}, nil
}

func (s *Server) buildGitDiffFileContent(targetID, cwd, path string) (gitDiffFileContentPayload, error) {
	repoRoot, reason, err := s.resolveGitRepoRoot(targetID, cwd)
	if err != nil {
		return gitDiffFileContentPayload{}, err
	}
	if reason == gitDiffReasonNoCwd {
		return gitDiffFileContentPayload{}, fmt.Errorf("git diff is unavailable because this terminal has no cwd")
	}
	if reason == gitDiffReasonNotGitRepo {
		return gitDiffFileContentPayload{}, fmt.Errorf("current cwd is not inside a git repository")
	}

	file, err := gitDiffTargetFile(repoRoot, path)
	if err != nil {
		return gitDiffFileContentPayload{}, err
	}

	current, err := workingTreeContentSnapshot(repoRoot, *file)
	if err != nil {
		return gitDiffFileContentPayload{}, err
	}

	base, err := baseContentSnapshot(repoRoot, *file)
	if err != nil {
		return gitDiffFileContentPayload{}, err
	}

	return gitDiffFileContentPayload{
		RepoRoot: repoRoot,
		Path:     file.Path,
		Current:  current,
		Base:     base,
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

func gitDiffTargetFile(repoRoot, path string) (*gitDiffFileInfo, error) {
	targetPath := strings.TrimSpace(path)
	if targetPath == "" {
		return nil, fmt.Errorf("git diff file path is required")
	}

	files, err := listGitDiffFiles(repoRoot)
	if err != nil {
		return nil, err
	}

	for index := range files {
		if files[index].Path == targetPath {
			return &files[index], nil
		}
	}

	return nil, fmt.Errorf("git diff file not found: %s", targetPath)
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

func gitLiteralPathspec(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "./")
	if path == "" {
		return ":(literal)."
	}
	return ":(literal)./" + path
}

func gitDiffPatchForFile(repoRoot, path string, staged bool) (string, error) {
	args := gitDiffPatchArgs(staged)
	args = append(args, "--", gitLiteralPathspec(path))
	patch, err := gitCommandOutput(repoRoot, false, args...)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(patch) != "" {
		return patch, nil
	}

	fullPatch, err := gitCommandOutput(repoRoot, false, gitDiffPatchArgs(staged)...)
	if err != nil {
		return "", err
	}
	if section, ok := extractGitDiffPatchForPath(fullPatch, path); ok {
		return section, nil
	}
	return patch, nil
}

func gitDiffPatchArgs(staged bool) []string {
	args := []string{
		"diff",
		"--no-ext-diff",
		"--find-renames",
		"--submodule=diff",
	}
	if staged {
		args = append(args, "--cached")
	}
	return args
}

func extractGitDiffPatchForPath(patch, path string) (string, bool) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "./")
	if strings.TrimSpace(patch) == "" || path == "" {
		return "", false
	}

	lines := strings.Split(patch, "\n")
	sectionStart := -1
	for index, line := range lines {
		if !strings.HasPrefix(line, "diff --git ") {
			continue
		}
		if sectionStart >= 0 && gitDiffSectionMatchesPath(lines[sectionStart:index], path) {
			return strings.TrimRight(strings.Join(lines[sectionStart:index], "\n"), "\n"), true
		}
		sectionStart = index
	}

	if sectionStart >= 0 && gitDiffSectionMatchesPath(lines[sectionStart:], path) {
		return strings.TrimRight(strings.Join(lines[sectionStart:], "\n"), "\n"), true
	}
	return "", false
}

func gitDiffSectionMatchesPath(lines []string, path string) bool {
	for _, line := range lines {
		for _, prefix := range []string{"--- ", "+++ ", "rename to ", "copy to "} {
			linePath, ok := gitDiffLinePath(line, prefix)
			if ok && linePath == path {
				return true
			}
		}

		if strings.HasPrefix(line, "diff --git ") &&
			(strings.Contains(line, " a/"+path+" ") || strings.Contains(line, " b/"+path)) {
			return true
		}
	}
	return false
}

func gitDiffLinePath(line, prefix string) (string, bool) {
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}

	value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if value == "" || value == "/dev/null" {
		return "", false
	}
	value = unquoteGitPath(value)
	value = strings.TrimPrefix(value, "a/")
	value = strings.TrimPrefix(value, "b/")
	return filepath.ToSlash(value), true
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

func workingTreeContentSnapshot(repoRoot string, file gitDiffFileInfo) (gitDiffContentSnapshot, error) {
	absolutePath := filepath.Join(repoRoot, filepath.FromSlash(file.Path))
	content, err := os.ReadFile(absolutePath)
	if err != nil {
		if os.IsNotExist(err) {
			return gitDiffContentSnapshot{
				Label:  "Working tree",
				Reason: "missing",
			}, nil
		}
		return gitDiffContentSnapshot{}, fmt.Errorf("read working tree file: %w", err)
	}

	return buildGitDiffContentSnapshot("Working tree", content), nil
}

func baseContentSnapshot(repoRoot string, file gitDiffFileInfo) (gitDiffContentSnapshot, error) {
	if file.Untracked {
		return gitDiffContentSnapshot{
			Label:  "Base",
			Reason: "untracked",
		}, nil
	}

	basePath := file.Path
	if file.OldPath != "" {
		basePath = file.OldPath
	}

	spec := fmt.Sprintf("HEAD:%s", basePath)
	exists, err := gitObjectExists(repoRoot, spec)
	if err != nil {
		return gitDiffContentSnapshot{}, err
	}
	if !exists {
		return gitDiffContentSnapshot{
			Label:  "Base",
			Reason: "missing",
		}, nil
	}

	content, err := gitObjectContent(repoRoot, spec)
	if err != nil {
		return gitDiffContentSnapshot{}, err
	}

	snapshot := buildGitDiffContentSnapshot("Base", content)
	return snapshot, nil
}

func buildGitDiffContentSnapshot(label string, content []byte) gitDiffContentSnapshot {
	snapshot := gitDiffContentSnapshot{
		Label:     label,
		Exists:    true,
		ByteCount: len(content),
	}

	if len(content) == 0 {
		return snapshot
	}

	if !utf8.Valid(content) {
		snapshot.Binary = true
		snapshot.Reason = "binary"
		return snapshot
	}

	display := content
	if len(display) > gitDiffContentMaxBytes {
		display = display[:gitDiffContentMaxBytes]
		for len(display) > 0 && !utf8.Valid(display) {
			display = display[:len(display)-1]
		}
		snapshot.Truncated = true
	}

	snapshot.LineCount = bytes.Count(content, []byte{'\n'})
	if len(content) > 0 && content[len(content)-1] != '\n' {
		snapshot.LineCount += 1
	}
	snapshot.Content = string(display)
	return snapshot
}

func gitObjectExists(repoRoot, spec string) (bool, error) {
	cmd := exec.Command("git", "-C", repoRoot, "cat-file", "-e", spec)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 128 {
			return false, nil
		}
		return false, fmt.Errorf("resolve git object %s: %w", spec, err)
	}
	return true, nil
}

func gitObjectContent(repoRoot, spec string) ([]byte, error) {
	cmd := exec.Command("git", "-C", repoRoot, "show", spec)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

func gitOutput(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
