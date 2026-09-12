// Package attachment owns the local upload store shared by authenticated UI
// uploads and owner-verified channel input. Files are data, never executable.
package attachment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type File struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size"`
	Kind        string `json:"kind,omitempty"`
	Description string `json:"description,omitempty"`
}

type Limits struct {
	FileBytes, StoreBytes int64
	Retention             time.Duration
}

func DefaultLimits() Limits {
	return Limits{FileBytes: 2 << 30, StoreBytes: 8 << 30, Retention: 7 * 24 * time.Hour}
}

var (
	ErrTooLarge = errors.New("upload exceeds file limit")
	ErrCapacity = errors.New("upload exceeds remaining storage capacity; remove old uploads or wait for retention cleanup")
	ErrLength   = errors.New("upload body does not match Content-Length")
)

type Store struct {
	Dir      string
	mu       sync.Mutex
	active   int
	reserved int64
}

func (s *Store) Usage() (int, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active, s.reserved
}

// Save streams with an up-front reservation. Unknown lengths reserve the
// remaining file budget; body IO does not hold the store lock.
func (s *Store) Save(ctx context.Context, body io.Reader, file File, limits Limits) (File, error) {
	if s == nil || s.Dir == "" {
		return File{}, errors.New("upload storage unavailable")
	}
	if file.Size > limits.FileBytes {
		return File{}, ErrTooLarge
	}
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	s.mu.Lock()
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		s.mu.Unlock()
		return File{}, err
	}
	info, err := os.Lstat(s.Dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		s.mu.Unlock()
		return File{}, errors.New("upload directory is not a confined directory")
	}
	stored, err := Inspect(s.Dir, time.Now(), limits, s.active == 0)
	if err != nil {
		s.mu.Unlock()
		return File{}, err
	}
	remaining := limits.StoreBytes - stored - s.reserved
	if remaining <= 0 || file.Size > remaining {
		s.mu.Unlock()
		return File{}, ErrCapacity
	}
	reservation := min(limits.FileBytes, remaining)
	if file.Size >= 0 {
		reservation = file.Size
	}
	copyLimit := min(limits.FileBytes, reservation)
	dst, err := os.CreateTemp(s.Dir, ".upload-*.partial")
	if err != nil {
		s.mu.Unlock()
		return File{}, err
	}
	s.active++
	s.reserved += reservation
	s.mu.Unlock()
	reservationReleased := false
	defer func() {
		_ = dst.Close()
		_ = os.Remove(dst.Name())
		if !reservationReleased {
			s.mu.Lock()
			s.active--
			s.reserved -= reservation
			s.mu.Unlock()
		}
	}()
	written, err := io.Copy(dst, io.LimitReader(body, copyLimit+1))
	if err != nil {
		return File{}, err
	}
	if written > copyLimit {
		if remaining < limits.FileBytes {
			return File{}, ErrCapacity
		}
		return File{}, ErrTooLarge
	}
	if file.Size >= 0 && written != file.Size {
		return File{}, ErrLength
	}
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	if err := dst.Sync(); err != nil {
		return File{}, err
	}
	if err := dst.Close(); err != nil {
		return File{}, err
	}
	file.Path = filepath.Join(s.Dir, uuid.NewString()+SafeExtension(file.Name))
	file.Size = written
	s.mu.Lock()
	err = os.Rename(dst.Name(), file.Path)
	// A committed file and its reservation must never both count against
	// capacity observed by the next uploader.
	s.active--
	s.reserved -= reservation
	reservationReleased = true
	s.mu.Unlock()
	if err != nil {
		return File{}, err
	}
	return file, nil
}

// Remove confines removal to this store. Remote names never address a local
// path. Used for unadmitted partial batches, not conversation history.
func (s *Store) Remove(file File) error {
	if s == nil || filepath.Dir(file.Path) != filepath.Clean(s.Dir) {
		return errors.New("attachment outside upload storage")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(file.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) Exists(file File) bool {
	if s == nil || filepath.Dir(file.Path) != filepath.Clean(s.Dir) {
		return false
	}
	info, err := os.Lstat(file.Path)
	return err == nil && info.Mode().IsRegular() && info.Size() == file.Size
}

func Inspect(dir string, now time.Time, limits Limits, removePartials bool) (int64, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var total int64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return 0, fmt.Errorf("inspect upload: %w", err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if strings.HasPrefix(entry.Name(), ".upload-") && strings.HasSuffix(entry.Name(), ".partial") {
			if removePartials {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					return 0, err
				}
			}
			continue
		}
		if now.Sub(info.ModTime()) >= limits.Retention {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return 0, err
			}
			continue
		}
		if info.Size() > 0 && total > limits.StoreBytes-info.Size() {
			return limits.StoreBytes, nil
		}
		total += max(info.Size(), 0)
	}
	return total, nil
}

func SafeExtension(name string) string {
	ext := filepath.Ext(filepath.Base(strings.TrimSpace(name)))
	if len(ext) < 2 || len(ext) > 11 {
		return ""
	}
	for _, char := range ext[1:] {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
			return ""
		}
	}
	return strings.ToLower(ext)
}
