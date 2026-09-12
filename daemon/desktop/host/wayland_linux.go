package host

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"golang.org/x/sys/unix"
)

// waylandTarget is the owner's active Wayland compositor connection: the
// protected runtime directory, the standard session-bus socket and the one
// running compositor socket. It is discovered, never guessed from the network.
type waylandTarget struct {
	RuntimeDir string
	BusAddress string
	Display    string
}

var waylandSocketName = regexp.MustCompile(`^wayland-[0-9]{1,4}$`)

// discoverWaylandTarget locates the active Wayland target for uid under the
// system runtime root. Only root-owned discovery may run it: the caller is the
// broker after logind reported an active owner session.
func discoverWaylandTarget(uid uint32) (waylandTarget, error) {
	return discoverWaylandTargetAt("/run/user", uid)
}

// discoverWaylandTargetAt validates the per-user runtime boundary and reports
// exactly one running compositor. Zero or multiple held compositor sockets fail
// closed with a precise code instead of sharing an arbitrary surface.
func discoverWaylandTargetAt(root string, uid uint32) (waylandTarget, error) {
	runtime := filepath.Join(root, strconv.FormatUint(uint64(uid), 10))
	var st unix.Stat_t
	if err := unix.Lstat(runtime, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != uid || st.Mode&0077 != 0 {
		return waylandTarget{}, errors.New("wayland_runtime_unavailable")
	}
	bus := filepath.Join(runtime, "bus")
	if err := unix.Lstat(bus, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFSOCK || st.Uid != uid {
		return waylandTarget{}, errors.New("wayland_bus_unavailable")
	}
	entries, err := os.ReadDir(runtime)
	if err != nil {
		return waylandTarget{}, errors.New("wayland_runtime_unavailable")
	}
	var held []string
	for _, entry := range entries {
		name := entry.Name()
		if !waylandSocketName.MatchString(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Mode()&os.ModeSocket == 0 {
			continue
		}
		if err := unix.Lstat(filepath.Join(runtime, name), &st); err != nil || st.Uid != uid {
			continue
		}
		if waylandSocketHeld(filepath.Join(runtime, name+".lock")) {
			held = append(held, name)
		}
	}
	if len(held) == 0 {
		return waylandTarget{}, errors.New("wayland_display_unavailable")
	}
	sort.Strings(held)
	if len(held) > 1 {
		return waylandTarget{}, errors.New("wayland_display_ambiguous")
	}
	return waylandTarget{RuntimeDir: runtime, BusAddress: "unix:path=" + bus, Display: held[0]}, nil
}

// waylandSocketHeld probes libwayland's advisory exclusive lock on the socket's
// lock file. A lock failure means a live compositor owns that socket; a free
// lock means the socket is stale. The probe never blocks and releases a lock it
// accidentally acquired.
func waylandSocketHeld(lock string) bool {
	fd, err := unix.Open(lock, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false
	}
	defer unix.Close(fd)
	if unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return true
	}
	_ = unix.Flock(fd, unix.LOCK_UN)
	return false
}
