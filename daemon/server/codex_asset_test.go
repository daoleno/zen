package server

import "testing"

func TestCodexAssetContentTypeDetectsImages(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	if got := codexAssetContentType("/tmp/screen.png", png); got != "image/png" {
		t.Fatalf("png content type = %q, want image/png", got)
	}

	if got := codexAssetContentType("/tmp/screen.webp", []byte("not enough bytes")); got != "image/webp" {
		t.Fatalf("webp fallback content type = %q, want image/webp", got)
	}
}
