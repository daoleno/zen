package main

import (
	"strings"

	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/watcher"
)

// serviceDiscoveryWatcher is the optional control-plane view of persistent +
// session service discovery. Production *watcher.Watcher implements it; the
// assertion keeps older fakes compiling while the registry rolls out.
type serviceDiscoveryWatcher interface {
	DiscoverSessionServices() (watcher.SessionServiceSnapshot, error)
}

type serviceRegistryWatcher interface {
	RegisterManagedService(watcher.ManagedServiceDescriptor) (watcher.ManagedServiceDescriptor, error)
	UnregisterManagedService(string) error
}

func (a *controlApp) handleServiceList() control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Worker watcher is not running.")
	}
	discovery, ok := a.watcher.(serviceDiscoveryWatcher)
	if !ok {
		return control.ErrorResponse("services_unavailable", "Service discovery is not configured.")
	}
	snapshot, err := discovery.DiscoverSessionServices()
	if err != nil {
		return control.ErrorResponse("service_list_failed", err.Error())
	}
	return control.Response{OK: true, ServiceSnapshot: &snapshot}
}

func (a *controlApp) handleServiceRegister(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Worker watcher is not running.")
	}
	registry, ok := a.watcher.(serviceRegistryWatcher)
	if !ok {
		return control.ErrorResponse("services_unavailable", "Service registration is not configured.")
	}
	desc := watcher.ManagedServiceDescriptor{}
	if req.Service != nil {
		desc = *req.Service
	}
	if unit := strings.TrimSpace(req.ServiceUnit); unit != "" {
		desc.Unit = unit
	}
	if strings.TrimSpace(desc.Unit) == "" {
		return control.ErrorResponse("missing_service_unit", "A user systemd unit is required (for example dsh-web.service).")
	}
	// Attribute the handoff to the calling Worker when it did not name one.
	if strings.TrimSpace(desc.RegisteredBy) == "" {
		desc.RegisteredBy = strings.TrimSpace(req.WorkerID)
	}
	if strings.TrimSpace(req.WorkID) != "" && strings.TrimSpace(desc.WorkID) == "" {
		desc.WorkID = strings.TrimSpace(req.WorkID)
	}
	registered, err := registry.RegisterManagedService(desc)
	if err != nil {
		return control.ErrorResponse("service_register_failed", err.Error())
	}
	return control.Response{
		OK:           true,
		Service:      &registered,
		Confirmation: "Registered persistent service " + registered.Unit + ".",
	}
}

func (a *controlApp) handleServiceUnregister(req control.Request) control.Response {
	if a == nil || a.watcher == nil {
		return control.ErrorResponse("watcher_unavailable", "Worker watcher is not running.")
	}
	registry, ok := a.watcher.(serviceRegistryWatcher)
	if !ok {
		return control.ErrorResponse("services_unavailable", "Service registration is not configured.")
	}
	unit := strings.TrimSpace(req.ServiceUnit)
	if req.Service != nil && unit == "" {
		unit = strings.TrimSpace(req.Service.Unit)
	}
	if unit == "" {
		return control.ErrorResponse("missing_service_unit", "A user systemd unit is required (for example dsh-web.service).")
	}
	if err := registry.UnregisterManagedService(unit); err != nil {
		return control.ErrorResponse("service_unregister_failed", err.Error())
	}
	return control.Response{OK: true, Confirmation: "Unregistered persistent service " + unit + "."}
}
