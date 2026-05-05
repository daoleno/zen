package watcher

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SessionServiceSnapshot describes TCP services started under tmux sessions.
type SessionServiceSnapshot struct {
	GeneratedAt time.Time                 `json:"generated_at"`
	Interfaces  []SessionServiceInterface `json:"interfaces"`
	Services    []SessionService          `json:"services"`
}

// SessionServiceInterface is a reachable host address exposed to mobile clients.
type SessionServiceInterface struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Kind    string `json:"kind"`
}

// SessionServiceURL is a URL candidate for opening a discovered service.
type SessionServiceURL struct {
	Label   string `json:"label"`
	URL     string `json:"url"`
	Address string `json:"address"`
	Kind    string `json:"kind"`
}

// SessionService is a listening TCP port owned by a tmux pane process tree.
type SessionService struct {
	ID        string              `json:"id"`
	AgentID   string              `json:"agent_id"`
	AgentName string              `json:"agent_name"`
	Project   string              `json:"project,omitempty"`
	Cwd       string              `json:"cwd,omitempty"`
	Command   string              `json:"command,omitempty"`
	Process   string              `json:"process,omitempty"`
	PID       int                 `json:"pid"`
	Port      int                 `json:"port"`
	Protocol  string              `json:"protocol"`
	Binds     []string            `json:"binds"`
	URLs      []SessionServiceURL `json:"urls"`
	LocalOnly bool                `json:"local_only"`
}

type servicePane struct {
	target  string
	paneID  string
	name    string
	cwd     string
	command string
	panePID int
	active  bool
}

type listeningSocket struct {
	pid  int
	port int
	bind string
}

var ssPIDPattern = regexp.MustCompile(`pid=([0-9]+)`)

// DiscoverSessionServices scans tmux-owned process trees for listening TCP ports.
func (w *Watcher) DiscoverSessionServices() (SessionServiceSnapshot, error) {
	panes, err := listServicePanes()
	if err != nil {
		return SessionServiceSnapshot{}, err
	}

	interfaces := discoverServiceInterfaces()
	processes := snapshotProcesses()
	panesByPID := panesByProcess(processes, panes)
	sockets, err := listListeningSockets()
	if err != nil {
		return SessionServiceSnapshot{}, err
	}

	agentsByID := make(map[string]*classifierAgentSnapshot)
	for _, agent := range w.Agents() {
		agentsByID[agent.ID] = &classifierAgentSnapshot{
			name:    agent.Name,
			project: agent.Project,
			cwd:     agent.Cwd,
			command: agent.Command,
		}
	}

	servicesByKey := make(map[string]*SessionService)
	for _, socket := range sockets {
		pane, ok := panesByPID[socket.pid]
		if !ok || socket.port <= 0 {
			continue
		}

		key := fmt.Sprintf("%s|%d|%d", pane.target, socket.pid, socket.port)
		service := servicesByKey[key]
		if service == nil {
			agent := agentsByID[pane.target]
			agentName := formatAgentName(pane.name, pane.target)
			project := projectNameFromPath(pane.cwd)
			command := pane.command
			cwd := pane.cwd
			if agent != nil {
				if agent.name != "" {
					agentName = agent.name
				}
				if agent.project != "" {
					project = agent.project
				}
				if agent.cwd != "" {
					cwd = agent.cwd
				}
				if agent.command != "" {
					command = agent.command
				}
			}

			process := command
			if proc, ok := processes[socket.pid]; ok {
				process = strings.TrimSpace(proc.args)
				if process == "" {
					process = strings.TrimSpace(proc.comm)
				}
			}

			service = &SessionService{
				ID:        fmt.Sprintf("%s:%d:%d", pane.target, socket.pid, socket.port),
				AgentID:   pane.target,
				AgentName: agentName,
				Project:   project,
				Cwd:       cwd,
				Command:   command,
				Process:   process,
				PID:       socket.pid,
				Port:      socket.port,
				Protocol:  "tcp",
			}
			servicesByKey[key] = service
		}
		service.Binds = appendUnique(service.Binds, socket.bind)
	}

	services := make([]SessionService, 0, len(servicesByKey))
	for _, service := range servicesByKey {
		sort.Strings(service.Binds)
		if service.Binds == nil {
			service.Binds = []string{}
		}
		service.URLs = buildServiceURLs(service.Binds, service.Port, interfaces)
		if service.URLs == nil {
			service.URLs = []SessionServiceURL{}
		}
		service.LocalOnly = len(service.URLs) == 0
		services = append(services, *service)
	}

	sort.Slice(services, func(i, j int) bool {
		if services[i].Project != services[j].Project {
			return services[i].Project < services[j].Project
		}
		if services[i].AgentName != services[j].AgentName {
			return services[i].AgentName < services[j].AgentName
		}
		if services[i].Port != services[j].Port {
			return services[i].Port < services[j].Port
		}
		return services[i].PID < services[j].PID
	})

	return SessionServiceSnapshot{
		GeneratedAt: time.Now(),
		Interfaces:  nonNilServiceInterfaces(interfaces),
		Services:    services,
	}, nil
}

func nonNilServiceInterfaces(interfaces []SessionServiceInterface) []SessionServiceInterface {
	if interfaces == nil {
		return []SessionServiceInterface{}
	}
	return interfaces
}

type classifierAgentSnapshot struct {
	name    string
	project string
	cwd     string
	command string
}

func listServicePanes() ([]servicePane, error) {
	out, err := exec.Command("tmux", "list-panes", "-a", "-F", "#{session_name}:#{window_id}\t#{pane_id}\t#{window_name}\t#{pane_current_path}\t#{pane_current_command}\t#{pane_pid}\t#{pane_active}").CombinedOutput()
	if err != nil {
		output := strings.TrimSpace(string(out))
		if strings.Contains(output, "no server running") {
			return nil, nil
		}
		return nil, fmt.Errorf("tmux list-panes: %w", err)
	}

	var panes []servicePane
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 7)
		if len(parts) < 6 {
			continue
		}
		target := strings.TrimSpace(parts[0])
		sessionName := strings.SplitN(target, ":", 2)[0]
		if target == "" || strings.HasPrefix(sessionName, "zen-") {
			continue
		}

		panePID, _ := strconv.Atoi(strings.TrimSpace(parts[5]))
		pane := servicePane{
			target:  target,
			paneID:  strings.TrimSpace(parts[1]),
			name:    strings.TrimSpace(parts[2]),
			cwd:     strings.TrimSpace(parts[3]),
			command: strings.TrimSpace(parts[4]),
			panePID: panePID,
		}
		if len(parts) >= 7 {
			pane.active = strings.TrimSpace(parts[6]) == "1"
		}
		panes = append(panes, pane)
	}
	return panes, nil
}

func panesByProcess(processes map[int]processInfo, panes []servicePane) map[int]servicePane {
	result := make(map[int]servicePane)
	if len(processes) == 0 {
		return result
	}

	for _, pane := range panes {
		if pane.panePID <= 0 {
			continue
		}
		for _, pid := range descendantPIDsIncludingRoot(pane.panePID, processes) {
			if existing, exists := result[pid]; exists && existing.active {
				continue
			}
			result[pid] = pane
		}
	}
	return result
}

func descendantPIDsIncludingRoot(rootPID int, processes map[int]processInfo) []int {
	children := make(map[int][]int)
	for _, proc := range processes {
		children[proc.ppid] = append(children[proc.ppid], proc.pid)
	}

	result := []int{rootPID}
	queue := append([]int(nil), children[rootPID]...)
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		result = append(result, pid)
		queue = append(queue, children[pid]...)
	}
	return result
}

func listListeningSockets() ([]listeningSocket, error) {
	out, err := exec.Command("ss", "-H", "-ltnp").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ss listening sockets: %w", err)
	}

	return parseSSListeningSockets(string(out)), nil
}

func parseSSListeningSockets(output string) []listeningSocket {
	var sockets []listeningSocket
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "LISTEN" {
			continue
		}

		match := ssPIDPattern.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		pid, err := strconv.Atoi(match[1])
		if err != nil || pid <= 0 {
			continue
		}

		bind, port, ok := splitAddressPort(fields[3])
		if !ok || port <= 0 {
			continue
		}
		sockets = append(sockets, listeningSocket{pid: pid, port: port, bind: bind})
	}
	return sockets
}

func splitAddressPort(value string) (string, int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, false
	}

	address := ""
	portText := ""
	if strings.HasPrefix(value, "[") {
		end := strings.LastIndex(value, "]:")
		if end < 0 {
			return "", 0, false
		}
		address = strings.Trim(value[1:end], "[]")
		portText = value[end+2:]
	} else {
		idx := strings.LastIndex(value, ":")
		if idx < 0 {
			return "", 0, false
		}
		address = value[:idx]
		portText = value[idx+1:]
	}

	port, err := strconv.Atoi(portText)
	if err != nil {
		return "", 0, false
	}
	return normalizeBindAddress(address), port, true
}

func normalizeBindAddress(address string) string {
	address = strings.TrimSpace(strings.Trim(address, "[]"))
	if address == "" {
		return "*"
	}
	return address
}

func discoverServiceInterfaces() []SessionServiceInterface {
	netInterfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var result []SessionServiceInterface
	for _, iface := range netInterfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if shouldSkipServiceInterface(iface.Name) {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip, ok := addrToIP(addr)
			if !ok || ip.IsLoopback() {
				continue
			}

			kind := classifyServiceAddress(iface.Name, ip)
			if kind == "" {
				continue
			}
			result = append(result, SessionServiceInterface{
				Name:    iface.Name,
				Address: ip.String(),
				Kind:    kind,
			})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].Address < result[j].Address
	})
	return preferServiceInterfaces(result)
}

func preferServiceInterfaces(interfaces []SessionServiceInterface) []SessionServiceInterface {
	hasTailscaleIPv4 := false
	for _, iface := range interfaces {
		ip := net.ParseIP(iface.Address)
		if iface.Kind == "tailscale" && ip != nil && ip.To4() != nil {
			hasTailscaleIPv4 = true
			break
		}
	}
	if !hasTailscaleIPv4 {
		return interfaces
	}

	result := interfaces[:0]
	for _, iface := range interfaces {
		ip := net.ParseIP(iface.Address)
		if iface.Kind == "tailscale" && (ip == nil || ip.To4() == nil) {
			continue
		}
		result = append(result, iface)
	}
	return result
}

func shouldSkipServiceInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"docker", "br-", "veth", "virbr", "cni", "podman", "kube"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func addrToIP(addr net.Addr) (net.IP, bool) {
	switch value := addr.(type) {
	case *net.IPNet:
		return value.IP, true
	case *net.IPAddr:
		return value.IP, true
	default:
		return nil, false
	}
}

func classifyServiceAddress(ifaceName string, ip net.IP) string {
	if ip == nil {
		return ""
	}
	if ip4 := ip.To4(); ip4 != nil {
		if isTailscaleIPv4(ifaceName, ip4) {
			return "tailscale"
		}
		if isPrivateIPv4(ip4) {
			return "lan"
		}
		return ""
	}

	lowerName := strings.ToLower(ifaceName)
	if strings.Contains(lowerName, "tailscale") && !ip.IsLinkLocalUnicast() {
		return "tailscale"
	}
	return ""
}

func isPrivateIPv4(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 10 ||
		(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) ||
		(ip4[0] == 192 && ip4[1] == 168)
}

func isTailscaleIPv4(ifaceName string, ip net.IP) bool {
	lowerName := strings.ToLower(ifaceName)
	if strings.Contains(lowerName, "tailscale") || strings.HasPrefix(lowerName, "ts") {
		return true
	}
	ip4 := ip.To4()
	return ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
}

func buildServiceURLs(binds []string, port int, interfaces []SessionServiceInterface) []SessionServiceURL {
	if port <= 0 {
		return nil
	}

	scheme := "http"
	if port == 443 || port == 8443 {
		scheme = "https"
	}

	var urls []SessionServiceURL
	seen := make(map[string]bool)
	for _, bind := range binds {
		for _, candidate := range candidateInterfacesForBind(bind, interfaces) {
			host := candidate.Address
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
			url := fmt.Sprintf("%s://%s:%d", scheme, host, port)
			if seen[url] {
				continue
			}
			seen[url] = true
			urls = append(urls, SessionServiceURL{
				Label:   serviceURLLabel(candidate.Kind),
				URL:     url,
				Address: candidate.Address,
				Kind:    candidate.Kind,
			})
		}
	}
	return urls
}

func candidateInterfacesForBind(bind string, interfaces []SessionServiceInterface) []SessionServiceInterface {
	bind = normalizeBindAddress(bind)
	if isWildcardBind(bind) {
		return interfaces
	}

	ip := net.ParseIP(bind)
	if ip == nil || ip.IsLoopback() {
		return nil
	}

	kind := classifyServiceAddress("", ip)
	for _, iface := range interfaces {
		if iface.Address == ip.String() {
			kind = iface.Kind
			break
		}
	}
	if kind == "" {
		return nil
	}
	return []SessionServiceInterface{{Address: ip.String(), Kind: kind}}
}

func isWildcardBind(bind string) bool {
	return bind == "" || bind == "*" || bind == "0.0.0.0" || bind == "::"
}

func serviceURLLabel(kind string) string {
	switch kind {
	case "tailscale":
		return "Tailscale"
	case "lan":
		return "LAN"
	default:
		return "Host"
	}
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
