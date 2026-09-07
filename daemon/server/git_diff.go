package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	Path             string `json:"path"`
	OldPath          string `json:"old_path,omitempty"`
	Status           string `json:"status"`
	Staged           bool   `json:"staged"`
	Unstaged         bool   `json:"unstaged"`
	Untracked        bool   `json:"untracked"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
	Binary           bool   `json:"binary,omitempty"`
	StagedAdditions  int    `json:"staged_additions,omitempty"`
	StagedDeletions  int    `json:"staged_deletions,omitempty"`
	WorkingAdditions int    `json:"working_additions,omitempty"`
	WorkingDeletions int    `json:"working_deletions,omitempty"`
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

	payload := gitDiffStatusPayload{
		Available: true,
		RepoRoot:  repoRoot,
		RepoName:  filepath.Base(repoRoot),
		Branch:    gitBranchName(repoRoot),
		Clean:     len(files) == 0,
		FileCount: len(files),
		Files:     files,
	}

	for _, file := range files {
		payload.Additions += file.Additions
		payload.Deletions += file.Deletions
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

	targetPath := path
	if targetPath == "" {
		return gitDiffPatchPayload{}, fmt.Errorf("git diff file path is required")
	}

	files, err := readGitDiffStatus(repoRoot)
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
		patch, err := gitDiffPatchForPaths(repoRoot, *file, true)
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
		patch, err := gitDiffPatchForPaths(repoRoot, *file, false)
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
			"--no-color",
			"--no-textconv",
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
	resolvedCwd := cwd
	if resolvedCwd == "" && targetID != "" {
		if worker := s.watcher.GetWorker(targetID); worker != nil {
			resolvedCwd = worker.Cwd
		}
	}
	if resolvedCwd == "" {
		return "", gitDiffReasonNoCwd, nil
	}

	root, gitErr := gitCommandOutput(resolvedCwd, false, "rev-parse", "--show-toplevel")
	if gitErr != nil {
		if strings.Contains(strings.ToLower(gitErr.Error()), "not a git repository") {
			return "", gitDiffReasonNotGitRepo, nil
		}
		return "", "", fmt.Errorf("resolve git repo root: %w", gitErr)
	}
	return strings.TrimSuffix(root, "\n"), "", nil
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

func listGitDiffFiles(repoRoot string) ([]gitDiffFileInfo, error) {
	files, err := readGitDiffStatus(repoRoot)
	if err != nil {
		return nil, err
	}
	if err := hydrateGitDiffFileStats(repoRoot, files); err != nil {
		return nil, err
	}
	return files, nil
}

func readGitDiffStatus(repoRoot string) ([]gitDiffFileInfo, error) {
	out, err := gitCommandOutput(repoRoot, false, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	files := make([]gitDiffFileInfo, 0)
	records := strings.Split(out, "\x00")
	for index := 0; index < len(records); index++ {
		line := records[index]
		if len(line) < 3 {
			continue
		}

		x := line[0]
		y := line[1]
		if x == '!' && y == '!' {
			continue
		}

		path, oldPath := line[3:], ""
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			index++
			if index >= len(records) {
				return nil, fmt.Errorf("incomplete git rename status")
			}
			oldPath = records[index]
		}
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

func hydrateGitDiffFileStats(repoRoot string, files []gitDiffFileInfo) error {
	byPath := make(map[string]*gitDiffFileInfo, len(files))
	for index := range files {
		byPath[files[index].Path] = &files[index]
	}
	for _, staged := range []bool{false, true} {
		args := []string{"diff", "--numstat", "-z", "--no-ext-diff", "--no-textconv", "--find-renames"}
		if staged {
			args = append(args, "--cached")
		}
		out, err := gitCommandOutput(repoRoot, false, args...)
		if err != nil {
			return err
		}
		records := strings.Split(out, "\x00")
		for index := 0; index < len(records); index++ {
			fields := strings.SplitN(records[index], "\t", 3)
			if len(fields) != 3 {
				continue
			}
			path := fields[2]
			if path == "" {
				if index+2 >= len(records) {
					return fmt.Errorf("incomplete git rename statistics")
				}
				path = records[index+2]
				index += 2
			}
			if file := byPath[path]; file != nil {
				additions, _ := parseNumstatField(fields[0])
				deletions, _ := parseNumstatField(fields[1])
				file.Additions += additions
				file.Deletions += deletions
				if staged {
					file.StagedAdditions += additions
					file.StagedDeletions += deletions
				} else {
					file.WorkingAdditions += additions
					file.WorkingDeletions += deletions
				}
				file.Binary = file.Binary || fields[0] == "-"
			}
		}
	}
	attributes, err := untrackedDiffAttributes(repoRoot, files)
	if err != nil {
		return err
	}
	for index := range files {
		if !files[index].Untracked {
			continue
		}
		attribute := attributes[files[index].Path]
		if attribute == "unset" {
			files[index].Binary = true
			continue
		}
		count, binary, err := countUntrackedLines(repoRoot, files[index].Path, attribute == "set")
		if err != nil {
			return err
		}
		files[index].Additions = count
		files[index].WorkingAdditions = count
		files[index].Binary = binary
	}
	return nil
}

func untrackedDiffAttributes(repoRoot string, files []gitDiffFileInfo) (map[string]string, error) {
	var input strings.Builder
	for _, file := range files {
		if file.Untracked {
			input.WriteString(file.Path)
			input.WriteByte(0)
		}
	}
	attributes := make(map[string]string)
	if input.Len() == 0 {
		return attributes, nil
	}
	cmd := exec.Command("git", "-C", repoRoot, "check-attr", "-z", "--stdin", "diff")
	cmd.Stdin = strings.NewReader(input.String())
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff attributes: %w", err)
	}
	records := strings.Split(string(out), "\x00")
	drivers := make(map[string]string)
	for i := 0; i+2 < len(records); i += 3 {
		value := records[i+2]
		if value != "set" && value != "unset" && value != "unspecified" {
			if cached, ok := drivers[value]; ok {
				value = cached
			} else {
				binary, _ := gitOutput(repoRoot, "config", "--type=bool", "--get", "diff."+value+".binary")
				resolved := "unspecified"
				if binary == "true" {
					resolved = "unset"
				}
				if binary == "false" {
					resolved = "set"
				}
				drivers[value] = resolved
				value = resolved
			}
		}
		attributes[records[i]] = value
	}
	return attributes, nil
}

func countUntrackedLines(repoRoot, path string, forceText bool) (int, bool, error) {
	full := filepath.Join(repoRoot, path)
	info, err := os.Lstat(full)
	if err != nil {
		return 0, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(full)
		if err != nil {
			return 0, false, err
		}
		count := strings.Count(link, "\n")
		if !strings.HasSuffix(link, "\n") {
			count++
		}
		return count, false, nil
	}
	if !info.Mode().IsRegular() {
		return 0, false, fmt.Errorf("cannot review non-regular file: %s", path)
	}
	f, err := os.Open(full)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()
	buf := make([]byte, 32*1024)
	count, total := 0, 0
	var last byte
	for {
		n, err := f.Read(buf)
		if n > 0 {
			// Git's default binary heuristic inspects the first 8000 bytes.
			if !forceText && total < 8000 && bytes.IndexByte(buf[:min(n, 8000-total)], 0) >= 0 {
				return 0, true, nil
			}
			count += bytes.Count(buf[:n], []byte{'\n'})
			total += n
			last = buf[n-1]
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, false, err
		}
	}
	if total > 0 && last != '\n' {
		count++
	}
	return count, false, nil
}

func parseNumstatField(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func gitDiffTargetFile(repoRoot, path string) (*gitDiffFileInfo, error) {
	targetPath := path
	if targetPath == "" {
		return nil, fmt.Errorf("git diff file path is required")
	}

	files, err := readGitDiffStatus(repoRoot)
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

func gitLiteralPathspec(path string) string {
	path = filepath.ToSlash(path)
	path = strings.TrimPrefix(path, "./")
	if path == "" {
		return ":(literal)."
	}
	return ":(literal)./" + path
}

func gitDiffPatchForPaths(repoRoot string, file gitDiffFileInfo, staged bool) (string, error) {
	args := gitDiffPatchArgs(staged)
	args = append(args, "--", gitLiteralPathspec(file.Path))
	if staged && file.OldPath != "" {
		args = append(args, gitLiteralPathspec(file.OldPath))
	}
	return gitCommandOutput(repoRoot, false, args...)
}

func gitDiffPatchArgs(staged bool) []string {
	args := []string{
		"diff",
		"--no-ext-diff",
		"--no-color",
		"--no-textconv",
		"--find-renames",
		"--submodule=diff",
	}
	if staged {
		args = append(args, "--cached")
	}
	return args
}

func gitDiffStatusName(x, y byte) string {
	if x == '?' && y == '?' {
		return "untracked"
	}
	if x == 'U' || y == 'U' || (x == 'A' && y == 'A') || (x == 'D' && y == 'D') {
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
