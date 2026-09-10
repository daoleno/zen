package host

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop"
	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

const OwnerSocket = "/run/zen-desktop/owner.sock"
const RegisterSocket = "/run/zen-desktop/register.sock"

type Challenge struct {
	Nonce      string  `json:"nonce"`
	Session    Session `json:"session"`
	Generation uint64  `json:"generation"`
}

type Admission struct {
	Challenge Challenge `json:"challenge"`
	Request   Request   `json:"request"`
}

type signedAdmission struct {
	Admission Admission `json:"admission"`
	PublicKey string    `json:"publicKey"`
	Signature string    `json:"signature"`
}

func decodeMessage(data []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("invalid_broker_message")
	}
	return nil
}

func sendJSON(conn *net.UnixConn, value any, file *os.File) error {
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New("invalid_broker_message")
	}
	return SendCapability(conn, data, file)
}

type registration struct {
	Action  string `json:"action"`
	Display string `json:"display"`
}

var xDisplay = regexp.MustCompile(`^:[0-9]{1,5}(\.[0-9]{1,2})?$`)

type broker struct {
	mu           sync.Mutex
	config       HostConfig
	authority    *os.File
	display      string
	session      Session
	sessionPath  string
	generation   uint64
	owner        *net.UnixConn
	process      *exec.Cmd
	media        net.Conn
	agent        net.Conn
	monitorReady bool
	faulted      bool
	window       time.Time
	attempts     int
	proxyWorkers sync.WaitGroup
}

// retireLocked must finish the old agent before a new generation is published.
func (b *broker) retireLocked() {
	owner := b.owner
	b.owner = nil
	if b.media != nil {
		_ = b.media.Close()
		b.media = nil
	}
	if b.agent != nil {
		_ = b.agent.Close()
		b.agent = nil
	}
	if b.process != nil {
		cmd := b.process
		b.process = nil
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(time.Second):
				b.faulted = true
			}
		}
	}
	if owner != nil {
		_ = owner.Close()
	}
	b.generation++
}

func (b *broker) observeLocked(ctx context.Context) error {
	observation, err := InspectLinux(ctx)
	s := observation.Session
	if err != nil || !b.monitorReady || b.faulted || b.authority == nil || observation.Display != b.display || s.Backend != "x11" || (s.Surface != Greeter && s.UID != b.config.OwnerUID) {
		if err != nil {
			log.Printf("desktop observation rejected: %v", err)
		} else {
			log.Printf("desktop session gate unavailable: monitor=%t fault=%t registration=%t displayMatch=%t x11=%t owner=%t", b.monitorReady, b.faulted, b.authority != nil, observation.Display == b.display, s.Backend == "x11", s.Surface == Greeter || s.UID == b.config.OwnerUID)
		}
		b.retireLocked()
		b.session = Session{}
		return errors.New("session_unavailable")
	}
	// The registration is a root-issued X server capability; agent startup must
	// additionally prove capture/control before the network publishes streaming.
	s.Ready, s.Control = true, true
	s.DisplayGeneration = b.generation
	previous := b.session
	previous.DisplayGeneration = s.DisplayGeneration
	if previous != s {
		b.retireLocked()
		s.DisplayGeneration = b.generation
	}
	b.session = s
	b.sessionPath = observation.Path
	return nil
}

func sessionSignal(signal *dbus.Signal, path, id string) bool {
	if signal == nil {
		return false
	}
	if signal.Name == "org.freedesktop.login1.Manager.SessionRemoved" {
		return len(signal.Body) > 0 && signal.Body[0] == id && id != ""
	}
	if signal.Name != "org.freedesktop.DBus.Properties.PropertiesChanged" || (string(signal.Path) != path && string(signal.Path) != "/org/freedesktop/login1/seat/seat0") {
		return false
	}
	if len(signal.Body) != 3 {
		return true
	}
	changed, ok := signal.Body[1].(map[string]dbus.Variant)
	invalidated, okInvalidated := signal.Body[2].([]string)
	if !ok || !okInvalidated {
		return true
	}
	for _, name := range []string{"ActiveSession", "CanGraphical", "Active", "State", "LockedHint", "Type", "Class", "User", "Seat", "Display"} {
		if _, ok := changed[name]; ok {
			return true
		}
		for _, invalid := range invalidated {
			if name == invalid {
				return true
			}
		}
	}
	return false
}

func (b *broker) register(conn *net.UnixConn) {
	defer conn.Close()
	if AuthenticateLocalPeer(conn, 0) != nil {
		return
	}
	// Both actions carry an FD, so the receiver never peeks at untrusted metadata
	// to decide how much ancillary authority to accept. Stop uses /dev/null.
	data, file, err := ReceiveCapability(conn, 0, true)
	if err != nil {
		return
	}
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	var request registration
	if decodeMessage(data, &request) != nil || !xDisplay.MatchString(request.Display) || (request.Action != "start" && request.Action != "stop") {
		return
	}
	if request.Action == "start" {
		var st unix.Stat_t
		flags, e := unix.FcntlInt(file.Fd(), unix.F_GETFL, 0)
		if unix.Fstat(int(file.Fd()), &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Size < 1 || st.Size > 65536 || st.Mode&0077 != 0 || e != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
			return
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if request.Action == "stop" && request.Display != b.display {
		_ = sendJSON(conn, map[string]bool{"ok": true}, nil)
		return
	}
	b.retireLocked()
	if b.authority != nil {
		_ = b.authority.Close()
	}
	b.authority, b.display, b.session = nil, "", Session{}
	if request.Action == "start" {
		b.authority, b.display = file, request.Display
		file = nil
	}
	_ = sendJSON(conn, map[string]bool{"ok": true}, nil)
}

func (b *broker) admit(ctx context.Context, conn *net.UnixConn) {
	defer conn.Close()
	if AuthenticateLocalPeer(conn, b.config.OwnerUID) != nil || !b.isCanonicalOwner(ctx, conn) {
		log.Print("desktop admission rejected: canonical_peer")
		return
	}
	if err := SendBrokerReady(conn); err != nil {
		log.Print("desktop admission rejected: ready_write")
		return
	}
	hello, _, err := ReceiveCapability(conn, b.config.OwnerUID, false)
	if err != nil || string(hello) != "hello" {
		log.Print("desktop admission rejected: hello")
		return
	}
	b.mu.Lock()
	if time.Since(b.window) >= time.Minute {
		b.window, b.attempts = time.Now(), 0
	}
	b.attempts++
	if b.attempts > 10 || b.owner != nil || b.observeLocked(ctx) != nil {
		log.Print("desktop admission rejected: session_or_busy")
		b.mu.Unlock()
		return
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		b.mu.Unlock()
		return
	}
	challenge := Challenge{Nonce: hex.EncodeToString(nonce[:]), Session: b.session, Generation: b.generation}
	b.mu.Unlock()
	if sendJSON(conn, challenge, nil) != nil {
		log.Print("desktop admission rejected: challenge_write")
		return
	}
	data, _, err := ReceiveCapability(conn, b.config.OwnerUID, false)
	if err != nil {
		log.Print("desktop admission rejected: proof_read")
		return
	}
	var proof signedAdmission
	if decodeMessage(data, &proof) != nil || proof.Admission.Challenge != challenge {
		log.Print("desktop admission rejected: challenge_binding")
		return
	}
	payload, _ := json.Marshal(proof.Admission)
	r := proof.Admission.Request
	if !auth.VerifyDesktopHostAdmission(b.config.HostID, proof.PublicKey, payload, proof.Signature) || r.HostID != b.config.HostID || !r.TLS || r.Mode != Unattended || r.DeviceID == "" || r.ConnectionID == "" || r.Fingerprint == [32]byte{} || r.Generation != challenge.Generation {
		log.Print("desktop admission rejected: signed_authority")
		return
	}
	b.mu.Lock()
	if b.owner != nil || b.observeLocked(ctx) != nil || b.generation != challenge.Generation || b.session != challenge.Session {
		log.Print("desktop admission rejected: changed_generation")
		b.mu.Unlock()
		return
	}
	file, cmd, err := b.startAgentLocked(r.Control)
	if err != nil {
		log.Printf("desktop agent unavailable: %v", err)
		b.mu.Unlock()
		return
	}
	b.owner, b.process = conn, cmd
	proxy, err := b.proxyLocked(conn, file)
	_ = file.Close()
	if err == nil {
		err = sendJSON(conn, challenge, proxy)
		_ = proxy.Close()
	}
	b.mu.Unlock()
	if err == nil {
		// Owner heartbeat is bounded and holds no device database. Revocation in
		// the canonical daemon closes this channel synchronously with its lease.
		for {
			data, _, e := ReceiveCapability(conn, b.config.OwnerUID, false)
			if e != nil || string(data) != "alive" {
				break
			}
		}
	}
	b.mu.Lock()
	if b.owner == conn {
		b.retireLocked()
	}
	b.mu.Unlock()
}

// The broker relays bounded packets without decoding them. Closing this relay
// under mu precedes agent cleanup, so no post-retirement media/input can pass
// while a codec or X server takes time to stop. GTK/codecs stay in the agent.
func (b *broker) proxyLocked(owner *net.UnixConn, agentFile *os.File) (*os.File, error) {
	agent, err := net.FileConn(agentFile)
	if err != nil {
		return nil, errors.New("agent_channel_failed")
	}
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		agent.Close()
		return nil, errors.New("proxy_channel_failed")
	}
	parent := os.NewFile(uintptr(fds[0]), "broker-proxy")
	child := os.NewFile(uintptr(fds[1]), "daemon-media")
	proxy, err := net.FileConn(parent)
	parent.Close()
	if err != nil {
		child.Close()
		agent.Close()
		return nil, errors.New("proxy_channel_failed")
	}
	b.agent, b.media = agent, proxy
	b.proxyWorkers.Add(2)
	generation := b.generation
	retire := func() {
		b.mu.Lock()
		if b.owner == owner && b.generation == generation {
			b.retireLocked()
		}
		b.mu.Unlock()
	}
	go func() {
		defer b.proxyWorkers.Done()
		defer retire()
		for {
			_ = agent.SetReadDeadline(time.Now().Add(15 * time.Second))
			kind, data, err := desktop.ReadPacket(agent)
			if err != nil {
				return
			}
			b.mu.Lock()
			if b.owner != owner || b.generation != generation {
				b.mu.Unlock()
				return
			}
			_ = proxy.SetWriteDeadline(time.Now().Add(2 * time.Second))
			var header [5]byte
			binary.BigEndian.PutUint32(header[:4], uint32(len(data)+1))
			header[4] = kind
			_, err = io.Copy(proxy, bytes.NewReader(header[:]))
			if err == nil {
				_, err = io.Copy(proxy, bytes.NewReader(data))
			}
			b.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer b.proxyWorkers.Done()
		defer retire()
		reader := bufio.NewReaderSize(proxy, 256)
		for {
			line, err := reader.ReadSlice('\n')
			if err != nil || len(line) > 255 {
				return
			}
			b.mu.Lock()
			if b.owner != owner || b.generation != generation {
				b.mu.Unlock()
				return
			}
			_ = agent.SetWriteDeadline(time.Now().Add(2 * time.Second))
			_, err = agent.Write(line)
			b.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return child, nil
}

// UID alone is insufficient: a terminal-only process under the same account
// must not impersonate the one boot-owned canonical daemon.
func (b *broker) isCanonicalOwner(ctx context.Context, peer *net.UnixConn) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := dbus.ConnectSystemBus(dbus.WithContext(ctx))
	if err != nil {
		return false
	}
	defer conn.Close()
	var unit dbus.ObjectPath
	if conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1").CallWithContext(ctx, "org.freedesktop.systemd1.Manager.GetUnit", 0, b.config.OwnerUnit).Store(&unit) != nil {
		return false
	}
	var value dbus.Variant
	if conn.Object("org.freedesktop.systemd1", unit).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, "org.freedesktop.systemd1.Service", "MainPID").Store(&value) != nil {
		return false
	}
	pid, ok := value.Value().(uint32)
	if !ok || pid == 0 {
		return false
	}
	raw, err := peer.SyscallConn()
	if err != nil {
		return false
	}
	matched := false
	if raw.Control(func(fd uintptr) {
		credentials, e := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		matched = e == nil && credentials.Pid > 0 && uint32(credentials.Pid) == pid && credentials.Uid == b.config.OwnerUID
	}) != nil {
		return false
	}
	return matched
}

func (b *broker) startAgentLocked(control bool) (*os.File, *exec.Cmd, error) {
	executable, err := OpenRootFile(InstalledBinary, 0755)
	if err != nil {
		return nil, nil, err
	}
	defer executable.Close()
	if err := BrokerMayLaunchAgent(executable); err != nil {
		return nil, nil, err
	}
	account, err := user.LookupId(strconv.FormatUint(uint64(b.session.UID), 10))
	if err != nil {
		return nil, nil, errors.New("agent_account_unavailable")
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil || gid == 0 || b.session.UID == 0 {
		return nil, nil, errors.New("invalid_agent_account")
	}
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, errors.New("agent_channel_failed")
	}
	parent := os.NewFile(uintptr(fds[0]), "agent-owner")
	child := os.NewFile(uintptr(fds[1]), "agent-channel")
	defer child.Close()
	mode := "view"
	if control {
		mode = "control"
	}
	cmd := exec.Command("/proc/self/fd/5", desktop.RoleAgent, b.display, mode)
	cmd.Env = []string{"PATH=/usr/bin", "LANG=C.UTF-8", "HOME=/nonexistent", "GST_REGISTRY_UPDATE=no", "GST_REGISTRY=/nonexistent/registry.bin", "GST_REGISTRY_FORK=no"}
	cmd.ExtraFiles = []*os.File{child, b.authority, executable}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: b.session.UID, Gid: uint32(gid), Groups: []uint32{}}, Pdeathsig: syscall.SIGTERM}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		log.Printf("desktop agent exec failed: %v", err)
		_ = parent.Close()
		return nil, nil, errors.New("agent_start_failed")
	}
	return parent, cmd, nil
}

// Subscribe before publishing readiness. A bus disconnect is denial; signals
// retire the old generation rather than being used as a synthetic grant.
func (b *broker) monitor(ctx context.Context) {
	conn, err := dbus.ConnectSystemBus(dbus.WithContext(ctx))
	if err != nil {
		return
	}
	defer conn.Close()
	signals := make(chan *dbus.Signal, 64)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)
	if conn.AddMatchSignal(dbus.WithMatchSender("org.freedesktop.login1")) != nil {
		return
	}
	b.mu.Lock()
	b.monitorReady = true
	b.mu.Unlock()
	defer func() { b.mu.Lock(); b.monitorReady = false; b.retireLocked(); b.mu.Unlock() }()
	for {
		select {
		case <-ctx.Done():
			return
		case <-conn.Context().Done():
			return
		case signal, ok := <-signals:
			if !ok || signal == nil {
				return
			}
			b.mu.Lock()
			if sessionSignal(signal, b.sessionPath, b.session.ID) {
				b.retireLocked()
				b.session = Session{}
				b.sessionPath = ""
			}
			b.mu.Unlock()
		}
	}
}

func rootListener(path string, uid uint32) (*net.UnixListener, error) {
	listener, err := net.ListenUnix("unixpacket", &net.UnixAddr{Name: path, Net: "unixpacket"})
	if err != nil {
		return nil, err
	}
	if os.Chmod(path, 0600) != nil || os.Chown(path, int(uid), -1) != nil {
		listener.Close()
		return nil, errors.New("broker_socket_permissions")
	}
	raw, err := listener.SyscallConn()
	if err != nil {
		listener.Close()
		return nil, errors.New("broker_socket_credentials")
	}
	var optionErr error
	err = raw.Control(func(fd uintptr) { optionErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_PASSCRED, 1) })
	if err != nil || optionErr != nil {
		listener.Close()
		return nil, errors.New("broker_socket_credentials")
	}
	return listener, nil
}

func ServeBroker(ctx context.Context, config HostConfig) error {
	if os.Geteuid() != 0 || config.Validate() != nil || !ownerUnitName.MatchString(config.OwnerUnit) {
		return errors.New("invalid_broker_owner")
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var capabilities [2]unix.CapUserData
	required := uint32(1<<unix.CAP_SETUID | 1<<unix.CAP_SETGID | 1<<unix.CAP_KILL | 1<<unix.CAP_CHOWN)
	if unix.Capget(&header, &capabilities[0]) != nil || capabilities[0].Effective&required != required {
		return errors.New("broker_required_capabilities_missing")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var st unix.Stat_t
	if unix.Lstat("/run/zen-desktop", &st) != nil || st.Uid != 0 || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Mode&0022 != 0 {
		return errors.New("unsafe_broker_runtime")
	}
	owner, err := rootListener(OwnerSocket, config.OwnerUID)
	if err != nil {
		return errors.New("broker_listener_failed")
	}
	defer owner.Close()
	registration, err := rootListener(RegisterSocket, 0)
	if err != nil {
		return errors.New("broker_listener_failed")
	}
	defer registration.Close()
	b := &broker{config: config}
	var workers sync.WaitGroup
	workers.Add(2)
	go func() { defer workers.Done(); b.monitor(ctx) }()
	go func() {
		defer workers.Done()
		for {
			conn, e := registration.AcceptUnix()
			if e != nil {
				return
			}
			b.register(conn)
		}
	}()
	go func() {
		<-ctx.Done()
		owner.Close()
		registration.Close()
		b.mu.Lock()
		b.retireLocked()
		b.mu.Unlock()
	}()
	for {
		conn, e := owner.AcceptUnix()
		if e != nil {
			break
		}
		b.admit(ctx, conn)
	}
	cancel()
	registration.Close()
	workers.Wait()
	b.mu.Lock()
	b.retireLocked()
	if b.authority != nil {
		b.authority.Close()
	}
	b.mu.Unlock()
	b.proxyWorkers.Wait()
	return nil
}

// RegisterDisplay is invoked only by the root SDDM wrapper. The authority file
// is opened once with O_NOFOLLOW and transferred by FD, never copied to config.
func RegisterDisplay(action, display, authority string) error {
	if os.Geteuid() != 0 || !xDisplay.MatchString(display) || (action != "start" && action != "stop") {
		return errors.New("invalid_registration")
	}
	if action == "stop" {
		authority = "/dev/null"
	}
	fd, err := unix.Open(authority, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return errors.New("authority_unavailable")
	}
	file := os.NewFile(uintptr(fd), "display-authority")
	defer file.Close()
	var conn *net.UnixConn
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err = net.DialUnix("unixpacket", nil, &net.UnixAddr{Name: RegisterSocket, Net: "unixpacket"})
		if err == nil || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		return errors.New("broker_unavailable")
	}
	defer conn.Close()
	if AuthenticateLocalPeer(conn, 0) != nil || sendJSON(conn, registration{Action: action, Display: display}, file) != nil {
		return errors.New("registration_failed")
	}
	data, _, err := ReceiveCapability(conn, 0, false)
	if err != nil || string(data) != `{"ok":true}` {
		return fmt.Errorf("registration_failed")
	}
	return nil
}
