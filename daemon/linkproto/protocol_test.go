package linkproto

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func TestProtocolRejectsUnknownVersionAndOversizedFrame(t *testing.T) {
	var unknown bytes.Buffer
	if err := WriteMessage(&unknown, Message{Type: TypePing}); err != nil {
		t.Fatal(err)
	}
	raw := unknown.Bytes()
	// JSON contains the protocol version as its first field.
	raw = bytes.Replace(raw, []byte(`"version":2`), []byte(`"version":9`), 1)
	binary.BigEndian.PutUint32(raw[:4], uint32(len(raw)-4))
	if _, err := ReadMessage(bytes.NewReader(raw)); err == nil ||
		!strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unknown version error=%v", err)
	}

	var size [4]byte
	binary.BigEndian.PutUint32(size[:], MaxFrameBytes+1)
	if _, err := ReadMessage(bytes.NewReader(size[:])); err == nil ||
		!strings.Contains(err.Error(), "frame size") {
		t.Fatalf("oversized frame error=%v", err)
	}
}
