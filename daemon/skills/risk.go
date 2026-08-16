package skills

import (
	"os"
	"path/filepath"
	"strings"
)

// Static risk signals are bounded, deterministic warnings extracted from
// package content. They are never a safety verdict: they exist so the UI can
// show "this package contains executable/script content" without claiming
// inspection equates to trust.

const (
	maxRiskInspectFiles     = 96
	maxRiskScanBytesPerFile = 64 << 10
	riskSecretMatchLimit    = 4
)

var scriptExtensions = map[string]bool{
	".sh": true, ".bash": true, ".zsh": true, ".py": true, ".rb": true,
	".pl": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true,
	".tsx": true, ".jsx": true, ".lua": true, ".go": true, ".rs": true,
	".sql": true, ".fish": true, ".ps1": true, ".r": true, ".groovy": true,
}

var secretFileStems = map[string]bool{
	"secret": true, "secrets": true, "token": true, "tokens": true,
	"credential": true, "credentials": true, "api_key": true, "apikey": true,
	".env": true, "password": true, "passwords": true, "private_key": true,
	"key.pem": true, "id_rsa": true, "id_ed25519": true,
}

// scanRiskSignals walks a package folder and returns bounded signals.
func scanRiskSignals(root string) []RiskSignal {
	files, err := collectRegularFiles(root)
	if err != nil || len(files) == 0 {
		return nil
	}
	if len(files) > maxRiskInspectFiles {
		files = files[:maxRiskInspectFiles]
	}
	signals := make([]RiskSignal, 0, 8)
	executables := 0
	scripts := 0
	secrets := 0
	for _, relative := range files {
		base := filepath.Base(relative)
		stem := strings.ToLower(base)
		info, err := statRegular(root, relative)
		isExecutable := err == nil && info&0o111 != 0
		if isExecutable {
			executables++
			if executables <= 3 {
				signals = append(signals, RiskSignal{
					Type: "executable", Severity: "warn",
					File: relative, Detail: "File carries executable permissions.",
				})
			}
		}
		extension := strings.ToLower(filepath.Ext(base))
		if scriptExtensions[extension] && base != "SKILL.md" {
			scripts++
			if scripts <= 3 {
				signals = append(signals, RiskSignal{
					Type: "script", Severity: "info",
					File: relative, Detail: "Script source file (" + strings.TrimPrefix(extension, ".") + ").",
				})
			}
		}
		if isSecretStem(stem) || extension == ".pem" {
			secrets++
			if secrets <= riskSecretMatchLimit {
				signals = append(signals, RiskSignal{
					Type: "secret-sensitive", Severity: "alert",
					File: relative, Detail: "File name suggests secret or credential material.",
				})
			}
		}
		if hasShebang(root, relative) {
			signals = append(signals, RiskSignal{
				Type: "script", Severity: "warn",
				File: relative, Detail: "Executable script (shebang).",
			})
		}
	}
	if networkCount(root, files) > 0 {
		signals = append(signals, RiskSignal{
			Type: "network", Severity: "info",
			Detail: "Package references network endpoints; verify destinations before trusting.",
		})
	}
	if executables > 3 {
		signals = append(signals, RiskSignal{
			Type: "executable", Severity: "warn",
			Detail: "Package contains many executable files.", File: "",
		})
	}
	return signals
}

func statRegular(root, relative string) (os.FileMode, error) {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return 0, err
	}
	return info.Mode().Perm(), nil
}

func isSecretStem(stem string) bool {
	// .env exact and secret/token/credential/password stems.
	if secretFileStems[stem] {
		return true
	}
	name := stem
	if name == ".env.example" || name == ".env.sample" {
		return false
	}
	if strings.HasPrefix(name, ".env") {
		return true
	}
	for part := range secretFileStems {
		if strings.Contains(name, part) {
			return true
		}
	}
	return false
}

func hasShebang(root, relative string) bool {
	content, ok, err := readTextFileBounded(filepath.Join(root, filepath.FromSlash(relative)), 512)
	if err != nil || !ok {
		return false
	}
	return strings.HasPrefix(content, "#!")
}

func networkCount(root string, files []string) int {
	count := 0
	checked := 0
	for _, relative := range files {
		if checked >= 24 {
			break
		}
		checked++
		data, ok, err := readTextFileBounded(filepath.Join(root, filepath.FromSlash(relative)), maxRiskScanBytesPerFile)
		if err != nil || !ok {
			continue
		}
		lower := strings.ToLower(data)
		if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
			// A single benign README link is not itself a network signal.
			if strings.Contains(lower, "curl ") || strings.Contains(lower, "wget ") ||
				strings.Contains(lower, "fetch(") || strings.Contains(lower, "axios") ||
				strings.Contains(lower, "open(") || strings.Contains(lower, "net.http") {
				count++
			}
		}
		if count >= 3 {
			return count
		}
	}
	return count
}
