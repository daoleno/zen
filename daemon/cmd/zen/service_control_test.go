package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/watcher"
)

type serviceFakeWatcher struct {
	*fakeControlWatcher
	snapshot      watcher.SessionServiceSnapshot
	snapshotErr   error
	registered    []watcher.ManagedServiceDescriptor
	registerErr   error
	unregistered  []string
	unregisterErr error
}

func (f *serviceFakeWatcher) DiscoverSessionServices() (watcher.SessionServiceSnapshot, error) {
	if f.snapshotErr != nil {
		return watcher.SessionServiceSnapshot{}, f.snapshotErr
	}
	return f.snapshot, nil
}

func (f *serviceFakeWatcher) RegisterManagedService(desc watcher.ManagedServiceDescriptor) (watcher.ManagedServiceDescriptor, error) {
	if f.registerErr != nil {
		return watcher.ManagedServiceDescriptor{}, f.registerErr
	}
	f.registered = append(f.registered, desc)
	return desc, nil
}

func (f *serviceFakeWatcher) UnregisterManagedService(unit string) error {
	if f.unregisterErr != nil {
		return f.unregisterErr
	}
	f.unregistered = append(f.unregistered, unit)
	return nil
}

func TestServiceListReturnsAuthoritativeSnapshot(t *testing.T) {
	fw := &serviceFakeWatcher{fakeControlWatcher: newFakeControlWatcher()}
	fw.snapshot = watcher.SessionServiceSnapshot{
		Services: []watcher.SessionService{
			{ID: "main:@1:10:3000", WorkerID: "main:@1", WorkerName: "Main", Port: 3000, Source: watcher.ServiceSourceSession},
			{ID: "persistent:dsh-web.service:11:3080", WorkerName: "DeepSeek Harness", Port: 3080, Source: watcher.ServiceSourcePersistent, Unit: "dsh-web.service", State: watcher.ServiceStateActive, LocalOnly: true},
		},
	}
	app := &controlApp{watcher: fw}
	resp := app.HandleControlRequest(control.Request{Type: "service_list"})
	if !resp.OK || resp.ServiceSnapshot == nil {
		t.Fatalf("service_list response = %+v", resp)
	}
	if len(resp.ServiceSnapshot.Services) != 2 {
		t.Fatalf("service_list services = %#v, want tmux + persistent rows", resp.ServiceSnapshot.Services)
	}
	persistent := resp.ServiceSnapshot.Services[1]
	if persistent.WorkerID != "" {
		t.Fatalf("persistent row WorkerID = %q, want empty", persistent.WorkerID)
	}
}

func TestServiceListSurfacesDiscoveryError(t *testing.T) {
	fw := &serviceFakeWatcher{fakeControlWatcher: newFakeControlWatcher(), snapshotErr: errors.New("ss failed")}
	app := &controlApp{watcher: fw}
	resp := app.HandleControlRequest(control.Request{Type: "service_list"})
	if resp.OK || resp.Error == nil {
		t.Fatalf("service_list error response = %+v, want failure", resp)
	}
}

func TestServiceRegisterAttributesCallerWorker(t *testing.T) {
	fw := &serviceFakeWatcher{fakeControlWatcher: newFakeControlWatcher()}
	app := &controlApp{watcher: fw}
	resp := app.HandleControlRequest(control.Request{
		Type:     "service_register",
		Service:  &watcher.ManagedServiceDescriptor{Unit: "dsh-web.service", Name: "DeepSeek Harness", Port: 3080},
		WorkerID: "main:@202",
		WorkID:   "dde7dc7f",
	})
	if !resp.OK || resp.Service == nil {
		t.Fatalf("service_register response = %+v", resp)
	}
	if len(fw.registered) != 1 {
		t.Fatalf("registered = %#v, want one descriptor", fw.registered)
	}
	got := fw.registered[0]
	if got.Unit != "dsh-web.service" || got.RegisteredBy != "main:@202" || got.WorkID != "dde7dc7f" {
		t.Fatalf("registered descriptor = %+v, want unit + caller provenance", got)
	}
}

func TestServiceRegisterRequiresUnit(t *testing.T) {
	fw := &serviceFakeWatcher{fakeControlWatcher: newFakeControlWatcher()}
	app := &controlApp{watcher: fw}
	resp := app.HandleControlRequest(control.Request{Type: "service_register", Service: &watcher.ManagedServiceDescriptor{Name: "Nameless"}})
	if resp.OK {
		t.Fatalf("service_register without unit = %+v, want failure", resp)
	}
}

func TestServiceUnregisterRemovesRegistration(t *testing.T) {
	fw := &serviceFakeWatcher{fakeControlWatcher: newFakeControlWatcher()}
	app := &controlApp{watcher: fw}
	resp := app.HandleControlRequest(control.Request{Type: "service_unregister", ServiceUnit: "dsh-web.service"})
	if !resp.OK {
		t.Fatalf("service_unregister response = %+v", resp)
	}
	if len(fw.unregistered) != 1 || fw.unregistered[0] != "dsh-web.service" {
		t.Fatalf("unregistered = %#v, want dsh-web.service", fw.unregistered)
	}
}

func TestServiceCLIUsage(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"service", "--help"}, &output); err != nil && !errors.Is(err, flag.ErrHelp) {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "zen service") {
		t.Fatalf("service help=%s", output.String())
	}
	if err := run([]string{"service", "bogus"}, &output); err == nil || !strings.Contains(err.Error(), "unknown service command") {
		t.Fatalf("unknown service subcommand err=%v", err)
	}
}

func TestServiceCLIDisplayHelpers(t *testing.T) {
	persistent := watcher.SessionService{WorkerName: "DeepSeek Harness", Unit: "dsh-web.service", Port: 3080, Source: watcher.ServiceSourcePersistent, State: watcher.ServiceStateActive}
	if got := serviceDisplayName(persistent); got != "DeepSeek Harness (dsh-web.service)" {
		t.Fatalf("serviceDisplayName = %q", got)
	}
	if got := serviceDisplayState(persistent); got != watcher.ServiceStateActive {
		t.Fatalf("serviceDisplayState = %q", got)
	}
	session := watcher.SessionService{WorkerID: "main:@1", WorkerName: "Main", Port: 3000}
	if got := serviceDisplayState(session); got != watcher.ServiceStateActive {
		t.Fatalf("session default state = %q, want active", got)
	}
	if got := serviceDisplaySource(session); got != watcher.ServiceSourceSession {
		t.Fatalf("session default source = %q", got)
	}
}
