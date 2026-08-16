package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Migration is the non-destructive bridge between existing local skill
// installations and the Zen-owned inventory:
//
//   - Skills already under Zen's management are counted as owned.
//   - Newly discovered external installations are TRACKED (written as
//     unowned inventory entries pointing at their directories) so the user can
//     adopt or forget each one without Zen touching their files.
//   - Same-name different-content installations inside agent directories are
//     classified as duplicates/conflicts and are NEVER overwritten.
//
// The scan is bounded and mirrors discovery bounds.

const (
	maxMigrationRowsPerRoot = defaultMaxInstalledSkills
	maxMigrationSkinny      = 64 << 10
)

type MigrationReport struct {
	Tracked   int      `json:"tracked"`
	Owned     int      `json:"owned"`
	External  int      `json:"external"`
	Duplicate int      `json:"duplicate"`
	Conflict  int      `json:"conflict"`
	Skipped   []string `json:"skipped,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
}

type migrationSource struct {
	dir    string
	scope  Scope
	agents []Agent
}

// ExecuteMigration scans the six Agent surfaces (plus project scope when a
// working directory is known), classifies every Skill, and writes tracking
// entries for external installations into the central inventory. It never
// deletes, moves, or edits any external file.
func ExecuteMigration(options InventoryOptions) (MigrationReport, error) {
	normalized, err := normalizeInventoryOptions(options)
	if err != nil {
		return MigrationReport{}, err
	}
	store := Store{StateDir: normalized.ZenStateDir, Home: normalized.Home, Now: normalized.Now}
	if err := store.Lock(); err != nil {
		return MigrationReport{}, err
	}
	defer store.Unlock()
	inventory, err := store.LoadInventory(false)
	if err != nil {
		return MigrationReport{}, err
	}
	if inventory.Packages == nil {
		inventory.Packages = map[string]PackageEntry{}
	}

	report := MigrationReport{}
	// name -> hash seen earlier in this run; a second different-content copy
	// of the same name is a duplicate/conflict, never a silent overwrite.
	seenHash := map[string]string{}
	for _, agent := range []Agent{AgentCodex, AgentClaudeCode, AgentCursor, AgentGrok, AgentOpenCode, AgentPi} {
		adapter, err := adapterFor(agent)
		if err != nil {
			continue
		}
		globalDir := globalSkillsDir(adapter, normalized.Home, envResolverFor(normalized))
		report.scanRoot(store, &inventory, migrationSource{dir: globalDir, scope: ScopeGlobal, agents: []Agent{agent}}, seenHash)
		if normalized.CWD != "" {
			report.scanRoot(store, &inventory, migrationSource{dir: projectSkillsDir(adapter, normalized.CWD), scope: ScopeProject, agents: []Agent{agent}}, seenHash)
		}
	}
	for name, entry := range inventory.Packages {
		if entry.Owned {
			report.Owned++
		} else {
			report.Tracked++
			report.External++
		}
		_ = name
	}
	if err := store.SaveInventory(inventory); err != nil {
		return report, err
	}
	if len(report.Skipped) > 0 {
		report.Warnings = append(report.Warnings, "Conflicting installations were skipped: "+strings.Join(report.Skipped, ", "))
	}
	return report, nil
}

func (report *MigrationReport) scanRoot(store Store, inventory *InventoryFile, source migrationSource, seenHash map[string]string) {
	entries, err := os.ReadDir(source.dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			report.Warnings = append(report.Warnings, fmt.Sprintf("Could not read %s.", source.dir))
		}
		return
	}
	for _, entry := range entries {
		if len(report.Skipped) > maxInventoryWarnings {
			return
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(source.dir, name)
		isDir, err := isSkillDirectory(entry, path)
		if err != nil || !isDir {
			continue
		}
		frontmatter, ok, err := readSkillFrontmatter(filepath.Join(path, "SKILL.md"))
		if err != nil || !ok {
			continue
		}
		skillName := cleanMetadata(name, maxSkillNameLength)
		if skillName == "" || ValidateSkillName(skillName) != nil {
			continue
		}
		frontmatterName := cleanMetadata(frontmatter.Name, maxSkillNameLength)
		if frontmatterName != "" && frontmatterName != skillName {
			report.Warnings = append(report.Warnings, fmt.Sprintf("Skipped %s: metadata name does not match its directory.", path))
			continue
		}
		hash, err := folderContentHash(path)
		if err != nil {
			continue // bounded traversal rejects oversized/unsafe folders
		}
		report.consider(store, inventory, source, skillName, path, hash, seenHash)
	}
}

func (report *MigrationReport) consider(store Store, inventory *InventoryFile, source migrationSource, name, path, hash string, seenHash map[string]string) {
	if existing, ok := inventory.Packages[name]; ok {
		if existing.Owned {
			// A stray same-name directory next to an owned package is a
			// conflict; Zen never overwrites it.
			report.Conflict++
			return
		}
		// Already tracked external: a second different-content copy of the
		// same name is a duplicate/conflict; the tracked entry keeps the first.
		if previous, found := seenHash[name]; found && previous != hash {
			report.Conflict++
			report.Skipped = append(report.Skipped, path)
			return
		}
		report.External++
		return
	}
	if previous, found := seenHash[name]; found && previous != hash {
		report.Duplicate++
		report.Skipped = append(report.Skipped, path)
		return
	}
	seenHash[name] = hash
	now := store.now().UTC().Format(time.RFC3339)
	inventory.Packages[name] = PackageEntry{
		SkillName:        name,
		Source:           path,
		SourceType:       string(SourceTypeExternal),
		ContentHash:      hash,
		UpdatedAt:        now,
		InstalledAt:      now,
		Owned:            false,
		DiscoveredAgents: append([]Agent{}, source.agents...),
		DiscoveredScope:  source.scope,
	}
	report.Tracked++
	report.External++
}
