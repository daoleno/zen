package issue

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// LoadProject reads <projectDir>/project.toml and returns the project config.
// If the file is absent, a default config with Name = basename(projectDir) is returned.
func LoadProject(projectDir string) (Project, error) {
	project := Project{Name: filepath.Base(projectDir)}
	path := filepath.Join(projectDir, "project.toml")

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return project, nil
	}
	if err != nil {
		return Project{}, err
	}
	if err := toml.Unmarshal(raw, &project); err != nil {
		return Project{}, err
	}
	if project.Name == "" {
		project.Name = filepath.Base(projectDir)
	}
	return project, nil
}
