package host

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// OpenRootFile walks from a pinned root directory, refusing symlinks and
// writable ancestors. The returned descriptor, not the pathname, is authority.
func OpenRootFile(path string, mode uint32) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, errors.New("unsafe_host_path")
	}
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.New("host_file_unavailable")
	}
	defer func() { _ = unix.Close(fd) }()
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		flags := unix.O_PATH | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
		if i == len(parts)-1 {
			flags = unix.O_RDONLY | unix.O_NONBLOCK | unix.O_NOFOLLOW | unix.O_CLOEXEC
		}
		next, openErr := unix.Openat(fd, part, flags, 0)
		if openErr != nil {
			return nil, errors.New("host_file_unavailable")
		}
		var st unix.Stat_t
		statErr := unix.Fstat(next, &st)
		valid := statErr == nil && st.Uid == 0 && st.Mode&0022 == 0
		if i == len(parts)-1 {
			valid = valid && st.Mode&unix.S_IFMT == unix.S_IFREG && st.Mode&07777 == mode && st.Nlink == 1
		}
		if !valid {
			_ = unix.Close(next)
			return nil, errors.New("unsafe_host_file")
		}
		_ = unix.Close(fd)
		fd = next
	}
	file := os.NewFile(uintptr(fd), "host-capability")
	fd = -1
	return file, nil
}

func LoadRootConfig(path string) (HostConfig, error) {
	f, err := OpenRootFile(path, 0600)
	if err != nil {
		return HostConfig{}, err
	}
	defer f.Close()
	return ReadHostConfig(f)
}

// AuthenticateLocalPeer checks kernel credentials, not a claimed UID in JSON.
// SO_PASSCRED additionally binds every subsequent message to its actual sender,
// even if an authenticated socket descriptor is passed to another process.
func AuthenticateLocalPeer(conn *net.UnixConn, uid uint32) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return errors.New("invalid_broker_peer")
	}
	var inner error
	err = raw.Control(func(fd uintptr) {
		kind, e := unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_TYPE)
		if e != nil || kind != unix.SOCK_SEQPACKET {
			inner = errors.New("wrong_socket_type")
			return
		}
		var peer *unix.Ucred
		peer, inner = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if inner == nil && (peer.Uid != uid || peer.Pid <= 0) {
			inner = errors.New("wrong_peer")
		}
		if inner == nil {
			inner = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_PASSCRED, 1)
		}
	})
	if err != nil || inner != nil {
		return errors.New("invalid_broker_peer")
	}
	return nil
}

const maxCapabilityMessage = 4096

// ReceiveCapability accepts exactly one bounded SOCK_SEQPACKET message and at
// most one descriptor. Every received FD is closed on rejection, including
// truncated ancillary messages. Call AuthenticateLocalPeer before any traffic.
// Payloads must contain metadata only, never credentials or Xauthority bytes.
func ReceiveCapability(conn *net.UnixConn, uid uint32, requireFD bool) ([]byte, *os.File, error) {
	return ReceiveCapabilityWithin(conn, uid, requireFD, 2*time.Second)
}

// ReceiveCapabilityWithin is ReceiveCapability with an explicit read deadline.
// The owner admission may legitimately wait longer than the default while the
// broker proves the canonical owner over the system bus.
func ReceiveCapabilityWithin(conn *net.UnixConn, uid uint32, requireFD bool, timeout time.Duration) ([]byte, *os.File, error) {
	data := make([]byte, maxCapabilityMessage)
	oob := make([]byte, unix.CmsgSpace(4*8)+unix.CmsgSpace(unix.SizeofUcred))
	if conn.SetReadDeadline(time.Now().Add(timeout)) != nil {
		return nil, nil, errors.New("broker_read_failed")
	}
	n, oobn, flags, _, err := conn.ReadMsgUnix(data, oob)
	var fds []int
	accepted := false
	defer func() {
		if !accepted {
			for _, fd := range fds {
				_ = unix.Close(fd)
			}
		}
	}()
	messages, parseErr := unix.ParseSocketControlMessage(oob[:oobn])
	valid := err == nil && parseErr == nil && n > 0 && flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) == 0
	credentials := 0
	for _, msg := range messages {
		switch {
		case msg.Header.Level == unix.SOL_SOCKET && msg.Header.Type == unix.SCM_RIGHTS:
			rights, e := unix.ParseUnixRights(&msg)
			fds = append(fds, rights...)
			valid = valid && e == nil
		case msg.Header.Level == unix.SOL_SOCKET && msg.Header.Type == unix.SCM_CREDENTIALS:
			credentials++
			cred, e := unix.ParseUnixCredentials(&msg)
			valid = valid && e == nil && cred != nil && cred.Uid == uid && cred.Pid > 0
		default:
			valid = false
		}
	}
	// Go's UnixConn receive path sets close-on-exec for received rights.
	want := 0
	if requireFD {
		want = 1
	}
	if !valid || credentials != 1 || len(fds) != want {
		return nil, nil, errors.New("invalid_broker_message")
	}
	var file *os.File
	if requireFD {
		file = os.NewFile(uintptr(fds[0]), "session-capability")
	}
	accepted = true
	return data[:n], file, nil
}

func SendCapability(conn *net.UnixConn, data []byte, file *os.File) error {
	if len(data) == 0 || len(data) > maxCapabilityMessage {
		return errors.New("invalid_broker_message")
	}
	var rights []byte
	if file != nil {
		rights = unix.UnixRights(int(file.Fd()))
	}
	if conn.SetWriteDeadline(time.Now().Add(2*time.Second)) != nil {
		return errors.New("broker_write_failed")
	}
	n, oobn, err := conn.WriteMsgUnix(data, rights, nil)
	if err != nil || n != len(data) || oobn != len(rights) {
		return errors.New("broker_write_failed")
	}
	return nil
}
