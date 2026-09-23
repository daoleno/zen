package attachment

import (
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"os"
	"strings"
	"testing"
)

func TestImagePreviewPreservesBytesAndBoundsDimensions(t *testing.T) {
	data, err := os.ReadFile("../../app/assets/reading-fixture/normal.png")
	if err != nil {
		t.Fatal(err)
	}
	uri, err := ImagePreviewDataURL(data)
	if err != nil {
		t.Fatal(err)
	}
	if uri != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(data) {
		t.Fatal("original image bytes changed")
	}
	if _, err := ImagePreviewDataURL([]byte("not an image")); err == nil {
		t.Fatal("nonimage accepted")
	}
	if _, err := ImagePreviewDataURL(make([]byte, ImagePreviewMaxBytes+1)); err == nil {
		t.Fatal("oversized image accepted")
	}
	large := append([]byte(nil), data...)
	binary.BigEndian.PutUint32(large[16:20], 10000)
	binary.BigEndian.PutUint32(large[20:24], 10000)
	binary.BigEndian.PutUint32(large[29:33], crc32.ChecksumIEEE(large[12:29]))
	if _, err := ImagePreviewDataURL(large); err == nil || !strings.Contains(err.Error(), "40 megapixel") {
		t.Fatalf("dimension bound=%v", err)
	}
}

func TestImagePreviewSVGPreservesBytesAndRejectsFakeRoots(t *testing.T) {
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 10"><path d="M0 0"/></svg>`)
	uri, err := ImagePreviewDataURL(svg)
	if err != nil || uri != "data:image/svg+xml;base64,"+base64.StdEncoding.EncodeToString(svg) {
		t.Fatalf("SVG changed or rejected: %v", err)
	}
	long := append([]byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 10"><path d="`), make([]byte, 500)...)
	if !HasSVGPreviewRoot(long[:512]) {
		t.Fatal("valid SVG sniff root rejected")
	}
	for _, invalid := range [][]byte{[]byte("not an image <svg>"), []byte("<?xml broken"), []byte{0xff, '<', 's', 'v', 'g', '>'}} {
		if IsSVGPreview(invalid) {
			t.Fatalf("invalid SVG accepted: %q", invalid)
		}
	}
}
