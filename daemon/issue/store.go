package issue

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type EventType string

const (
	EventChanged EventType = "changed"
	EventDeleted EventType = "deleted"
)

// ErrConflict is returned by Write when the base mtime does not match the file on disk.
var ErrConflict = errors.New("issue write conflict: file changed on disk")

// Event describes an issue change emitted by the store.
type Event struct {
	Type  EventType
	ID    string
	Path  string
	Issue *Issue
}

// Store is an in-memory index of Markdown issue files under Root.
type Store struct {
	Root string

	mu      sync.RWMutex
	byPath  map[string]*Issue
	byID    map[string]*Issue
	subs    map[int]chan Event
	nextSub int

	watcherMu sync.Mutex
	watcher   *fsnotify.Watcher
	stopCh    chan struct{}
	stopOnce  sync.Once
}

// NewStore creates the store and performs an initial scan.
func NewStore(root string) (*Store, error) {
	if err := EnsureDir(root); err != nil {
		return nil, err
	}
	store := &Store{
		Root:   root,
		byPath: map[string]*Issue{},
		byID:   map[string]*Issue{},
		subs:   map[int]chan Event{},
		stopCh: make(chan struct{}),
	}
	if err := store.scanAll(); err != nil {
		return nil, err
	}
	return store, nil
}

// Close stops the watcher and closes subscriptions.
func (s *Store) Close() error {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		s.watcherMu.Lock()
		if s.watcher != nil {
			_ = s.watcher.Close()
			s.watcher = nil
		}
		s.watcherMu.Unlock()

		s.mu.Lock()
		for id, ch := range s.subs {
			close(ch)
			delete(s.subs, id)
		}
		s.mu.Unlock()
	})
	return nil
}

// Subscribe returns a channel that receives issue change events.
func (s *Store) Subscribe() (int, <-chan Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextSub
	s.nextSub++
	ch := make(chan Event, 64)
	s.subs[id] = ch
	return id, ch
}

// Unsubscribe removes a subscriber by id.
func (s *Store) Unsubscribe(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.subs[id]; ok {
		close(ch)
		delete(s.subs, id)
	}
}

func (s *Store) broadcast(ev Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// List returns a stable snapshot of all issues sorted by creation time descending.
func (s *Store) List() []*Issue {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*Issue, 0, len(s.byPath))
	for _, iss := range s.byPath {
		out = append(out, cloneIssue(iss))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Frontmatter.Created.After(out[j].Frontmatter.Created)
	})
	return out
}

// GetByID returns one issue by frontmatter ID.
func (s *Store) GetByID(id string) (*Issue, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	iss, ok := s.byID[id]
	return cloneIssue(iss), ok
}

// Write persists the issue to disk atomically. If baseMtime is non-zero and
// the current file mtime does not match, ErrConflict is returned.
func (s *Store) Write(iss *Issue, baseMtime time.Time) (*Issue, error) {
	if iss == nil {
		return nil, fmt.Errorf("issue required")
	}
	if strings.TrimSpace(iss.Path) == "" {
		return nil, fmt.Errorf("issue path required")
	}

	if !baseMtime.IsZero() {
		st, err := os.Stat(iss.Path)
		if err == nil {
			if !sameMtime(st.ModTime(), baseMtime) {
				return nil, ErrConflict
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}

	data, err := SerializeIssue(iss)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(iss.Path), 0o700); err != nil {
		return nil, err
	}
	if err := writeAtomic(iss.Path, data, 0o600); err != nil {
		return nil, err
	}

	st, err := os.Stat(iss.Path)
	if err != nil {
		return nil, err
	}
	parsed, err := ParseFile(iss.Path, data, st.ModTime())
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.upsertLocked(parsed)
	s.mu.Unlock()

	out := cloneIssue(parsed)
	s.broadcast(Event{Type: EventChanged, ID: out.ID, Path: out.Path, Issue: out})
	return out, nil
}

// Delete removes the file and evicts the issue from the in-memory snapshot.
func (s *Store) Delete(id string) error {
	s.mu.RLock()
	iss, ok := s.byID[id]
	s.mu.RUnlock()
	if !ok {
		return os.ErrNotExist
	}

	if err := os.Remove(iss.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	s.mu.Lock()
	delete(s.byID, id)
	delete(s.byPath, iss.Path)
	s.mu.Unlock()

	s.broadcast(Event{Type: EventDeleted, ID: id, Path: iss.Path})
	return nil
}

// StartWatcher begins recursive fsnotify watching of Root. Events are debounced
// per-path with a 200ms window.
func (s *Store) StartWatcher() error {
	s.watcherMu.Lock()
	defer s.watcherMu.Unlock()
	if s.watcher != nil {
		return nil
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := addRecursive(watcher, s.Root); err != nil {
		_ = watcher.Close()
		return err
	}
	s.watcher = watcher

	pending := map[string]*time.Timer{}
	var pendingMu sync.Mutex

	flush := func(path string) {
		pendingMu.Lock()
		delete(pending, path)
		pendingMu.Unlock()

		st, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			s.mu.Lock()
			removed, ok := s.byPath[path]
			if ok {
				delete(s.byPath, path)
				delete(s.byID, removed.ID)
			}
			s.mu.Unlock()
			if ok {
				s.broadcast(Event{Type: EventDeleted, ID: removed.ID, Path: path})
			}
			return
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "issue: stat %s: %v\n", path, err)
			return
		}
		if st.IsDir() || !strings.HasSuffix(path, ".md") {
			return
		}
		if err := s.reloadPath(path); err != nil {
			fmt.Fprintf(os.Stderr, "issue: reload %s: %v\n", path, err)
			return
		}
		if iss, ok := s.GetByIDFromPath(path); ok {
			s.broadcast(Event{Type: EventChanged, ID: iss.ID, Path: path, Issue: iss})
		}
	}

	go func() {
		for {
			select {
			case <-s.stopCh:
				return
			case ev, ok := <-watcher.Events:
				if !ok {
					return
				}
				if ev.Op&fsnotify.Create != 0 {
					if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
						_ = addRecursive(watcher, ev.Name)
					}
				}
				pendingMu.Lock()
				if timer, ok := pending[ev.Name]; ok {
					timer.Stop()
				}
				path := ev.Name
				pending[path] = time.AfterFunc(200*time.Millisecond, func() {
					flush(path)
				})
				pendingMu.Unlock()
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Fprintf(os.Stderr, "issue: watch error: %v\n", err)
			}
		}
	}()

	return nil
}

func (s *Store) GetByIDFromPath(path string) (*Issue, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	iss, ok := s.byPath[path]
	return cloneIssue(iss), ok
}

func (s *Store) scanAll() error {
	return filepath.Walk(s.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		if err := s.reloadPath(path); err != nil {
			fmt.Fprintf(os.Stderr, "issue: skip %s: %v\n", path, err)
		}
		return nil
	})
}

func (s *Store) reloadPath(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	iss, err := ParseFile(path, data, st.ModTime())
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.upsertLocked(iss)
	s.mu.Unlock()
	return nil
}

func (s *Store) upsertLocked(iss *Issue) {
	if old, ok := s.byPath[iss.Path]; ok && old.ID != iss.ID {
		delete(s.byID, old.ID)
	}
	if old, ok := s.byID[iss.ID]; ok && old.Path != iss.Path {
		delete(s.byPath, old.Path)
	}
	s.byPath[iss.Path] = cloneIssue(iss)
	s.byID[iss.ID] = cloneIssue(iss)
}

func sameMtime(left, right time.Time) bool {
	return left.UTC().Truncate(time.Millisecond).Equal(right.UTC().Truncate(time.Millisecond))
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".zen-issue-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

func addRecursive(watcher *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return watcher.Add(path)
		}
		return nil
	})
}
