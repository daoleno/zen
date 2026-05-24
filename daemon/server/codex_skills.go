package server

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxCodexSkillsPerRoot = 400

type CodexSkill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`
	Scope       string `json:"scope"`
	Enabled     bool   `json:"enabled"`
}

type codexSkillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func discoverCodexSkills(cwd string) []CodexSkill {
	roots := codexSkillRoots(cwd)
	seen := map[string]struct{}{}
	seenSkills := map[string]struct{}{}
	skills := make([]CodexSkill, 0)
	for _, root := range roots {
		if root.path == "" {
			continue
		}
		rootPath := filepath.Clean(expandHome(root.path))
		if _, ok := seen[rootPath]; ok {
			continue
		}
		seen[rootPath] = struct{}{}
		for _, skill := range discoverCodexSkillsInRoot(rootPath, root.scope) {
			key := skill.Name + "\x00" + skill.Path
			if _, ok := seenSkills[key]; ok {
				continue
			}
			seenSkills[key] = struct{}{}
			skills = append(skills, skill)
		}
	}
	sort.SliceStable(skills, func(left, right int) bool {
		leftRank := codexSkillScopeRank(skills[left].Scope)
		rightRank := codexSkillScopeRank(skills[right].Scope)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if skills[left].Name != skills[right].Name {
			return skills[left].Name < skills[right].Name
		}
		return skills[left].Path < skills[right].Path
	})
	return skills
}

func codexSkillRoots(cwd string) []struct {
	path  string
	scope string
} {
	roots := make([]struct {
		path  string
		scope string
	}, 0, 4)
	if projectRoot := findProjectRoot(cwd); projectRoot != "" {
		for _, dir := range dirsBetween(projectRoot, cwd) {
			roots = append(roots, struct {
				path  string
				scope string
			}{
				path:  filepath.Join(dir, ".agents", "skills"),
				scope: "repo",
			})
		}
	}
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		codexHome = filepath.Join(userHomeDir(), ".codex")
	}
	home := userHomeDir()
	roots = append(roots,
		struct {
			path  string
			scope string
		}{path: filepath.Join(codexHome, "skills"), scope: "user"},
		struct {
			path  string
			scope string
		}{path: filepath.Join(home, ".agents", "skills"), scope: "user"},
		struct {
			path  string
			scope string
		}{path: filepath.Join(codexHome, "skills", ".system"), scope: "system"},
	)
	return roots
}

func discoverCodexSkillsInRoot(root string, scope string) []CodexSkill {
	var skills []CodexSkill
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return filepath.SkipDir
			}
			return nil
		}
		if len(skills) >= maxCodexSkillsPerRoot {
			return filepath.SkipDir
		}
		name := entry.Name()
		if entry.IsDir() {
			if name != "." && strings.HasPrefix(name, ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if name != "SKILL.md" {
			return nil
		}
		skill, ok := parseCodexSkill(path, scope)
		if ok {
			skills = append(skills, skill)
		}
		return nil
	})
	return skills
}

func parseCodexSkill(path string, scope string) (CodexSkill, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CodexSkill{}, false
	}
	frontmatter, ok := splitSkillFrontmatter(string(data))
	if !ok {
		return CodexSkill{}, false
	}
	var parsed codexSkillFrontmatter
	if yaml.Unmarshal([]byte(frontmatter), &parsed) != nil {
		return CodexSkill{}, false
	}
	name := sanitizeCodexSkillLine(parsed.Name)
	if name == "" {
		name = filepath.Base(filepath.Dir(path))
	}
	name = sanitizeCodexSkillName(name)
	if name == "" {
		return CodexSkill{}, false
	}
	return CodexSkill{
		Name:        name,
		Description: truncateCodexSkillDescription(sanitizeCodexSkillLine(parsed.Description)),
		Path:        filepath.Clean(path),
		Scope:       scope,
		Enabled:     true,
	}, true
}

func splitSkillFrontmatter(value string) (string, bool) {
	value = strings.TrimPrefix(strings.ReplaceAll(value, "\r\n", "\n"), "\ufeff")
	if !strings.HasPrefix(value, "---\n") {
		return "", false
	}
	rest := value[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", false
	}
	frontmatter := strings.TrimSpace(rest[:end])
	return frontmatter, frontmatter != ""
}

func sanitizeCodexSkillLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func sanitizeCodexSkillName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "$")
	value = strings.ReplaceAll(value, " ", "-")
	var out strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == ':' {
			out.WriteRune(r)
		}
	}
	return strings.Trim(out.String(), "-_")
}

func truncateCodexSkillDescription(value string) string {
	const limit = 180
	if len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func codexSkillScopeRank(scope string) int {
	switch strings.ToLower(scope) {
	case "repo":
		return 0
	case "user":
		return 1
	case "system":
		return 2
	default:
		return 3
	}
}

func findProjectRoot(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}
	current, err := filepath.Abs(expandHome(cwd))
	if err != nil {
		current = filepath.Clean(expandHome(cwd))
	}
	for {
		if hasProjectRootMarker(current) {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return cwd
		}
		current = parent
	}
}

func hasProjectRootMarker(path string) bool {
	for _, marker := range []string{".git", "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(path, marker)); err == nil {
			return true
		}
	}
	return false
}

func dirsBetween(root string, cwd string) []string {
	root = filepath.Clean(expandHome(root))
	cwd = filepath.Clean(expandHome(cwd))
	if root == "" || cwd == "" {
		return nil
	}
	dirs := []string{root}
	rel, err := filepath.Rel(root, cwd)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return dirs
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		dirs = append(dirs, current)
	}
	return dirs
}

func expandHome(path string) string {
	if path == "~" {
		return userHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(userHomeDir(), strings.TrimPrefix(path, "~/"))
	}
	return path
}

func userHomeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}
