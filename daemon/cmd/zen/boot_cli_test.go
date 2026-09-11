package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeBootRunner struct {
	calls     [][]string
	responses map[string]string
	failures  map[string]bool
}

func (f *fakeBootRunner) run(name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	key := strings.Join(call, " ")
	if f.failures[key] {
		return []byte(f.responses[key]), errors.New("fake failure")
	}
	return []byte(f.responses[key]), nil
}

func (f *fakeBootRunner) called(key string) bool {
	for _, call := range f.calls {
		if strings.Join(call, " ") == key {
			return true
		}
	}
	return false
}

func newBootTestEnvironment(t *testing.T) (bootConfig, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("HOME", home)
	binary := filepath.Join(home, "zen")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bootConfig{
		Binary:   binary,
		StateDir: filepath.Join(home, ".zen"),
		Addr:     "127.0.0.1:9876",
	}, home
}

func bootTestRunner() *fakeBootRunner {
	return &fakeBootRunner{
		responses: map[string]string{},
		failures:  map[string]bool{},
	}
}

func TestRenderBootUnit(t *testing.T) {
	config, home := newBootTestEnvironment(t)
	config.LAN = true
	unit, err := renderBootUnit(config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, bootManagedMarker) {
		t.Fatal("managed marker missing")
	}
	if !strings.Contains(unit, "ExecStart="+config.Binary+" -state-dir "+config.StateDir+" -addr "+config.Addr+" -lan\n") {
		t.Fatalf("unexpected ExecStart:\n%s", unit)
	}
	if !strings.Contains(unit, "Environment=HOME="+home+"\n") || !strings.Contains(unit, "WantedBy=default.target\n") {
		t.Fatalf("missing environment or target:\n%s", unit)
	}
	if !strings.Contains(unit, "Restart=on-failure\n") {
		t.Fatal("restart policy missing")
	}
	// No tmux or second-owner supervision may appear in the unit.
	if strings.Contains(unit, "tmux") {
		t.Fatal("boot unit must not manage tmux")
	}
}

func TestRenderBootUnitQuotesAndEscapes(t *testing.T) {
	config := bootConfig{Binary: "/opt/zen dir/zen", StateDir: "/tmp/state%name", Addr: "127.0.0.1:9876"}
	unit, err := renderBootUnit(config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, `ExecStart="/opt/zen dir/zen" -state-dir /tmp/state%%name -addr 127.0.0.1:9876`) {
		t.Fatalf("unsafe ExecStart quoting:\n%s", unit)
	}
}

func TestBootInstallWritesManagedUnitAndEnables(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	runner := bootTestRunner()
	runner.responses["loginctl show-user zen-test -p Linger --value"] = "yes"
	runner.responses["loginctl show-user "+currentUserName()+" -p Linger --value"] = "yes"
	var out bytes.Buffer
	if err := bootInstall(config, runner, &out); err != nil {
		t.Fatal(err)
	}
	path, err := bootUnitPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), bootManagedMarker) || !strings.Contains(string(data), config.Binary) {
		t.Fatalf("managed unit not written:\n%s", data)
	}
	if !runner.called("systemctl --user daemon-reload") {
		t.Fatal("daemon-reload not called")
	}
	if !runner.called("systemctl --user enable --now zen.service") {
		t.Fatal("enable --now not called")
	}
	if !strings.Contains(out.String(), "Installed and started") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestBootInstallRefusesForeignUnit(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	path, err := bootUnitPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[Unit]\nDescription=user unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := bootTestRunner()
	if err := bootInstall(config, runner, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "not managed") {
		t.Fatalf("foreign unit not refused: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Description=user unit") {
		t.Fatal("foreign unit was modified")
	}
}

func TestBootInstallLingerFailureReportsExactOperatorCommand(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	runner := bootTestRunner()
	name := currentUserName()
	runner.responses["loginctl show-user "+name+" -p Linger --value"] = "no"
	runner.responses["loginctl enable-linger "+name] = "Interactive authentication required."
	runner.failures["loginctl enable-linger "+name] = true
	var out bytes.Buffer
	err := bootInstall(config, runner, &out)
	if err == nil || !strings.Contains(err.Error(), "sudo loginctl enable-linger "+name) {
		t.Fatalf("linger failure did not report the exact operator command: %v", err)
	}
	if !strings.Contains(out.String(), "sudo loginctl enable-linger "+name) {
		t.Fatalf("operator command missing from output: %s", out.String())
	}
}

func TestBootUninstallRemovesOnlyManagedUnit(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	runner := bootTestRunner()
	runner.responses["loginctl show-user "+currentUserName()+" -p Linger --value"] = "yes"
	runner.responses["loginctl show-user zen-test -p Linger --value"] = "yes"
	if err := bootInstall(config, runner, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	path, _ := bootUnitPath()
	if err := bootUninstall(runner, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("managed unit was not removed")
	}
	if !runner.called("systemctl --user disable --now zen.service") {
		t.Fatal("disable --now not called")
	}

	// A foreign unit must never be removed.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[Unit]\nDescription=user unit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := bootUninstall(runner, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "not managed") {
		t.Fatalf("foreign unit not refused on uninstall: %v", err)
	}
}

func TestBootDryRunWritesNothing(t *testing.T) {
	config, _ := newBootTestEnvironment(t)
	config.DryRun = true
	var out bytes.Buffer
	if err := bootInstall(config, bootTestRunner(), &out); err != nil {
		t.Fatal(err)
	}
	path, _ := bootUnitPath()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("dry run wrote a unit")
	}
	if !strings.Contains(out.String(), bootManagedMarker) {
		t.Fatalf("dry run did not print the unit: %s", out.String())
	}
}
