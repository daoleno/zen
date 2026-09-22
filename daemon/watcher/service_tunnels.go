package watcher

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type ServiceTunnel struct {
	Status     string `json:"status"`
	URL        string `json:"url,omitempty"`
	Error      string `json:"error,omitempty"`
	Generation string `json:"generation"`
}

type quickTunnel struct {
	state  ServiceTunnel
	cancel context.CancelFunc
	done   chan struct{}
}

type serviceTunnels struct {
	mu    sync.Mutex
	items map[string]*quickTunnel
}

func (w *Watcher) tunnelOwner() *serviceTunnels {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.quickTunnels == nil {
		w.quickTunnels = &serviceTunnels{items: map[string]*quickTunnel{}}
	}
	return w.quickTunnels
}

func serviceGeneration(pid int) string {
	started, ok := processStartTimeFromProc(pid)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d:%d", pid, started.UnixNano())
}

func serviceInstanceGeneration(service SessionService, sockets []listeningSocket) string {
	birth := serviceGeneration(service.PID)
	if birth == "" {
		return ""
	}
	listeners := []string{}
	for _, socket := range sockets {
		if socket.pid == service.PID && socket.port == service.Port && socket.inode != "" {
			listeners = append(listeners, socket.inode+"@"+socket.bind)
		}
	}
	if len(listeners) == 0 {
		return ""
	}
	sort.Strings(listeners)
	return birth + ":" + strings.Join(listeners, ",")
}

func (w *Watcher) decorateServiceTunnels(services []SessionService, sockets []listeningSocket) {
	owner := w.tunnelOwner()
	owner.mu.Lock()
	defer owner.mu.Unlock()
	for index := range services {
		service := &services[index]
		service.Generation = serviceInstanceGeneration(*service, sockets)
		if tunnel := owner.items[service.ID]; tunnel != nil && tunnel.state.Generation == service.Generation {
			state := tunnel.state
			service.Tunnel = &state
		}
	}
}

func (w *Watcher) StopServiceTunnels() {
	owner := w.tunnelOwner()
	owner.mu.Lock()
	items := make([]*quickTunnel, 0, len(owner.items))
	for _, tunnel := range owner.items {
		tunnel.cancel()
		items = append(items, tunnel)
	}
	owner.mu.Unlock()
	for _, tunnel := range items {
		select {
		case <-tunnel.done:
		case <-time.After(5 * time.Second):
		}
	}
}

func (w *Watcher) ServiceTunnelAction(id, generation, action string) (ServiceTunnel, error) {
	owner := w.tunnelOwner()
	if action == "stop" || action == "status" {
		owner.mu.Lock()
		tunnel := owner.items[id]
		if tunnel == nil || tunnel.state.Generation != generation {
			owner.mu.Unlock()
			return ServiceTunnel{Status: "stopped", Generation: generation}, nil
		}
		if action == "stop" {
			tunnel.state.Status = "stopping"
			select {
			case <-tunnel.done:
				tunnel.state.Status = "stopped"
				tunnel.state.Error = ""
			default:
			}
			tunnel.state.URL = ""
			tunnel.cancel()
		}
		state := tunnel.state
		owner.mu.Unlock()
		return state, nil
	}
	if action != "start" {
		return ServiceTunnel{}, fmt.Errorf("unsupported tunnel action")
	}
	snapshot, err := w.DiscoverSessionServices()
	if err != nil {
		return ServiceTunnel{}, err
	}
	var selected *SessionService
	for _, service := range snapshot.Services {
		if service.ID == id {
			copy := service
			selected = &copy
			break
		}
	}
	if selected == nil || generation == "" || selected.Generation != generation {
		return ServiceTunnel{}, fmt.Errorf("Service changed. Refresh Services before sharing it.")
	}
	if selected.PID == os.Getpid() || privateService(*selected) {
		return ServiceTunnel{}, fmt.Errorf("This control service cannot be published")
	}
	owner.mu.Lock()
	if previous := owner.items[id]; previous != nil {
		if previous.state.Generation == generation && (previous.state.Status == "starting" || previous.state.Status == "running" || previous.state.Status == "stopping") {
			state := previous.state
			owner.mu.Unlock()
			return state, nil
		}
		previous.cancel()
		select {
		case <-previous.done:
		default:
			state := previous.state
			state.Status = "stopping"
			state.URL = ""
			owner.mu.Unlock()
			return state, nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	tunnel := &quickTunnel{state: ServiceTunnel{Status: "starting", Generation: generation}, cancel: cancel, done: make(chan struct{})}
	owner.items[id] = tunnel
	owner.mu.Unlock()
	go w.runServiceTunnel(ctx, owner, tunnel, *selected)
	return ServiceTunnel{Status: "starting", Generation: generation}, nil
}

func privateService(service SessionService) bool {
	value := strings.ToLower(service.Command + " " + service.Process)
	for _, marker := range []string{"zen-dev", "zen serve", "codex app-server", "dsh-session", "deepseek-harness", "cloudflared", "zen-candidate"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func (w *Watcher) serviceOriginAlive(service SessionService) bool {
	if service.Generation == "" {
		return false
	}
	sockets, err := w.listeningSocketsForServices()
	if err != nil {
		return false
	}
	return serviceInstanceGeneration(service, sockets) == service.Generation
}

func serviceOriginURL(service SessionService) (*url.URL, error) {
	host := "127.0.0.1"
	hasIPv4 := false
	for _, bind := range service.Binds {
		if bind == "0.0.0.0" || bind == "*" || strings.Contains(bind, ".") {
			hasIPv4 = true
			break
		}
	}
	if !hasIPv4 && len(service.Binds) > 0 {
		host = "::1"
	}
	// A listener bound to one local interface may not accept loopback connections.
	for _, bind := range service.Binds {
		if bind != "0.0.0.0" && bind != "*" && bind != "::" && net.ParseIP(bind) != nil {
			host = bind
			break
		}
	}
	return url.Parse("http://" + net.JoinHostPort(host, fmt.Sprint(service.Port)))
}

var quickTunnelURL = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com\b`)

func quickTunnelLogURL(line []byte) string {
	var record map[string]json.RawMessage
	if json.Unmarshal(line, &record) != nil {
		return ""
	}
	for _, key := range []string{"url", "message"} {
		var value string
		if json.Unmarshal(record[key], &value) == nil {
			if match := quickTunnelURL.FindString(value); match != "" {
				return match
			}
		}
	}
	return ""
}

func (w *Watcher) runServiceTunnel(ctx context.Context, owner *serviceTunnels, tunnel *quickTunnel, service SessionService) {
	defer close(tunnel.done)
	defer tunnel.cancel()
	set := func(status, publicURL, message string) {
		owner.mu.Lock()
		defer owner.mu.Unlock()
		if owner.items[service.ID] == tunnel {
			tunnel.state.Status = status
			tunnel.state.URL = publicURL
			tunnel.state.Error = message
		}
	}
	defer func() {
		owner.mu.Lock()
		defer owner.mu.Unlock()
		if tunnel.state.Status != "failed" {
			tunnel.state.Status = "stopped"
		}
		tunnel.state.URL = ""
	}()
	fail := func(err error) {
		if ctx.Err() == nil {
			set("failed", "", err.Error())
		}
	}
	origin, err := serviceOriginURL(service)
	if err != nil {
		fail(err)
		return
	}
	if !w.serviceOriginAlive(service) {
		fail(fmt.Errorf("The origin service is no longer running"))
		return
	}
	probeCtx, cancelProbe := context.WithTimeout(ctx, 1500*time.Millisecond)
	request, _ := http.NewRequestWithContext(probeCtx, http.MethodHead, origin.String(), nil)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	cancelProbe()
	if err != nil {
		fail(fmt.Errorf("No local HTTP response. Only HTTP services support Quick Tunnels."))
		return
	}
	response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		fail(fmt.Errorf("This service requires authentication. Use its private access method."))
		return
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fail(err)
		return
	}
	defer listener.Close()
	proxy := httputil.NewSingleHostReverseProxy(origin)
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if !w.serviceOriginAlive(service) {
			return nil, fmt.Errorf("origin service changed")
		}
		connection, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if !w.serviceOriginAlive(service) {
			connection.Close()
			return nil, fmt.Errorf("origin service changed")
		}
		return connection, nil
	}}
	proxy.Transport = transport
	defer transport.CloseIdleConnections()
	proxy.ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(writer, "The selected service is unavailable", http.StatusServiceUnavailable)
	}
	proxyServer := &http.Server{Handler: proxy, ReadHeaderTimeout: 5 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	go proxyServer.Serve(listener)
	defer proxyServer.Close()
	directory, err := os.MkdirTemp("", "zen-quick-tunnel-")
	if err != nil {
		fail(err)
		return
	}
	defer os.RemoveAll(directory)
	config := filepath.Join(directory, "config.yml")
	if err := os.WriteFile(config, []byte("{}\n"), 0600); err != nil {
		fail(err)
		return
	}
	command := exec.CommandContext(ctx, "cloudflared", "tunnel", "--config", config, "--no-autoupdate", "--output", "json", "--metrics", "127.0.0.1:0", "--url", "http://"+listener.Addr().String())
	command.Env = []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "TUNNEL_") || strings.HasPrefix(key, "CLOUDFLARE") {
			continue
		}
		command.Env = append(command.Env, entry)
	}
	if err := bindTunnelToDaemon(command); err != nil {
		fail(err)
		return
	}
	output, err := command.StdoutPipe()
	if err != nil {
		fail(err)
		return
	}
	command.Stderr = command.Stdout
	if err := command.Start(); err != nil {
		fail(err)
		return
	}
	// Scanner stores no logs or public URLs on disk.
	announced := make(chan string, 1)
	connected := make(chan struct{}, 1)
	scanned := make(chan struct{})
	go func() {
		defer close(scanned)
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 4096), 128<<10)
		for scanner.Scan() {
			var record struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(scanner.Bytes(), &record) == nil && strings.Contains(record.Message, "Registered tunnel connection") {
				select {
				case connected <- struct{}{}:
				default:
				}
			}
			if publicURL := quickTunnelLogURL(scanner.Bytes()); publicURL != "" {
				select {
				case announced <- publicURL:
				default:
				}
			}
		}
	}()
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	defer func() {
		tunnel.cancel()
		_ = command.Process.Kill()
		select {
		case <-exited:
		case <-time.After(3 * time.Second):
		}
		<-scanned
	}()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	startup := time.NewTimer(30 * time.Second)
	defer startup.Stop()
	publicURL := ""
	connectionReady := false
	for {
		select {
		case <-ctx.Done():
			return
		case value := <-announced:
			publicURL = value
			if connectionReady {
				set("running", publicURL, "")
				startup.Stop()
			}
		case <-connected:
			connectionReady = true
			if publicURL != "" {
				set("running", publicURL, "")
				startup.Stop()
			}
		case err := <-exited:
			exited <- err
			if err != nil {
				fail(fmt.Errorf("cloudflared exited. Retry the tunnel."))
			}
			return
		case <-startup.C:
			fail(fmt.Errorf("Cloudflare did not return a temporary URL. Check network access and retry."))
			return
		case <-ticker.C:
			if !w.serviceOriginAlive(service) {
				fail(fmt.Errorf("The origin service stopped or changed. Refresh Services."))
				return
			}
		}
	}
}
