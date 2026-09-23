package attachment

import (
	"bytes"
	"encoding/base64"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
	"unicode/utf8"
)

const ImagePreviewMaxBytes = 2 << 20

// ImagePreviewDataURL preserves original image bytes while bounding both the
// encoded response and native decoding dimensions for small workspace previews.
func ImagePreviewDataURL(data []byte) (string, error) {
	if len(data) > ImagePreviewMaxBytes {
		return "", fmt.Errorf("image exceeds the 2 MiB workspace preview limit")
	}
	mediaType := http.DetectContentType(data)
	if IsSVGPreview(data) {
		// The mobile renderer validates the complete structured SVG before drawing.
		return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(data), nil
	}
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return "", fmt.Errorf("unsupported workspace image format")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		return "", fmt.Errorf("image is invalid or exceeds the 40 megapixel preview limit")
	}
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// IsSVGPreview admits bounded UTF-8 roots. Mobile validates the full document,
// including drawing tags and local-only references, before rendering.
func IsSVGPreview(data []byte) bool {
	if len(data) > ImagePreviewMaxBytes || !utf8.Valid(data) {
		return false
	}
	return HasSVGPreviewRoot(data)
}

// HasSVGPreviewRoot inspects a content sample; callers must bound the complete
// file and leave document validation to the mobile renderer.
func HasSVGPreviewRoot(data []byte) bool {
	root := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
	if strings.HasPrefix(root, "<?xml") {
		end := strings.Index(root, "?>")
		if end < 0 || end > 256 {
			return false
		}
		root = strings.TrimSpace(root[end+2:])
	}
	return strings.HasPrefix(root, "<svg ") || strings.HasPrefix(root, "<svg>") || strings.HasPrefix(root, "<svg\n") || strings.HasPrefix(root, "<svg\t")
}
