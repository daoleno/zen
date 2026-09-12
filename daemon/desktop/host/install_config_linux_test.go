package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/ini.v1"
)

func TestSDDMMultipleClosedFragmentsPreserveEffectiveHooks(t *testing.T) {
	merged := ini.Empty()
	for i, content := range []string{
		"[General]\nDisplayServer=x11\n[X11]\nDisplayCommand=/usr/share/sddm/scripts/Xsetup\nDisplayStopCommand=/usr/share/sddm/scripts/Xstop\n",
		"[Theme]\nCurrent=fixture\n",
		"[X11]\nDisplayCommand=/usr/local/share/fixture-Xsetup\n",
	} {
		path := filepath.Join(t.TempDir(), "fragment.conf")
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		err = appendSDDMConfig(merged, file)
		file.Close()
		if err != nil {
			t.Fatalf("fragment %d: %v", i, err)
		}
	}
	if merged.Section("X11").Key("DisplayCommand").String() != "/usr/local/share/fixture-Xsetup" {
		t.Fatal("lost last override")
	}
	if merged.Section("X11").Key("DisplayStopCommand").String() != "/usr/share/sddm/scripts/Xstop" {
		t.Fatal("lost original stop hook")
	}
	if err := merged.Reload(); err != nil {
		t.Fatal(err)
	}
}

func TestSDDMFragmentSizeBound(t *testing.T) {
	if err := appendSDDMConfig(ini.Empty(), strings.NewReader(strings.Repeat("x", 65537))); err == nil {
		t.Fatal("oversized fragment accepted")
	}
}
