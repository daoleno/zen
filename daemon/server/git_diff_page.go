package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const gitDiffPageSize = 120
const gitDiffRowBytes = 512

type gitDiffRow struct {
	Kind         string `json:"kind"`
	Text         string `json:"text"`
	Old          int    `json:"old,omitempty"`
	New          int    `json:"new,omitempty"`
	Continuation bool   `json:"continuation,omitempty"`
}

type gitDiffPage struct {
	Path          string       `json:"path"`
	Scope         string       `json:"scope"`
	Version       string       `json:"version"`
	Stale         bool         `json:"stale"`
	Start         int          `json:"start"`
	Total         int          `json:"total"`
	Rows          []gitDiffRow `json:"rows"`
	Hunks         int          `json:"hunks"`
	PreviousHunk  int          `json:"previous_hunk"`
	NextHunk      int          `json:"next_hunk"`
	Matches       int          `json:"matches"`
	PreviousMatch int          `json:"previous_match"`
	NextMatch     int          `json:"next_match"`
}

var gitHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// Pages are stateless: Git is re-read, hashed and streamed, never cached across
// repository changes. Only requested rows are retained. Version prevents mixing
// pages from different patches; callers refresh explicitly when it changes.
func (s *Server) buildGitDiffPage(targetID, cwd, path, scope string, start int, version, query string) (gitDiffPage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page := gitDiffPage{Path: path, Scope: scope, Start: start, Rows: []gitDiffRow{}, PreviousHunk: -1, NextHunk: -1, PreviousMatch: -1, NextMatch: -1}
	if start < 0 {
		return page, fmt.Errorf("invalid diff row")
	}
	if scope != "all" && scope != "working" && scope != "staged" {
		return page, fmt.Errorf("invalid diff comparison")
	}
	root, reason, err := s.resolveGitRepoRoot(targetID, cwd)
	if err != nil {
		return page, err
	}
	if reason != "" {
		return page, fmt.Errorf("git diff unavailable: %s", reason)
	}
	file, err := gitDiffTargetFile(root, path)
	if err != nil {
		return page, err
	}
	hash := sha256.New()
	query = strings.ToLower(query)
	emit := func(row gitDiffRow) {
		if page.Total >= start && page.Total < start+gitDiffPageSize {
			page.Rows = append(page.Rows, row)
		}
		page.Total++
	}
	visit := func(kind string, row int) {
		if kind == "hunk" {
			page.Hunks++
			if row < start {
				page.PreviousHunk = row
			}
			if row > start && page.NextHunk < 0 {
				page.NextHunk = row
			}
		} else {
			page.Matches++
			if row < start {
				page.PreviousMatch = row
			}
			if row > start && page.NextMatch < 0 {
				page.NextMatch = row
			}
		}
	}
	for _, section := range []struct {
		enabled       bool
		scope         string
		args          []string
		allowDiffExit bool
	}{
		{file.Staged && scope != "working", "staged", gitDiffPageArgs(*file, true), false},
		{file.Unstaged && scope != "staged", "working", gitDiffPageArgs(*file, false), false},
		{file.Untracked && scope != "staged", "untracked", []string{"diff", "--no-index", "--no-ext-diff", "--no-textconv", "--no-color", "--", "/dev/null", file.Path}, true},
	} {
		if !section.enabled {
			continue
		}
		io.WriteString(hash, section.scope+"\x00")
		emit(gitDiffRow{Kind: "scope", Text: section.scope})
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, section.args...)...)
		cmd.WaitDelay = time.Second
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return page, err
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err = cmd.Start(); err != nil {
			return page, err
		}
		reader := bufio.NewReader(io.TeeReader(stdout, hash))
		oldLine, newLine := 0, 0
		inHunk := false
		for {
			line, readErr := reader.ReadString('\n')
			if len(line) > 0 {
				line = strings.TrimSuffix(line, "\n")
				row := gitDiffRow{Kind: "meta", Text: line}
				if strings.HasPrefix(line, "@@ ") {
					if match := gitHunkHeader.FindStringSubmatch(line); match != nil {
						oldLine, _ = strconv.Atoi(match[1])
						newLine, _ = strconv.Atoi(match[2])
						inHunk = true
						row.Kind = "hunk"
						visit("hunk", page.Total)
					}
				} else if strings.HasPrefix(line, "diff ") {
					inHunk = false
				} else if inHunk && len(line) > 0 {
					switch line[0] {
					case '+':
						row.Kind = "add"
						row.New = newLine
						newLine++
					case '-':
						row.Kind = "delete"
						row.Old = oldLine
						oldLine++
					case ' ':
						row.Kind = "context"
						row.Old = oldLine
						row.New = newLine
						oldLine++
						newLine++
					case '\\':
						row.Kind = "marker"
					}
				}
				// Split long lines on UTF-8 boundaries, retaining every byte and the
				// original line number. No giant native Text or invisible truncation.
				matchAt := -1
				if query != "" {
					matchAt = strings.Index(strings.ToLower(line), query)
				}
				consumed := 0
				for {
					n := min(len(row.Text), gitDiffRowBytes)
					for n < len(row.Text) && n > 0 && !utf8.RuneStart(row.Text[n]) {
						n--
					}
					if n == 0 {
						// Invalid text is replaced by JSON encoding; still advance.
						n = min(len(row.Text), gitDiffRowBytes)
					}
					part := row
					part.Text = row.Text[:n]
					if matchAt >= consumed && matchAt < consumed+n {
						visit("match", page.Total)
					}
					emit(part)
					if n == len(row.Text) {
						break
					}
					consumed += n
					row.Text = row.Text[n:]
					row.Continuation = true
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				stdout.Close()
				cmd.Wait()
				return page, readErr
			}
		}
		err = cmd.Wait()
		var exitErr *exec.ExitError
		if err != nil && !(section.allowDiffExit && errors.As(err, &exitErr) && exitErr.ExitCode() == 1) {
			return page, fmt.Errorf("git diff: %s", strings.TrimSpace(stderr.String()))
		}
	}
	page.Version = hex.EncodeToString(hash.Sum(nil))
	page.Stale = version != "" && version != page.Version
	if page.Stale {
		page.Rows = nil
	}
	return page, nil
}

func gitDiffPageArgs(file gitDiffFileInfo, staged bool) []string {
	args := append(gitDiffPatchArgs(staged), "--", gitLiteralPathspec(file.Path))
	if staged && file.OldPath != "" {
		args = append(args, gitLiteralPathspec(file.OldPath))
	}
	return args
}
