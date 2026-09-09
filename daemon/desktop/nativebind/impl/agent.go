//go:build linux && cgo && zen_desktop

package impl

/*
#cgo CFLAGS: -std=c11 -DZEN_DESKTOP_CGO -I${SRCDIR}/../../native
#cgo pkg-config: gtk+-3.0 gstreamer-app-1.0 gstreamer-video-1.0 x11 xtst gio-unix-2.0
#include "../../native/host-agent.c"
*/
import "C"
