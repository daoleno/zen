package skills

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxSkillNameLength = 128
	maxCWDLength       = 4096
)

var skillPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,126}[a-z0-9])?$`)

func ValidateSkillName(value string) error {
	if value == "" || len(value) > maxSkillNameLength || !utf8.ValidString(value) {
		return errors.New("invalid Skill name length")
	}
	if !skillPattern.MatchString(value) || value == "." || value == ".." {
		return errors.New("invalid Skill name")
	}
	return nil
}

func ValidateCWD(value string, required bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return "", errors.New("a working directory is required")
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
