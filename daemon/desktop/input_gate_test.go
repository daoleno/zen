package desktop

import (
	"bytes"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

type gatedWriter struct {
	bytes.Buffer
	entered chan struct{}
	resume  chan struct{}
	closed  bool
}

func (w *gatedWriter) Write(p []byte) (int, error) {
	if w.entered != nil {
		close(w.entered)
		<-w.resume
		w.entered = nil
	}
	return w.Buffer.Write(p)
}

func (w *gatedWriter) Close() error { w.closed = true; return nil }

func TestInputGateRevocationOrdersInFlightBatch(t *testing.T) {
	var gate inputGate
	writer := &gatedWriter{entered: make(chan struct{}), resume: make(chan struct{})}
	trusted := func() bool { return true }
	if err := gate.start(writer, func() error { return nil }, trusted); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		if err := gate.write([]byte("key down\n"), trusted); err != nil {
			finished <- err
			return
		}
		finished <- gate.write([]byte("key up\n"), trusted)
	}()
	<-writer.entered
	revoked := make(chan struct{})
	go func() { gate.revoke(); close(revoked) }()
	deadline := time.Now().Add(time.Second)
	for !gate.retired.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !gate.retired.Load() {
		close(writer.resume)
		t.Fatal("revocation did not publish retirement")
	}
	select {
	case <-revoked:
		t.Fatal("revocation returned before admitted write completed")
	default:
	}
	close(writer.resume)
	if err := <-finished; err != io.ErrClosedPipe {
		t.Fatalf("remaining batch accepted after retirement: %v", err)
	}
	<-revoked
	if !writer.closed || writer.String() != "key down\n" {
		t.Fatalf("revocation boundary: closed=%v bytes=%q", writer.closed, writer.String())
	}
	if err := gate.write([]byte("new input\n"), trusted); err != io.ErrClosedPipe {
		t.Fatal("post-revocation input accepted", err)
	}
}

func TestInputGateChecksTrustForEveryRecord(t *testing.T) {
	var gate inputGate
	var trust atomic.Bool
	trust.Store(true)
	writer := &gatedWriter{}
	if err := gate.start(writer, func() error { return nil }, trust.Load); err != nil {
		t.Fatal(err)
	}
	defer gate.revoke()
	if err := gate.write([]byte("before"), trust.Load); err != nil {
		t.Fatal(err)
	}
	trust.Store(false)
	if err := gate.write([]byte("after"), trust.Load); err != io.ErrClosedPipe || writer.String() != "before" {
		t.Fatal("trust loss did not stop batch", err, writer.String())
	}
}

func TestInputGateCannotStartAfterRevocation(t *testing.T) {
	var gate inputGate
	gate.revoke()
	writer := &gatedWriter{}
	started := false
	err := gate.start(writer, func() error { started = true; return nil }, func() bool { return true })
	if err != io.ErrClosedPipe || started || !writer.closed {
		t.Fatal("revoked owner started helper", err, started, writer.closed)
	}
	gate.revoke()
}

func TestInputGateRetirementDuringTrustCheck(t *testing.T) {
	for _, operation := range []string{"start", "write"} {
		t.Run(operation, func(t *testing.T) {
			var gate inputGate
			writer := &gatedWriter{}
			defer gate.revoke()
			if operation == "write" {
				if err := gate.start(writer, func() error { return nil }, func() bool { return true }); err != nil {
					t.Fatal(err)
				}
			}
			trust := func() bool {
				// Model retirement racing a delayed trust lookup.
				gate.retired.Store(true)
				return true
			}
			var err error
			if operation == "start" {
				err = gate.start(writer, func() error { t.Fatal("retired start admitted"); return nil }, trust)
			} else {
				err = gate.write([]byte("new input"), trust)
			}
			if err != io.ErrClosedPipe || writer.Len() != 0 {
				t.Fatal("retirement during trust lookup was ignored", err)
			}
		})
	}
}
