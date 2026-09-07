package server

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Fixtures live only in the test temp directory; no user's index is mutated.
func BenchmarkGitDiff(b *testing.B) {
	for _, fixture := range []struct {
		name                string
		files, lines, width int
	}{
		{"small", 3, 20, 40}, {"medium", 50, 200, 80},
		{"many", 1000, 20, 40}, {"large", 1, 50000, 80}, {"long", 1, 10, 100000},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			repo := b.TempDir()
			git := func(args ...string) {
				b.Helper()
				if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
					b.Fatalf("%s: %v", out, err)
				}
			}
			git("init", "-q")
			git("config", "user.email", "fixture@example.invalid")
			git("config", "user.name", "Fixture")
			write := func(version string) {
				for i := 0; i < fixture.files; i++ {
					content := strings.Repeat(version+strings.Repeat("x", fixture.width)+"\n", fixture.lines)
					if err := os.WriteFile(filepath.Join(repo, fmt.Sprintf("file-%04d.ts", i)), []byte(content), 0600); err != nil {
						b.Fatal(err)
					}
				}
			}
			write("old ")
			git("add", ".")
			git("commit", "-qm", "fixture")
			write("new ")
			for _, operation := range []string{"status", "patch", "page"} {
				b.Run(operation, func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						var payload any
						var err error
						if operation == "status" {
							payload, err = (&Server{}).buildGitDiffStatus("", repo)
						} else if operation == "patch" {
							payload, err = (&Server{}).buildGitDiffPatch("", repo, "file-0000.ts")
						} else {
							payload, err = (&Server{}).buildGitDiffPage("", repo, "file-0000.ts", "all", 0, "", "")
						}
						if err != nil {
							b.Fatal(err)
						}
						data, err := json.Marshal(payload)
						if err != nil {
							b.Fatal(err)
						}
						b.ReportMetric(float64(len(data)), "payload-B")
					}
				})
			}
		})
	}
}
