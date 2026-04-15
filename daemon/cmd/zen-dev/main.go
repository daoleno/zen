package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	restartDebounce = 250 * time.Millisecond
	stopTimeout     = 4 * time.Second
)

type runningProcess struct {
	cmd  *exec.Cmd
	done chan error
}

type watchTree struct {
	root    string
	watcher *fsnotify.Watcher
	watched map[string]struct{}
}

type devRunner struct {
	root       string
	binary     string
	daemonArgs []string
	stdout     io.Writer
	stderr     io.Writer
	child      *runningProcess
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "zen-dev: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}

	tmpDir := filepath.Join(root, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("create tmp dir: %w", err)
	}

	runner := &devRunner{
		root:       root,
		binary:     filepath.Join(tmpDir, "zen-daemon-dev"),
		daemonArgs: args,
		stdout:     stdout,
		stderr:     stderr,
	}

	if err := runner.rebuild(); err != nil {
		return err
	}
	if err := runner.start(); err != nil {
		return err
	}
	defer func() {
		_ = runner.stop(syscall.SIGINT)
	}()

	tree, err := newWatchTree(root)
	if err != nil {
		return err
	}
	defer tree.Close()

	fmt.Fprintf(stderr, "zen-dev watching %s\n", root)
	if len(args) > 0 {
		fmt.Fprintf(stderr, "zen-dev args: %s\n", strings.Join(args, " "))
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	pending := map[string]struct{}{}
	var debounce *time.Timer
	var debounceC <-chan time.Time

	for {
		select {
		case <-signals:
			fmt.Fprintln(stderr, "\nzen-dev shutting down...")
			return nil
		case event, ok := <-tree.watcher.Events:
			if !ok {
				return fmt.Errorf("watcher closed")
			}
			if event.Op == 0 {
				continue
			}

			if err := tree.handleEvent(event); err != nil {
				fmt.Fprintf(stderr, "zen-dev watch error: %v\n", err)
			}

			rel, ok := tree.relevantPath(event.Name)
			if !ok {
				continue
			}

			pending[rel] = struct{}{}
			debounce = resetTimer(debounce)
			debounceC = debounce.C
		case err, ok := <-tree.watcher.Errors:
			if !ok {
				return fmt.Errorf("watcher error channel closed")
			}
			fmt.Fprintf(stderr, "zen-dev watch error: %v\n", err)
		case <-debounceC:
			debounceC = nil

			changedFiles := mapKeys(pending)
			pending = map[string]struct{}{}

			fmt.Fprintf(stderr, "\nzen-dev detected changes: %s\n", strings.Join(changedFiles, ", "))
			if err := runner.rebuild(); err != nil {
				fmt.Fprintf(stderr, "zen-dev build failed:\n%v\n", err)
				continue
			}
			if err := runner.restart(); err != nil {
				return err
			}
		}
	}
}

func newWatchTree(root string) (*watchTree, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create watcher: %w", err)
	}

	tree := &watchTree{
		root:    root,
		watcher: watcher,
		watched: make(map[string]struct{}),
	}
	if err := tree.addTree(root); err != nil {
		watcher.Close()
		return nil, err
	}
	return tree, nil
}

func (t *watchTree) Close() error {
	return t.watcher.Close()
}

func (t *watchTree) addTree(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if ignoredDirName(d.Name()) {
			return filepath.SkipDir
		}

		clean := filepath.Clean(path)
		if _, ok := t.watched[clean]; ok {
			return nil
		}
		if err := t.watcher.Add(clean); err != nil {
			return fmt.Errorf("watch %s: %w", clean, err)
		}
		t.watched[clean] = struct{}{}
		return nil
	})
}

func (t *watchTree) handleEvent(event fsnotify.Event) error {
	path := filepath.Clean(event.Name)

	if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
		t.removeTree(path)
	}
	if event.Op&fsnotify.Create == 0 {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.IsDir() || ignoredDirName(info.Name()) {
		return nil
	}
	return t.addTree(path)
}

func (t *watchTree) removeTree(root string) {
	clean := filepath.Clean(root)
	prefix := clean + string(os.PathSeparator)
	toRemove := make([]string, 0)

	for watched := range t.watched {
		if watched == clean || strings.HasPrefix(watched, prefix) {
			toRemove = append(toRemove, watched)
		}
	}

	for _, watched := range toRemove {
		delete(t.watched, watched)
		_ = t.watcher.Remove(watched)
	}
}

func (t *watchTree) relevantPath(path string) (string, bool) {
	rel, err := filepath.Rel(t.root, path)
	if err != nil {
		return "", false
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}

	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if ignoredDirName(part) {
			return "", false
		}
	}

	base := filepath.Base(rel)
	if base != "go.mod" && base != "go.sum" && filepath.Ext(base) != ".go" {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func (r *devRunner) rebuild() error {
	cmd := exec.Command("go", "build", "-o", r.binary, "./cmd/zen-daemon")
	cmd.Dir = r.root
	output, err := cmd.CombinedOutput()
	if len(output) > 0 {
		_, _ = r.stderr.Write(output)
	}
	if err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}
	return nil
}

func (r *devRunner) restart() error {
	if err := r.stop(syscall.SIGINT); err != nil {
		return err
	}
	return r.start()
}

func (r *devRunner) start() error {
	cmd := exec.Command(r.binary, r.daemonArgs...)
	cmd.Dir = r.root
	cmd.Stdout = r.stdout
	cmd.Stderr = r.stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	r.child = &runningProcess{
		cmd:  cmd,
		done: make(chan error, 1),
	}

	go func(child *runningProcess) {
		child.done <- child.cmd.Wait()
	}(r.child)

	return nil
}

func (r *devRunner) stop(sig os.Signal) error {
	if r.child == nil {
		return nil
	}

	child := r.child
	r.child = nil

	select {
	case err := <-child.done:
		if err != nil && !isExpectedExit(err) {
			return fmt.Errorf("daemon exited unexpectedly: %w", err)
		}
		return nil
	default:
	}

	if err := child.cmd.Process.Signal(sig); err != nil && !isDoneProcess(err) {
		return fmt.Errorf("signal daemon: %w", err)
	}

	select {
	case err := <-child.done:
		if err != nil && !isExpectedExit(err) {
			return fmt.Errorf("wait for daemon stop: %w", err)
		}
		return nil
	case <-time.After(stopTimeout):
		if err := child.cmd.Process.Kill(); err != nil && !isDoneProcess(err) {
			return fmt.Errorf("kill daemon: %w", err)
		}
		err := <-child.done
		if err != nil && !isExpectedExit(err) {
			return fmt.Errorf("wait for daemon kill: %w", err)
		}
		return nil
	}
}

func resetTimer(timer *time.Timer) *time.Timer {
	if timer == nil {
		return time.NewTimer(restartDebounce)
	}

	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(restartDebounce)
	return timer
}

func ignoredDirName(name string) bool {
	switch name {
	case ".git", "bin", "tmp":
		return true
	default:
		return false
	}
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func isExpectedExit(err error) bool {
	if err == nil {
		return true
	}
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

func isDoneProcess(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "process already finished")
}
