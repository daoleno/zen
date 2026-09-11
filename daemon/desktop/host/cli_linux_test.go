package host

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanPrintsWithoutInstalling(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "desktop-host.json")
	if err := os.WriteFile(config, []byte(`{"version":1,"hostId":"fixture","ownerUid":1000,"seat":"seat0"}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if err := RunLinuxCLI([]string{"--plan", "--config", config}, &stderr); err != nil {
		t.Fatal(err)
	}
	rendered := stderr.String()
	if !strings.Contains(rendered, "not applied") || !strings.Contains(rendered, "/usr/libexec/zen/zen desktop-host") {
		t.Fatalf("plan missing review content:\n%s", rendered)
	}
	if err := RunLinuxCLI([]string{"--plan", "--install", "--config", config}, &stderr); err == nil {
		t.Fatal("plan combined with install")
	}
}
