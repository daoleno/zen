package nativebind

import "errors"

// ErrNativeUnavailable is returned when this ELF has no cgo-linked capture/agent.
var ErrNativeUnavailable = errors.New("this zen binary was built without desktop native support; rebuild with CGO and -tags zen_desktop after installing GTK3, GStreamer, X11 and XTest development files")
