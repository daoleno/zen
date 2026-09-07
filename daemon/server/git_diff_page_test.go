package server

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGitDiffPageInvalidTextMakesProgress(t *testing.T) {
	repo := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repo, "invalid.txt", strings.Repeat("\x80", 2048))
	page, err := (&Server{}).buildGitDiffPage("", repo, "invalid.txt", "all", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Total == 0 || page.Total > 30 {
		t.Fatalf("unexpected row count: %d", page.Total)
	}
}

func TestGitDiffPageReconstructsLongLinesAndMarkers(t *testing.T) {
	repo := initGitDiffTestRepo(t)
	path := " :[odd]\t->\nname .txt "
	writeGitDiffTestFile(t, repo, path, "old\n")
	runGitDiffTestGit(t, repo, "add", "--", gitLiteralPathspec(path))
	runGitDiffTestGit(t, repo, "commit", "-qm", "initial")
	content := strings.Repeat("界", 30000) + "last-match"
	writeGitDiffTestFile(t, repo, path, content)
	s := &Server{}
	var reconstructed strings.Builder
	start := 0
	version := ""
	pages := 0
	marker := false
	for {
		page, err := s.buildGitDiffPage("", repo, path, "working", start, version, "last-match")
		if err != nil {
			t.Fatal(err)
		}
		if page.Stale || len(page.Rows) > gitDiffPageSize {
			t.Fatalf("invalid page: %+v", page)
		}
		if page.Matches != 1 {
			t.Fatalf("match count %d", page.Matches)
		}
		version = page.Version
		pages++
		for _, row := range page.Rows {
			if !utf8.ValidString(row.Text) || len(row.Text) > gitDiffRowBytes {
				t.Fatal("invalid UTF-8 or oversized row")
			}
			if row.Kind == "add" {
				reconstructed.WriteString(row.Text)
				if row.New != 1 {
					t.Fatalf("wrong line number %d", row.New)
				}
			}
			if row.Kind == "marker" {
				marker = true
			}
		}
		start += len(page.Rows)
		if start >= page.Total {
			break
		}
	}
	if pages < 2 || reconstructed.String() != "+"+content || !marker {
		t.Fatalf("incomplete patch pages=%d marker=%v", pages, marker)
	}
	writeGitDiffTestFile(t, repo, path, "changed again\n")
	page, err := s.buildGitDiffPage("", repo, path, "working", 0, version, "")
	if err != nil || !page.Stale || len(page.Rows) != 0 {
		t.Fatalf("stale pages must not display: %+v %v", page, err)
	}
}

func TestGitDiffPageComparisonsAndRename(t *testing.T) {
	repo := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repo, "old -> name.txt", "original\n")
	runGitDiffTestGit(t, repo, "add", ".")
	runGitDiffTestGit(t, repo, "commit", "-qm", "initial")
	runGitDiffTestGit(t, repo, "mv", "old -> name.txt", "new\tname.txt")
	writeGitDiffTestFile(t, repo, "new\tname.txt", "working\n")
	files, err := listGitDiffFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].OldPath != "old -> name.txt" || files[0].Status != "renamed" || files[0].Additions != 1 || files[0].Deletions != 1 {
		t.Fatalf("rename stats: %+v", files)
	}
	s := &Server{}
	staged, err := s.buildGitDiffPage("", repo, files[0].Path, "staged", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	working, err := s.buildGitDiffPage("", repo, files[0].Path, "working", 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range staged.Rows {
		if row.Kind == "add" || row.Kind == "delete" {
			t.Fatal("pure rename must not become addition/deletion")
		}
	}
	if working.Hunks != 1 {
		t.Fatalf("working hunks: %+v", working)
	}
	if _, err = s.buildGitDiffPage("", repo, "../outside", "all", 0, "", ""); err == nil {
		t.Fatal("outside path accepted")
	}
	if _, err = s.buildGitDiffPage("", repo, files[0].Path, "invented", 0, "", ""); err == nil {
		t.Fatal("invalid scope accepted")
	}
}

func TestGitDiffUntrackedBinaryDeletedAndEmpty(t *testing.T) {
	repo := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repo, "deleted.txt", "old\n")
	runGitDiffTestGit(t, repo, "add", ".")
	runGitDiffTestGit(t, repo, "commit", "-qm", "initial")
	if err := os.Remove(repo + "/deleted.txt"); err != nil {
		t.Fatal(err)
	}
	writeGitDiffTestFile(t, repo, "binary.bin", "hello\x00world")
	writeGitDiffTestFile(t, repo, "empty.txt", "")
	writeGitDiffTestFile(t, repo, "  spaced  ", "one\ntwo")
	files, err := listGitDiffFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 {
		t.Fatalf("files: %+v", files)
	}
	for _, file := range files {
		page, err := (&Server{}).buildGitDiffPage("", repo, file.Path, "all", 0, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Rows) == 0 {
			t.Fatalf("missing metadata for %q", file.Path)
		}
		switch file.Path {
		case "binary.bin":
			if !file.Binary || file.Additions != 0 {
				t.Fatal(file)
			}
		case "  spaced  ":
			if file.Additions != 2 {
				t.Fatal(file)
			}
		case "deleted.txt":
			if file.Deletions != 1 || file.Status != "deleted" {
				t.Fatal(file)
			}
		}
	}
}

func TestGitDiffUntrackedAttributes(t *testing.T) {
	repo := initGitDiffTestRepo(t)
	writeGitDiffTestFile(t, repo, ".gitattributes", "*.asset -diff\n*.text diff\n")
	runGitDiffTestGit(t, repo, "add", ".")
	runGitDiffTestGit(t, repo, "commit", "-qm", "attributes")
	writeGitDiffTestFile(t, repo, "ascii.asset", "not text\n")
	writeGitDiffTestFile(t, repo, "null.text", "text\x00content\n")
	files, err := listGitDiffFiles(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		out, err := gitCommandOutput(repo, true, "diff", "--no-index", "--numstat", "/dev/null", file.Path)
		if err != nil {
			t.Fatal(err)
		}
		t.Log(file.Path, out)
		if file.Path == "ascii.asset" && (!file.Binary || file.Additions != 0) {
			t.Fatalf("binary attribute ignored: %+v", file)
		}
		if file.Path == "null.text" && (file.Binary || file.Additions != 1) {
			t.Fatalf("text attribute ignored: %+v", file)
		}
	}
}
