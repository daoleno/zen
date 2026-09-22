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
)

const ImagePreviewMaxBytes = 2 << 20

// ImagePreviewDataURL preserves original raster bytes while bounding both the
// encoded response and native decoding dimensions for small workspace previews.
func ImagePreviewDataURL(data []byte) (string, error) {
	if len(data) > ImagePreviewMaxBytes {
		return "", fmt.Errorf("image exceeds the 2 MiB workspace preview limit")
	}
	mediaType := http.DetectContentType(data)
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
