package host

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
	"gopkg.in/ini.v1"
)

const installJournalPath = "/etc/zen/desktop-install.json"

func lockInstaller() (*os.File, error) {
	parent, name, err := safeParent("/etc/zen/desktop-install.lock", true)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parent)
	fd, err := unix.Openat(parent, name, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, errors.New("installer_lock_unavailable")
	}
	f := os.NewFile(uintptr(fd), "installer-lock")
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 || st.Nlink != 1 || unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		f.Close()
		return nil, errors.New("installer_locked_or_unsafe")
	}
	return f, nil
}

type installedFile struct {
	Path         string `json:"path"`
	SHA256       string `json:"sha256"`
	Before       []byte `json:"before,omitempty"`
	BeforePath   string `json:"beforePath,omitempty"`
	BeforeSHA256 string `json:"beforeSha256,omitempty"`
	Mode         uint32 `json:"mode"`
	Exists       bool   `json:"existed"`
}

type installJournal struct {
	Version int `json:"version"`
	// Previous points at the durable copy of the journal that this upgrade
	// replaced. It is retained until an explicit rollback restores that
	// installation, so a successful upgrade always leaves one coherent
	// rollback step.
	Previous string          `json:"previous,omitempty"`
	Files    []installedFile `json:"files"`
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// safeParent pins every root-owned directory. Neither installation nor rollback
// follows an administrator-config path through a symlink or writable ancestor.
func safeParent(path string, create bool) (int, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return -1, "", errors.New("unsafe_install_path")
	}
	fd, err := unix.Open("/", unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, "", err
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for _, part := range parts[:len(parts)-1] {
		next, e := unix.Openat(fd, part, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e == unix.ENOENT && create {
			if e = unix.Mkdirat(fd, part, 0755); e == nil {
				e = unix.Fsync(fd)
			}
			if e == nil {
				next, e = unix.Openat(fd, part, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			}
		}
		unix.Close(fd)
		if e != nil {
			return -1, "", errors.New("unsafe_install_parent")
		}
		fd = next
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || st.Uid != 0 || st.Mode&0022 != 0 {
			unix.Close(fd)
			return -1, "", errors.New("unsafe_install_parent")
		}
	}
	return fd, parts[len(parts)-1], nil
}

func writeRootFile(path string, data []byte, mode uint32) error {
	parent, name, err := safeParent(path, true)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	// The name is created exclusively in a non-writable root-owned directory.
	temporary := ".zen-install-" + name
	fd, err := unix.Openat(parent, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return errors.New("install_stage_failed")
	}
	f := os.NewFile(uintptr(fd), "install-stage")
	defer f.Close()
	defer unix.Unlinkat(parent, temporary, 0)
	if _, err = f.Write(data); err != nil || f.Chmod(os.FileMode(mode)) != nil || f.Sync() != nil {
		return errors.New("install_stage_failed")
	}
	if unix.Renameat(parent, temporary, parent, name) != nil || unix.Fsync(parent) != nil {
		return errors.New("install_commit_failed")
	}
	return nil
}

func readInstallSource(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("install_binary_unavailable")
	}
	f := os.NewFile(uintptr(fd), "install-source")
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() < 4 || st.Size() > 64<<20 {
		return nil, errors.New("invalid_install_binary")
	}
	data, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
	if err != nil || len(data) < 4 || len(data) > 64<<20 || !bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		return nil, errors.New("invalid_install_binary")
	}
	return data, nil
}

func sddmConfiguration() (*ini.File, string, string, error) {
	merged := ini.Empty()
	for _, directory := range []string{"/usr/lib/sddm/sddm.conf.d", "/etc/sddm.conf.d"} {
		files, err := filepath.Glob(directory + "/*.conf")
		if err != nil {
			return nil, "", "", err
		}
		sort.Strings(files)
		for _, path := range files {
			file, err := OpenRootFile(path, 0644)
			if err != nil {
				return nil, "", "", err
			}
			err = appendSDDMConfig(merged, file)
			file.Close()
			if err != nil {
				return nil, "", "", err
			}
		}
	}
	main := ini.Empty()
	if _, err := os.Lstat("/etc/sddm.conf"); err == nil {
		file, err := OpenRootFile("/etc/sddm.conf", 0644)
		if err != nil {
			return nil, "", "", err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 65537))
		if err != nil || len(data) > 65536 {
			return nil, "", "", errors.New("sddm_config_too_large")
		}
		if err = merged.Append(data); err != nil {
			return nil, "", "", err
		}
		main, err = ini.Load(data)
		if err != nil {
			return nil, "", "", err
		}
	} else if !os.IsNotExist(err) {
		return nil, "", "", err
	}
	if merged.Section("General").Key("DisplayServer").MustString("x11") != "x11" {
		return nil, "", "", errors.New("sddm_x11_required")
	}
	start := merged.Section("X11").Key("DisplayCommand").MustString("/usr/share/sddm/scripts/Xsetup")
	stop := merged.Section("X11").Key("DisplayStopCommand").MustString("/usr/share/sddm/scripts/Xstop")
	for _, path := range []string{start, stop} {
		if strings.ContainsAny(path, "'\n\r") || strings.HasPrefix(path, "/usr/libexec/zen/") {
			return nil, "", "", errors.New("unsupported_sddm_hook")
		}
		f, err := OpenRootFile(path, 0755)
		if err != nil {
			return nil, "", "", err
		}
		f.Close()
	}
	return main, start, stop, nil
}

func appendSDDMConfig(merged *ini.File, reader io.Reader) error {
	// ini.Append reloads every retained source. Keep bounded bytes, not file
	// handles that close before the next fragment is appended.
	data, err := io.ReadAll(io.LimitReader(reader, 65537))
	if err != nil || len(data) > 65536 {
		return errors.New("sddm_config_too_large")
	}
	return merged.Append(data)
}

// InstallLinux installs one reviewed zen ELF as the broker/agent identity, but never
// starts, stops or enables a service. Activation is an explicit installer CLI
// operation. The existing canonical owner unit/state is referenced, not copied.
func InstallLinux(config HostConfig, binarySource string) error {
	if os.Geteuid() != 0 || config.Validate() != nil {
		return errors.New("invalid_install_owner")
	}
	lock, err := lockInstaller()
	if err != nil {
		return err
	}
	defer lock.Close()
	return installLinuxLocked(config, binarySource)
}

// buildInstallPlan renders the complete file set for an install or upgrade, so
// both transactions always write the identical reviewed plan.
func buildInstallPlan(config HostConfig, binarySource string) (InstallPlan, error) {
	binaryData, err := readInstallSource(binarySource)
	if err != nil {
		return InstallPlan{}, err
	}
	sddm, start, stop, err := sddmConfiguration()
	if err != nil {
		return InstallPlan{}, err
	}
	plan, err := PrepareLinuxInstall(config)
	if err != nil {
		return InstallPlan{}, err
	}
	sddm.Section("X11").Key("DisplayCommand").SetValue("/usr/libexec/zen/sddm-start")
	sddm.Section("X11").Key("DisplayStopCommand").SetValue("/usr/libexec/zen/sddm-stop")
	var sddmData bytes.Buffer
	if _, err := sddm.WriteTo(&sddmData); err != nil {
		return InstallPlan{}, err
	}
	plan.Files = append(plan.Files,
		PlannedFile{Path: InstalledBinary, Mode: 0755, Content: string(binaryData)},
		PlannedFile{Path: "/usr/libexec/zen/sddm-start", Mode: 0755, Content: "#!/bin/sh\n'" + start + "' \"$@\" || exit $?\n/usr/libexec/zen/zen desktop-host --register start || :\n"},
		PlannedFile{Path: "/usr/libexec/zen/sddm-stop", Mode: 0755, Content: "#!/bin/sh\n/usr/libexec/zen/zen desktop-host --register stop || :\nexec '" + stop + "' \"$@\"\n"},
		PlannedFile{Path: "/etc/sddm.conf", Mode: 0644, Content: sddmData.String()},
	)
	return plan, nil
}

func installLinuxLocked(config HostConfig, binarySource string) error {
	if _, err := os.Lstat(installJournalPath); !os.IsNotExist(err) {
		return errors.New("installation_already_exists")
	}
	plan, err := buildInstallPlan(config, binarySource)
	if err != nil {
		return err
	}
	journal := installJournal{Version: 1}
	for _, file := range plan.Files {
		entry := installedFile{Path: file.Path, SHA256: digest([]byte(file.Content)), Mode: file.Mode}
		if _, err := os.Lstat(file.Path); err == nil {
			if file.Path != "/etc/sddm.conf" {
				return errors.New("install_destination_exists")
			}
			before, err := OpenRootFile(file.Path, 0644)
			if err != nil {
				return err
			}
			entry.Before, err = io.ReadAll(io.LimitReader(before, 65537))
			before.Close()
			if err != nil || len(entry.Before) > 65536 {
				return errors.New("invalid_install_backup")
			}
			entry.Exists = true
		} else if !os.IsNotExist(err) {
			return errors.New("install_destination_unavailable")
		}
		journal.Files = append(journal.Files, entry)
	}
	data, _ := json.Marshal(journal)
	if err := writeRootFile(installJournalPath, data, 0600); err != nil {
		return err
	}
	for _, file := range plan.Files {
		if err := writeRootFile(file.Path, []byte(file.Content), file.Mode); err != nil {
			return errors.New("install_incomplete_use_rollback")
		}
	}
	return nil
}

func RollbackLinuxInstall() error {
	if os.Geteuid() != 0 {
		return errors.New("root_required")
	}
	lock, err := lockInstaller()
	if err != nil {
		return err
	}
	defer lock.Close()
	return rollbackLinuxLocked()
}

func rollbackLinuxLocked() error {
	return rollbackWithIO(rootUpgradeIO())
}

// rollbackWithIO restores the installation recorded by the journal and then
// either reinstates the retained previous journal (one-step rollback of a
// successful upgrade) or removes the journal for a fresh-install uninstall.
// The previous-install metadata is written before any backup is consumed, so a
// metadata failure stays retryable; administrator changes still refuse.
func rollbackWithIO(io upgradeIO) error {
	journal, err := loadJournalWithIO(io)
	if err != nil {
		return err
	}
	// Preflight every required restore input before the first write: a missing
	// or malformed previous journal must not leave the installed files changed.
	previousBytes := []byte(nil)
	if journal.Previous != "" {
		previousBytes, err = io.read(journal.Previous, 0600)
		if err != nil {
			return errors.New("previous_install_journal_missing")
		}
		var parsed installJournal
		if decodeMessage(previousBytes, &parsed) != nil || parsed.Version != 1 {
			return errors.New("invalid_previous_install_journal")
		}
	}
	restored, err := restoreJournalWithBackups(journal, io)
	if err != nil {
		return err
	}
	if journal.Previous != "" {
		var previous installJournal
		if decodeMessage(previousBytes, &previous) != nil || previous.Version != 1 {
			return errors.New("invalid_previous_install_journal")
		}
		// Older history referenced by that journal was pruned when this
		// upgrade committed; the restored install is the current management
		// state and must not point at removed backups.
		previous.Previous = ""
		data, _ := json.Marshal(previous)
		if err := io.write(installJournalPath, data, 0600); err != nil {
			return err
		}
		for _, backup := range restored {
			if err := io.remove(backup); err != nil {
				return err
			}
		}
		return io.remove(journal.Previous)
	}
	for _, backup := range restored {
		if err := io.remove(backup); err != nil {
			return err
		}
	}
	parent, name, err := safeParent(installJournalPath, false)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	if err := unix.Unlinkat(parent, name, 0); err != nil {
		return err
	}
	return unix.Fsync(parent)
}
