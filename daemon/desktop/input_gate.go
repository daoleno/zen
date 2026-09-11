package desktop

import (
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// inputGate serializes admission with revocation. Previously written pipe bytes
// may still be consumed, but revoke returns with no writer or future admission.
type inputGate struct {
	mu      sync.Mutex
	retired atomic.Bool
	writer  io.WriteCloser
}

func (g *inputGate) start(writer io.WriteCloser, start func() error, trusted func() bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.retired.Load() || !trusted() || g.retired.Load() {
		_ = writer.Close()
		return io.ErrClosedPipe
	}
	if err := start(); err != nil {
		_ = writer.Close()
		return err
	}
	g.writer = writer
	return nil
}

func (g *inputGate) write(data []byte, trusted func() bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.retired.Load() || g.writer == nil || !trusted() || g.retired.Load() {
		return io.ErrClosedPipe
	}
	if pipe, ok := g.writer.(interface{ SetWriteDeadline(time.Time) error }); ok {
		if err := pipe.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
			return err
		}
	}
	n, err := g.writer.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}

func (g *inputGate) revoke() {
	// Publish before waiting: queued batch records cannot overtake the revoker.
	g.retired.Store(true)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.writer != nil {
		_ = g.writer.Close()
		g.writer = nil
	}
}
