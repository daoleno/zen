package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/watcher"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in, loopback-only native fixture server. The production authenticated WS
// handler and real Git repositories are used without starting a live daemon.
func TestGitDiffNativeFixtureServer(t *testing.T) {
	if os.Getenv("ZEN_GIT_DIFF_NATIVE") != "1" {
		t.Skip("native fixture server is opt-in")
	}
	fixtures := map[string]string{}
	for _, f := range []struct {
		name                string
		files, lines, width int
	}{{"small", 3, 20, 40}, {"medium", 50, 200, 80}, {"many", 1000, 20, 40}, {"large", 1, 50000, 80}, {"long", 1, 10, 100000}} {
		repo := initGitDiffTestRepo(t)
		fixtures[f.name] = repo
		for i := 0; i < f.files; i++ {
			writeGitDiffTestFile(t, repo, fmt.Sprintf("file-%04d.ts", i), strings.Repeat("old "+strings.Repeat("x", f.width)+"\n", f.lines))
		}
		runGitDiffTestGit(t, repo, "add", ".")
		runGitDiffTestGit(t, repo, "commit", "-qm", "fixture")
		for i := 0; i < f.files; i++ {
			writeGitDiffTestFile(t, repo, fmt.Sprintf("file-%04d.ts", i), strings.Repeat("new "+strings.Repeat("x", f.width)+"\n", f.lines))
		}
	}
	manager, err := auth.NewManager(t.TempDir())
	review := initGitDiffTestRepo(t)
	fixtures["review"] = review
	before := "export async function loadChanges(serverId: string) {\n  const response = await request(serverId, {\n    type: 'git_diff_patch',\n    path: selectedPath,\n  });\n  return response.patch;\n}\n"
	writeGitDiffTestFile(t, review, "app/services/changes.ts", before)
	writeGitDiffTestFile(t, review, "docs/git.md", "# Git review\n\nReview local changes before committing.\n")
	writeGitDiffTestFile(t, review, "app/services/legacy.ts", "export const expandAll = true;\n")
	runGitDiffTestGit(t, review, "add", ".")
	runGitDiffTestGit(t, review, "commit", "-qm", "review fixture")
	runGitDiffTestGit(t, review, "mv", "docs/git.md", "docs/review.md")
	writeGitDiffTestFile(t, review, "app/services/changes.ts", strings.ReplaceAll(before, "git_diff_patch", "git_diff_page"))
	runGitDiffTestGit(t, review, "add", "app/services/changes.ts")
	writeGitDiffTestFile(t, review, "app/services/changes.ts", "export async function loadChanges(serverId: string) {\n  const response = await request(serverId, {\n    type: 'git_diff_page',\n    path: selectedPath,\n    scope: comparison,\n    row: position.row,\n    file_generation: position.version,\n  });\n  return response.page;\n}\n")
	writeGitDiffTestFile(t, review, "app/components/ChangeList.tsx", "export function ChangeList({ files }) {\n  return <FlatList data={files} renderItem={renderFile} />;\n}\n")
	writeGitDiffTestFile(t, review, "assets/review.bin", "image\x00bytes")
	os.Remove(review + "/app/services/legacy.ts")
	if err != nil {
		t.Fatal(err)
	}
	pairing, _ := manager.IssuePairingToken(time.Minute)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	if _, err = manager.EnrollDevice(pairing.Value, manager.DaemonID(), manager.PublicKeyHex(), "diff-fixture", "Fixture", hex.EncodeToString(public)); err != nil {
		t.Fatal(err)
	}
	srv := New(manager, watcher.New(time.Second), nil, nil, nil, nil, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", srv.handleWS)
	mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"fixtures": fixtures, "authorization": calendarAuthHeader(private, manager.DaemonID(), "diff-fixture", "zen-connect")})
	})
	done := make(chan struct{}, 1)
	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		select {
		case done <- struct{}{}:
		default:
		}
	})
	listener, err := net.Listen("tcp", "127.0.0.1:8097")
	if err != nil {
		t.Fatal(err)
	}
	httpServer := &http.Server{Handler: mux}
	defer httpServer.Close()
	go httpServer.Serve(listener)
	t.Log("Git diff native fixtures ready on loopback port 8097")
	select {
	case <-done:
	case <-time.After(30 * time.Minute):
		t.Error("native fixture session timed out")
	}
}
