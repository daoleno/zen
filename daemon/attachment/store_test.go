package attachment

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestSharedStoreCapacityConfinementAndEnvelope(t *testing.T) {
	s := &Store{Dir: filepath.Join(t.TempDir(), "uploads")}
	limits := DefaultLimits()
	limits.FileBytes = 8
	limits.StoreBytes = 10
	file, err := s.Save(t.Context(), bytes.NewBufferString("12345678"), File{Name: "report.TXT", Size: 8, ContentType: "text/plain"}, limits)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(file.Path) != ".txt" || file.Name != "report.TXT" || !s.Exists(file) {
		t.Fatalf("metadata=%+v", file)
	}
	if _, err := s.Save(t.Context(), bytes.NewBufferString("123"), File{Name: "x", Size: 3}, limits); !errors.Is(err, ErrCapacity) {
		t.Fatalf("shared capacity ignored: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "owned.txt")
	if err := os.WriteFile(outside, []byte("owned"), 0600); err != nil {
		t.Fatal(err)
	}
	if s.Remove(File{Path: outside}) == nil {
		t.Fatal("removed outside store")
	}
	if err := os.Remove(file.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, file.Path); err != nil {
		t.Fatal(err)
	}
	if s.Exists(file) {
		t.Fatal("symlink accepted as attachment")
	}
	unsafe := &Store{Dir: filepath.Join(t.TempDir(), "symlink")}
	if err := os.Symlink(filepath.Dir(outside), unsafe.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := unsafe.Save(t.Context(), bytes.NewBufferString("x"), File{Name: "x", Size: 1}, limits); err == nil {
		t.Fatal("symlink upload store accepted")
	}
}

type reservedUploadReader struct {
	reader  *bytes.Reader
	started chan<- struct{}
	release <-chan struct{}
	once    sync.Once
}

func (r *reservedUploadReader) Read(buffer []byte) (int, error) {
	r.once.Do(func() { r.started <- struct{}{}; <-r.release })
	return r.reader.Read(buffer)
}

func TestStoreConcurrentFinalizationNeverDoubleCountsReservation(t *testing.T) {
	const uploads = 16
	const size = 1024
	s := &Store{Dir: filepath.Join(t.TempDir(), "uploads")}
	limits := DefaultLimits()
	limits.FileBytes, limits.StoreBytes = size, uploads*size
	started := make(chan struct{}, uploads)
	release := make(chan struct{})
	results := make(chan error, uploads)
	for range uploads {
		go func() {
			reader := &reservedUploadReader{reader: bytes.NewReader(make([]byte, size)), started: started, release: release}
			_, err := s.Save(t.Context(), reader, File{Name: "fixture.bin", Size: size}, limits)
			results <- err
		}()
	}
	for range uploads {
		select {
		case <-started:
		case err := <-results:
			t.Fatalf("reservation rejected before body: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("reservation did not start")
		}
	}
	if active, reserved := s.Usage(); active != uploads || reserved != uploads*size {
		t.Fatalf("reservations=%d %d", active, reserved)
	}
	observed := make(chan error, 1)
	stop := make(chan struct{})
	go func() {
		for {
			s.mu.Lock()
			stored, err := Inspect(s.Dir, time.Now(), limits, false)
			if err == nil && stored+s.reserved > limits.StoreBytes {
				err = fmt.Errorf("committed bytes counted twice: stored=%d reserved=%d", stored, s.reserved)
			}
			s.mu.Unlock()
			if err != nil {
				observed <- err
				return
			}
			select {
			case <-stop:
				observed <- nil
				return
			default:
				runtime.Gosched()
			}
		}
	}()
	close(release)
	for range uploads {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	close(stop)
	if err := <-observed; err != nil {
		t.Fatal(err)
	}
	if active, reserved := s.Usage(); active != 0 || reserved != 0 {
		t.Fatalf("reservations leaked: %d %d", active, reserved)
	}
	stored, err := Inspect(s.Dir, time.Now(), limits, false)
	if err != nil || stored != uploads*size {
		t.Fatalf("stored=%d err=%v", stored, err)
	}
}
