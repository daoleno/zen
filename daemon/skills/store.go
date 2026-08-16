package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	inventoryVersion      = 1
	maxInventoryFileBytes = 4 << 20
	// maxPackageFiles bounds the number of files Zen copies into the store so
	// a hostile archive cannot exhaust disk or metadata.
	maxPackageFiles = 512
	// maxPackageBytes bounds total materialized bytes per package.
	maxPackageBytes = 64 << 20
	maxPackageDepth = 16
)

// Store is the Zen-owned canonical package store plus its central inventory.
//
// Layout (all under the Zen state directory):
//
//	<state>/skills/inventory.json   central lock/inventory metadata
//	<state>/skills/store/<name>/    canonical package content (SKILL.md + files)
//	<state>/skills/.tmp/            atomic staging area
//	<state>/skills/.rollback/<name>/ previous content for failed-update rollback
//	<state>/skills/.lock            mutation lock file
//
// Metadata never lives inside package directories, and Zen never writes
// metadata into third-party Agent skill directories.
type Store struct {
	StateDir string
	Home     string
	Now      func() time.Time
}

// store instance bookkeeping for the advisory file lock, keyed by state root.
var (
	storeLocksMu sync.Mutex
	storeLocks   = map[string]*os.File{}
)

func NewStore(home string) Store {
	return Store{StateDir: filepath.Join(home, ".zen"), Home: home}
}

func (s Store) Root() string          { return filepath.Join(s.StateDir, "skills") }
func (s Store) InventoryPath() string { return filepath.Join(s.Root(), "inventory.json") }
func (s Store) StoreDir() string      { return filepath.Join(s.Root(), "store") }
func (s Store) TmpDir() string        { return filepath.Join(s.Root(), ".tmp") }
func (s Store) RollbackDir() string   { return filepath.Join(s.Root(), ".rollback") }
func (s Store) PackageDir(name string) string {
	return filepath.Join(s.StoreDir(), name)
}

// Lock serializes mutations on this daemon host. The lock is advisory: every
// mutation path takes it before reading or writing inventory.json or the
// store, so concurrent requests cannot interleave package writes. Re-entrant
// lock requests on the same state directory are idempotent until Unlock.
func (s Store) Lock() error {
	key := s.Root()
	storeLocksMu.Lock()
	defer storeLocksMu.Unlock()
	if _, held := storeLocks[key]; held {
		return nil
	}
	if err := os.MkdirAll(s.Root(), 0o700); err != nil {
		return fmt.Errorf("create Skills store: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(s.Root(), ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open Skills lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return fmt.Errorf("lock Skills store: %w", err)
	}
	storeLocks[key] = file
	return nil
}

func (s Store) Unlock() {
	key := s.Root()
	storeLocksMu.Lock()
	defer storeLocksMu.Unlock()
	file, held := storeLocks[key]
	if !held {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
	delete(storeLocks, key)
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// PackageEntry is the central inventory truth for one package or tracked
// external skill. Metadata lives only in inventory.json, never in package dirs.
type PackageEntry struct {
	SkillName    string         `json:"skill_name"`
	Description  string         `json:"description,omitempty"`
	Source       string         `json:"source"`
	SourceType   string         `json:"source_type"`
	SourceURL    string         `json:"source_url,omitempty"`
	Ref          string         `json:"ref,omitempty"`
	Version      string         `json:"version,omitempty"`
	ContentHash  string         `json:"content_hash"`
	PreviousHash string         `json:"previous_hash,omitempty"`
	InstalledAt  string         `json:"installed_at"`
	UpdatedAt    string         `json:"updated_at"`
	Owned        bool           `json:"owned"` // true = content in the store
	Bindings     []BindingEntry `json:"bindings,omitempty"`
	// DiscoveredAgents/DiscoveredScope record where an external installation
	// was found during migration so adopt can rebind the same surfaces.
	DiscoveredAgents []Agent `json:"discovered_agents,omitempty"`
	DiscoveredScope  Scope   `json:"discovered_scope,omitempty"`
}

type BindingEntry struct {
	Agent      Agent       `json:"agent"`
	Scope      Scope       `json:"scope"`
	TargetPath string      `json:"target_path"`
	Enabled    bool        `json:"enabled"`
	BoundAt    string      `json:"bound_at"`
	Mode       BindingMode `json:"mode"`
	Note       string      `json:"note,omitempty"`
}

type InventoryFile struct {
	Version   int                     `json:"version"`
	UpdatedAt string                  `json:"updated_at"`
	Packages  map[string]PackageEntry `json:"packages"`
}

// LoadInventory reads and validates the central inventory. A missing file is
// an empty inventory (not an error); a malformed one is an error so mutations
// fail closed instead of writing over unknown state.
func (s Store) LoadInventory(required bool) (InventoryFile, error) {
	data, err := os.ReadFile(s.InventoryPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if required {
				return InventoryFile{}, errors.New("Skills inventory is missing")
			}
			return InventoryFile{Version: inventoryVersion, Packages: map[string]PackageEntry{}}, nil
		}
		return InventoryFile{}, fmt.Errorf("read Skills inventory: %w", err)
	}
	if len(data) == 0 || len(data) > maxInventoryFileBytes {
		return InventoryFile{}, errors.New("Skills inventory has invalid size")
	}
	var file InventoryFile
	if err := json.Unmarshal(data, &file); err != nil {
		return InventoryFile{}, errors.New("Skills inventory is corrupt")
	}
	if file.Version != inventoryVersion || file.Packages == nil {
		return InventoryFile{}, errors.New("Skills inventory has an unsupported schema")
	}
	for name, entry := range file.Packages {
		if ValidateSkillName(name) != nil || entry.SkillName != name {
			return InventoryFile{}, fmt.Errorf("Skills inventory contains an invalid package id %q", name)
		}
		if err := validatePackageEntry(entry); err != nil {
			return InventoryFile{}, fmt.Errorf("Skills inventory package %q is invalid: %w", name, err)
		}
	}
	return file, nil
}

func validatePackageEntry(entry PackageEntry) error {
	if entry.Source == "" || len(entry.Source) > 1024 || strings.ContainsRune(entry.Source, '\x00') {
		return errors.New("missing or invalid source")
	}
	switch SourceType(entry.SourceType) {
	case SourceTypeCatalog, SourceTypeGithub:
		if ValidateRepository(entry.Source) != nil {
			return errors.New("invalid catalog source")
		}
	case SourceTypeLocal, SourceTypeArchive, SourceTypeExternal:
		if !filepath.IsAbs(entry.Source) {
			return errors.New("local source must be an absolute path")
		}
	default:
		return errors.New("invalid source type")
	}
	if entry.ContentHash == "" || len(entry.ContentHash) > 64 {
		return errors.New("missing or invalid content hash")
	}
	if len(entry.Description) > 240 || len(entry.Ref) > maxRefLength || len(entry.SourceURL) > 1024 {
		return errors.New("metadata exceeds its bounds")
	}
	if len(entry.Bindings) > 12 {
		return errors.New("too many bindings")
	}
	seen := map[string]bool{}
	for _, binding := range entry.Bindings {
		if ValidateAgent(binding.Agent) != nil || ValidateScope(binding.Scope) != nil || !ValidateBindingMode(string(binding.Mode)) {
			return errors.New("invalid binding")
		}
		key := string(binding.Agent) + "/" + string(binding.Scope)
		if seen[key] {
			return errors.New("duplicate binding")
		}
		seen[key] = true
		if binding.TargetPath == "" || !filepath.IsAbs(binding.TargetPath) {
			return errors.New("binding target must be absolute")
		}
	}
	for _, agent := range entry.DiscoveredAgents {
		if ValidateAgent(agent) != nil {
			return errors.New("invalid discovered agent")
		}
	}
	if entry.DiscoveredScope != "" && ValidateScope(entry.DiscoveredScope) != nil {
		return errors.New("invalid discovered scope")
	}
	return nil
}

// SaveInventory writes the inventory atomically: stage in .tmp, fsync, rename
// over the live file. A reader can therefore never observe a torn inventory.
func (s Store) SaveInventory(file InventoryFile) error {
	if err := os.MkdirAll(s.TmpDir(), 0o700); err != nil {
		return fmt.Errorf("create Skills staging dir: %w", err)
	}
	file.Version = inventoryVersion
	file.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Skills inventory: %w", err)
	}
	return atomicWriteFile(s.InventoryPath(), data, 0o600)
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-inventory-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// RemovePackageDir removes the canonical store content for a package from the
// rollback-safe staging path used by uninstall: content is moved (not
// deleted) to .rollback one planning step at a time so a crashed uninstall
// never loses the user's data silently.
func (s Store) stageToRollback(name string) error {
	packageDir := s.PackageDir(name)
	if _, err := os.Stat(packageDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	rollback := s.RollbackDir()
	if err := os.MkdirAll(rollback, 0o700); err != nil {
		return err
	}
	dest := filepath.Join(rollback, name)
	_ = os.RemoveAll(dest)
	if err := os.Rename(packageDir, dest); err != nil {
		return err
	}
	return nil
}

func (s Store) RemoveRollback(name string) {
	_ = os.RemoveAll(filepath.Join(s.RollbackDir(), name))
}
