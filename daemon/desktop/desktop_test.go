package desktop

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestViewingCannotInjectAnyInput(t *testing.T) {
	for _, kind := range []string{"pointer", "button", "key", "scroll", "release"} {
		if (Command{Type: kind}).ValidateInput(false) == nil {
			t.Fatalf("view-only accepted %s", kind)
		}
	}
}

func TestInputRejectsInvalidCoordinatesKeysAndScroll(t *testing.T) {
	invalid := []Command{
		{Type: "pointer", X: math.NaN()}, {Type: "pointer", Y: math.Inf(1)},
		{Type: "pointer", X: -0.01}, {Type: "pointer", Y: 1.01},
		{Type: "button", Code: 4}, {Type: "key", Code: 0},
		{Type: "scroll", Delta: 11}, {Type: "start"}, {Type: "batch"},
	}
	for _, input := range invalid {
		if input.ValidateInput(true) == nil {
			t.Fatalf("accepted %+v", input)
		}
	}
	valid := []Command{{Type: "pointer", X: 1, Y: 0}, {Type: "button", Code: 1}, {Type: "key", Code: 0xffe1}, {Type: "scroll", Delta: -10}, {Type: "release"}}
	for _, input := range valid {
		if err := input.ValidateInput(true); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHelperPacketsAreLengthBoundedAndTyped(t *testing.T) {
	for _, size := range []uint32{0, 1, MaxFrame + 1, math.MaxUint32} {
		var raw bytes.Buffer
		_ = binary.Write(&raw, binary.BigEndian, size)
		if _, _, err := ReadPacket(&raw); err == nil {
			t.Fatalf("accepted size %d", size)
		}
	}
	for _, raw := range [][]byte{{0, 0, 0, 2, 3, 0}, {0, 0, 0, 4, 2, 1}} {
		if _, _, err := ReadPacket(bytes.NewReader(raw)); err == nil {
			t.Fatal("accepted malformed packet")
		}
	}
	kind, data, err := ReadPacket(bytes.NewReader([]byte{0, 0, 0, 3, 2, 7, 8}))
	if err != nil || kind != 2 || !bytes.Equal(data, []byte{7, 8}) {
		t.Fatalf("packet %d %v %v", kind, data, err)
	}
}

func FuzzReadPacket(f *testing.F) {
	f.Add([]byte{0, 0, 0, 2, 2, 5})
	f.Fuzz(func(t *testing.T, raw []byte) { _, _, _ = ReadPacket(bytes.NewReader(raw)) })
}
