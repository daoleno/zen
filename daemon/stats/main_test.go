package stats

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Collector construction must never import a developer's live price cache.
	home, err := os.MkdirTemp("", "zen-stats-test-home-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("HOME", home); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
