package doctor

import (
	"io"
	"runtime"
	"testing"
)

func TestDesktopCheckReportsSameBinaryProvenance(t *testing.T) {
	report, err := Run(Options{
		Home:     t.TempDir(),
		StateDir: t.TempDir(),
		Addr:     "127.0.0.1:0",
		PathEnv:  t.TempDir(),
		Listen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Desktop.SHA256 == "" || report.Desktop.Executable == "" {
		t.Fatalf("desktop=%+v", report.Desktop)
	}
	if len(report.Desktop.Roles) != 3 {
		t.Fatalf("roles=%v", report.Desktop.Roles)
	}
	found := false
	for _, check := range report.Checks {
		if check.ID == "desktop" {
			found = true
		}
	}
	if !found {
		t.Fatal("desktop check missing from report.Checks")
	}
	if report.Desktop.StreamReady {
		t.Fatal("doctor must not claim stream readiness from linking")
	}
	if runtime.GOOS == "linux" && report.Desktop.NativeLinked && report.Desktop.Status == StatusFail {
		t.Fatal("linked native binary reported corrupt")
	}
}
