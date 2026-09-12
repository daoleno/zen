package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

type sddmService struct {
	PID   uint32
	Group string
	UID   uint32
}

// currentDisplay pins the X socket and authority descriptor selected by logind.
// No process scan, caller DISPLAY, or guessed authority filename is involved.
type currentDisplay struct {
	observation LinuxObservation
	service     sddmService
	peerPID     uint32
	socket      *net.UnixConn
	authority   *os.File
	process     *os.File
}

func (d *currentDisplay) Close() {
	if d.process != nil {
		_ = d.process.Close()
	}
	if d.authority != nil {
		_ = d.authority.Close()
	}
	if d.socket != nil {
		_ = d.socket.Close()
	}
}

func inspectSDDM(ctx context.Context) (sddmService, error) {
	var result sddmService
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := dbus.ConnectSystemBus(dbus.WithContext(ctx))
	if err != nil {
		return result, errors.New("sddm_system_bus_unavailable")
	}
	defer conn.Close()
	var path dbus.ObjectPath
	if conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1").CallWithContext(ctx, "org.freedesktop.systemd1.Manager.GetUnit", 0, "display-manager.service").Store(&path) != nil {
		return result, errors.New("active_sddm_service_required")
	}
	object := conn.Object("org.freedesktop.systemd1", path)
	var unit, service map[string]dbus.Variant
	if object.CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", 0, "org.freedesktop.systemd1.Unit").Store(&unit) != nil ||
		object.CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", 0, "org.freedesktop.systemd1.Service").Store(&service) != nil {
		return result, errors.New("sddm_service_identity_unavailable")
	}
	if unit["Id"].Value() != "sddm.service" || unit["ActiveState"].Value() != "active" {
		return result, errors.New("active_sddm_service_required")
	}
	result.PID, _ = service["MainPID"].Value().(uint32)
	result.Group, _ = service["ControlGroup"].Value().(string)
	account, err := user.Lookup("sddm")
	if err != nil {
		return result, errors.New("sddm_account_unavailable")
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil || uid == 0 || result.PID == 0 || !validSystemUnitCgroup(result.Group) || !ownerCgroupMember("/proc", result.PID, result.Group) {
		return result, errors.New("sddm_service_identity_unavailable")
	}
	result.UID = uint32(uid)
	return result, nil
}

func currentXSocket(display string) (string, error) {
	if !xDisplay.MatchString(display) {
		return "", errors.New("logind_x11_display_missing")
	}
	number, _, _ := strings.Cut(strings.TrimPrefix(display, ":"), ".")
	return "/tmp/.X11-unix/X" + number, nil
}

func validateCurrentSession(observation LinuxObservation, config HostConfig, sddmUID uint32) error {
	s := observation.Session
	if !s.Active || s.Seat != config.Seat || s.Backend != "x11" || s.ID == "" || s.BootID == "" {
		return errors.New("active_seat0_x11_required")
	}
	if observation.Class == "greeter" && s.UID == sddmUID {
		return nil
	}
	if observation.Class == "user" && s.UID == config.OwnerUID {
		return nil
	}
	return errors.New("active_session_is_not_enrolled_owner_or_sddm")
}

func xServerAuthority(args []byte) (string, error) {
	if len(args) == 0 || len(args) > 65536 || args[len(args)-1] != 0 {
		return "", errors.New("xserver_arguments_unavailable")
	}
	parts := bytes.Split(args[:len(args)-1], []byte{0})
	authority, seat := "", ""
	for i := 1; i < len(parts); i++ {
		option := string(parts[i])
		if option != "-auth" && option != "-seat" {
			continue
		}
		if i+1 == len(parts) || len(parts[i+1]) == 0 {
			return "", errors.New("ambiguous_xserver_arguments")
		}
		i++
		if option == "-auth" {
			if authority != "" {
				return "", errors.New("ambiguous_xserver_authority")
			}
			authority = string(parts[i])
		} else {
			if seat != "" {
				return "", errors.New("ambiguous_xserver_seat")
			}
			seat = string(parts[i])
		}
	}
	if seat != "seat0" || !filepath.IsAbs(authority) {
		return "", errors.New("sddm_xserver_authority_not_identified")
	}
	return authority, nil
}

func openSDDMAuthority(path string, sddmUID uint32) (*os.File, error) {
	// /var/run is a root-owned alias on supported distributions. Resolve it,
	// then descriptor-walk only the SDDM runtime directory, never a home path.
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || !strings.HasPrefix(canonical, "/run/sddm/") {
		return nil, errors.New("authority_outside_sddm_runtime")
	}
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(fd) }()
	parts := strings.Split(strings.TrimPrefix(canonical, "/"), "/")
	for i, part := range parts {
		flags := unix.O_PATH | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
		if i == len(parts)-1 {
			flags = unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK
		}
		next, err := unix.Openat(fd, part, flags, 0)
		if err != nil {
			return nil, errors.New("sddm_authority_unavailable")
		}
		var st unix.Stat_t
		valid := unix.Fstat(next, &st) == nil && (st.Uid == 0 || i >= 1 && st.Uid == sddmUID) && st.Mode&0022 == 0
		if i == len(parts)-1 {
			valid = valid && st.Mode&unix.S_IFMT == unix.S_IFREG && st.Mode&0077 == 0 && st.Nlink == 1 && st.Size > 0 && st.Size <= 65536
		}
		if !valid {
			unix.Close(next)
			return nil, errors.New("unsafe_sddm_authority")
		}
		unix.Close(fd)
		fd = next
	}
	file := os.NewFile(uintptr(fd), "sddm-authority")
	fd = -1
	return file, nil
}

func selectCurrentDisplay(ctx context.Context, config HostConfig) (*currentDisplay, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("current_display_registration_requires_administrator")
	}
	observation, err := InspectLinux(ctx)
	if err != nil {
		return nil, fmt.Errorf("current display: %w", err)
	}
	service, err := inspectSDDM(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateCurrentSession(observation, config, service.UID); err != nil {
		return nil, err
	}
	address, err := currentXSocket(observation.Display)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", address, time.Second)
	if err != nil {
		return nil, errors.New("logind_x11_socket_unavailable")
	}
	d := &currentDisplay{observation: observation, service: service, socket: conn.(*net.UnixConn)}
	complete := false
	defer func() {
		if !complete {
			d.Close()
		}
	}()
	pid, uid, ok := peerCredentials(d.socket)
	if !ok || uid != 0 || pid <= 0 || !ownerCgroupMember("/proc", uint32(pid), service.Group) {
		return nil, errors.New("x11_socket_not_owned_by_root_sddm_xserver")
	}
	d.peerPID = uint32(pid)
	pidfd, err := unix.PidfdOpen(int(pid), 0)
	if err != nil {
		return nil, errors.New("xserver_lifetime_pin_unavailable; Linux pidfd support is required")
	}
	d.process = os.NewFile(uintptr(pidfd), "xserver-lifetime")
	executable, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil || filepath.Base(executable) != "Xorg" {
		return nil, errors.New("unsupported_sddm_xserver_executable")
	}
	pinned, err := OpenRootFile(executable, 0755)
	if err != nil {
		return nil, err
	}
	defer pinned.Close()
	actual, err := os.Stat(fmt.Sprintf("/proc/%d/exe", pid))
	want, statErr := pinned.Stat()
	if err != nil || statErr != nil || !os.SameFile(actual, want) {
		return nil, errors.New("xserver_identity_changed")
	}
	file, err := os.Open(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil, errors.New("xserver_arguments_unavailable")
	}
	args, err := io.ReadAll(io.LimitReader(file, 65537))
	file.Close()
	if err != nil {
		return nil, errors.New("xserver_arguments_unavailable")
	}
	authority, err := xServerAuthority(args)
	if err != nil {
		return nil, err
	}
	d.authority, err = openSDDMAuthority(authority, service.UID)
	if err != nil {
		return nil, err
	}
	if err := d.Revalidate(ctx, config); err != nil {
		return nil, err
	}
	complete = true
	return d, nil
}

func (d *currentDisplay) Revalidate(ctx context.Context, config HostConfig) error {
	if d.process == nil {
		return errors.New("xserver_lifetime_pin_missing")
	}
	if n, err := unix.Poll([]unix.PollFd{{Fd: int32(d.process.Fd()), Events: unix.POLLIN}}, 0); err != nil || n != 0 {
		return errors.New("xserver_exited_retry_setup")
	}
	current, err := InspectLinux(ctx)
	if err != nil || current != d.observation {
		return errors.New("active_display_changed_retry_setup")
	}
	service, err := inspectSDDM(ctx)
	if err != nil || service != d.service || !ownerCgroupMember("/proc", d.peerPID, service.Group) {
		return errors.New("sddm_xserver_changed_retry_setup")
	}
	return validateCurrentSession(current, config, service.UID)
}
