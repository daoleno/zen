package host

import "syscall"

// SunshineProcess and SunshineSpawner are platform-neutral so non-Linux builds
// can compile the capability hooks even though the supervision implementation
// is Linux-only.
type SunshineProcess interface {
	Signal(signal syscall.Signal) error
	Wait() error
	PID() int
}

type SunshineSpawner func(binary string, args []string, dir string) (SunshineProcess, error)

// SunshineRuntimeSnapshot is the capability-visible view of the supervised host.
type SunshineRuntimeSnapshot struct {
	Configured bool   `json:"configured"`
	Running    bool   `json:"running"`
	HostKey    string `json:"host_key"`
	HTTPPort   int    `json:"http_port"`
	HTTPSPort  int    `json:"https_port"`
	AppID      int    `json:"app_id"`
}
