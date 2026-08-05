package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestWorkProviderActivityProbeCursorAdmissionPreservesExactBytesAndCursor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "repo", "zen")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	projectName := strings.Trim(strings.ReplaceAll(cwd, string(filepath.Separator), "-"), "-")
	projectDir := filepath.Join(home, ".cursor", "projects", projectName)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	trusted, err := json.Marshal(map[string]string{"workspacePath": cwd})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".workspace-trusted"), trusted, 0o644); err != nil {
		t.Fatal(err)
	}
	sessionID := "cursor-admission-fixture"
	transcriptDir := filepath.Join(projectDir, "agent-transcripts", sessionID)
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(transcriptDir, sessionID+".jsonl")
	initial := "task \n\n\nwith CRLF\r\n你好"
	follow := "task\n\nwith CRLF\n你好"
	writeCursorAdmissionFixture(t, transcript, initial)

	agent := classifier.Agent{
		ID:        "cursor-fixture:@1",
		Name:      "Cursor fixture",
		Command:   "cursor-agent --force",
		Cwd:       cwd,
		StartedAt: time.Now().UTC().Add(-time.Minute),
		PaneAlive: true,
	}
	probe := newWorkProviderActivityProbe()
	before := probe.ObserveProviderActivity(agent, time.Now().UTC())
	wantInitial := fmt.Sprintf("%x", sha256.Sum256([]byte(initial)))
	if before.AdmissionID == "" || before.AdmissionCursor == 0 ||
		before.InputSHA256 != wantInitial {
		t.Fatalf("initial admission = %+v, want exact digest %s", before, wantInitial)
	}

	appendCursorAdmissionFixture(t, transcript, follow)
	after := probe.ObserveProviderActivity(agent, time.Now().UTC())
	wantFollow := fmt.Sprintf("%x", sha256.Sum256([]byte(follow)))
	if after.AdmissionID == "" || after.AdmissionID == before.AdmissionID ||
		after.AdmissionCursor <= before.AdmissionCursor ||
		after.AdmissionStream != before.AdmissionStream ||
		after.InputSHA256 != wantFollow {
		t.Fatalf("follow-up admission = %+v, before=%+v want digest=%s", after, before, wantFollow)
	}
	if before.InputSHA256 == after.InputSHA256 {
		t.Fatal("normalized-but-byte-different Cursor inputs shared an admission digest")
	}
}

func writeCursorAdmissionFixture(t *testing.T, path string, payload string) {
	t.Helper()
	raw, err := json.Marshal(cursorAdmissionFixtureRow(payload))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendCursorAdmissionFixture(t *testing.T, path string, payload string) {
	t.Helper()
	raw, err := json.Marshal(cursorAdmissionFixtureRow(payload))
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
}

func cursorAdmissionFixtureRow(payload string) map[string]any {
	return map[string]any{
		"role": "user",
		"message": map[string]any{
			"content": []map[string]any{{
				"type": "text",
				"text": "<timestamp>" + time.Now().UTC().Format(time.RFC3339Nano) + "</timestamp>\n" +
					"<user_query>\n" + payload + "\n</user_query>",
			}},
		},
	}
}
