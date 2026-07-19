package skills

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxSkillNameLength = 128
	maxSourceLength    = 141
	maxCWDLength       = 4096
)

var (
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
	repoPattern  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,98}[A-Za-z0-9])?$`)
	skillPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,126}[a-z0-9])?$`)
)

func ValidateSkillName(value string) error {
	if value == "" || len(value) > maxSkillNameLength || !utf8.ValidString(value) {
		return errors.New("invalid Skill name length")
	}
	if !skillPattern.MatchString(value) || value == "." || value == ".." {
		return errors.New("invalid Skill name")
	}
	return nil
}

func ValidateRepository(value string) error {
	if value == "" || len(value) > maxSourceLength || !utf8.ValidString(value) {
		return errors.New("invalid repository length")
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 || !ownerPattern.MatchString(parts[0]) || !repoPattern.MatchString(parts[1]) {
		return errors.New("repository must be owner/repository")
	}
	if strings.EqualFold(parts[1], ".git") || strings.HasSuffix(strings.ToLower(parts[1]), ".git") {
		return errors.New("repository must not include a .git suffix")
	}
	return nil
}

func ValidateCatalogIdentity(id, source, name string) error {
	if err := ValidateRepository(source); err != nil {
		return err
	}
	if err := ValidateSkillName(name); err != nil {
		return err
	}
	if id != source+"/"+name {
		return errors.New("catalog identity does not match source and Skill")
	}
	return nil
}

func ValidateAgent(agent Agent) error {
	switch agent {
	case AgentCodex, AgentClaudeCode, AgentCursor:
		return nil
	case AgentGrok:
		return errors.New("Grok is not an official skills CLI target")
	default:
		return fmt.Errorf("unsupported Skill target %q", agent)
	}
}

func ValidateScope(scope Scope) error {
	if scope != ScopeProject && scope != ScopeGlobal {
		return fmt.Errorf("unsupported managed Skill scope %q", scope)
	}
	return nil
}

func ValidateCWD(value string, required bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return "", errors.New("project scope requires a working directory")
		}
		return "", nil
	}
	if len(value) > maxCWDLength || !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
		return "", errors.New("invalid working directory")
	}
	if !filepath.IsAbs(value) {
		return "", errors.New("working directory must be absolute")
	}
	return filepath.Clean(value), nil
}

func validateAgents(values []Agent) ([]Agent, error) {
	if len(values) == 0 || len(values) > 3 {
		return nil, errors.New("choose between one and three supported agents")
	}
	seen := make(map[Agent]struct{}, len(values))
	validated := make([]Agent, 0, len(values))
	for _, agent := range values {
		if err := ValidateAgent(agent); err != nil {
			return nil, err
		}
		if _, ok := seen[agent]; ok {
			return nil, fmt.Errorf("duplicate Skill target %q", agent)
		}
		seen[agent] = struct{}{}
		validated = append(validated, agent)
	}
	return validated, nil
}
