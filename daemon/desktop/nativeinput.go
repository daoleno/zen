package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const nativeBuildInputMacro = "ZEN_NATIVE_BUILD_INPUT"

// NativeBuildInputToken is a content hash of cgo-included native C/headers.
// Size and mtime are ignored: a comment-only C edit still changes this token
// (source bytes changed) but may compile to an identical ELF, which is why
// rebuild proof must inspect linked marker bytes, not output stamps.
func NativeBuildInputToken(nativeDir string) (string, error) {
	entries, err := os.ReadDir(nativeDir)
	if err != nil {
		return "", err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !nativeCgoInputName(name) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	sum := sha256.New()
	for _, name := range names {
		fmt.Fprintf(sum, "%s\n", name)
		file, err := os.Open(filepath.Join(nativeDir, name))
		if err != nil {
			return "", err
		}
		_, copyErr := io.Copy(sum, file)
		_ = file.Close()
		if copyErr != nil {
			return "", copyErr
		}
		sum.Write([]byte{'\n'})
	}
	return hex.EncodeToString(sum.Sum(nil)[:8]), nil
}

func nativeCgoInputName(name string) bool {
	switch name {
	case "linux.c", "host-agent.c", "encoder.c", "encoder.h", "portal.c", "portal.h", "portal_capture.c", "portal_capture.h":
		return true
	default:
		return false
	}
}

// NativeBuildInputFlag is the structured compiler define cgo uses as a build
// input so an external #include of desktop/native/*.c is not served from a
// stale GOCACHE object after those files change.
func NativeBuildInputFlag(token string) string {
	if token == "" {
		token = "none"
	}
	return "-D" + nativeBuildInputMacro + "=h" + token
}

// WithNativeBuildInput appends or replaces CGO_CFLAGS with the native build
// input define, preserving any caller CGO_CFLAGS. It does not delete GOCACHE.
func WithNativeBuildInput(env []string, token string) []string {
	flag := NativeBuildInputFlag(token)
	out := make([]string, 0, len(env)+1)
	existing := ""
	for _, item := range env {
		if strings.HasPrefix(item, "CGO_CFLAGS=") {
			existing = strings.TrimPrefix(item, "CGO_CFLAGS=")
			continue
		}
		out = append(out, item)
	}
	merged := strings.TrimSpace(existing + " " + flag)
	return append(out, "CGO_CFLAGS="+merged)
}
