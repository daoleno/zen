package doctor

import (
	"debug/elf"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/daoleno/zen/daemon/desktop"
	"github.com/daoleno/zen/daemon/desktop/nativebind"
)

var linuxDesktopLibraries = []string{
	"libgtk-3.so.0",
	"libgstreamer-1.0.so.0",
	"libgstapp-1.0.so.0",
	"libgstvideo-1.0.so.0",
	"libX11.so.6",
	"libXtst.so.6",
	"libgio-2.0.so.0",
}

func (e env) checkDesktop() DesktopCheck {
	check := DesktopCheck{
		NativeLinked: nativebind.NativeLinked,
		StreamReady:  false,
		DisplaySet:   os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "",
		Roles:        []string{desktop.RoleHelper, desktop.RoleHost, desktop.RoleAgent},
	}
	exe, err := desktop.CurrentExecutable()
	if err != nil {
		check.Status = StatusFail
		check.Remediation = RemediationDesktopCorrupt
		check.Summary = "zen executable identity is unavailable"
		return check
	}
	check.Executable = exe
	sum, err := desktop.FileSHA256(exe)
	if err != nil {
		check.Status = StatusFail
		check.Remediation = RemediationDesktopCorrupt
		check.Summary = "zen executable could not be hashed"
		return check
	}
	check.SHA256 = sum
	if runtime.GOOS != "linux" {
		check.Status = StatusWarn
		check.Summary = fmt.Sprintf("desktop host capture is not implemented on %s", runtime.GOOS)
		return check
	}
	needed, corrupt := elfNeeded(exe)
	if corrupt {
		check.Status = StatusFail
		check.Remediation = RemediationDesktopCorrupt
		check.Summary = "zen executable is not a readable ELF"
		return check
	}
	check.Libraries = needed
	if !nativebind.NativeLinked {
		check.Status = StatusWarn
		check.Remediation = RemediationDesktopNativeMissing
		check.Summary = "this zen binary was built without desktop native roles; rebuild with CGO and -tags zen_desktop. A CGO-disabled archive is daemon-only"
		return check
	}
	missing := missingDTNeeded(needed, linuxDesktopLibraries)
	check.Missing = missing
	if len(missing) > 0 {
		check.Status = StatusWarn
		check.Remediation = RemediationDesktopLibraries
		check.Summary = "desktop native tag is set but this ELF does not DT_NEED: " + strings.Join(missing, ", ")
		return check
	}
	check.Status = StatusOK
	check.Summary = "same-binary desktop roles are cgo-linked; GTK/GStreamer/X11 remain dynamic OS dependencies, not a static single file. Linking is not stream readiness. If those DT_NEEDED libraries are absent, ld.so will refuse to start this ELF before zen doctor can run"
	if !check.DisplaySet {
		check.Summary += ". DISPLAY and WAYLAND_DISPLAY are unset in this process"
	}
	return check
}

func elfNeeded(path string) ([]string, bool) {
	file, err := elf.Open(path)
	if err != nil {
		return nil, true
	}
	defer file.Close()
	needed, err := file.DynString(elf.DT_NEEDED)
	if err != nil {
		return nil, true
	}
	return needed, false
}

func missingDTNeeded(needed, want []string) []string {
	have := map[string]bool{}
	for _, lib := range needed {
		have[lib] = true
		have[filepath.Base(lib)] = true
	}
	var missing []string
	for _, lib := range want {
		if !have[lib] {
			missing = append(missing, lib)
		}
	}
	return missing
}
