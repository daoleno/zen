//go:build linux && cgo && zen_desktop

package impl

/*
#cgo CFLAGS: -std=c11 -DZEN_DESKTOP_CGO -I${SRCDIR}/../../native
#cgo pkg-config: gtk+-3.0 gstreamer-app-1.0 gstreamer-video-1.0 x11 xtst gio-unix-2.0
#include <stdlib.h>
int zen_desktop_helper_main(int argc, char **argv);
int zen_desktop_agent_main(int argc, char **argv);
*/
import "C"

import (
	"fmt"
	"os"
	"unsafe"
)

func RunHelper(args []string) error {
	os.Exit(callC(args, true))
	return nil
}

func RunAgent(args []string) error {
	os.Exit(callC(args, false))
	return nil
}

func callC(args []string, helper bool) int {
	if len(args) == 0 {
		return 2
	}
	cArgs := make([]*C.char, len(args)+1)
	for i, arg := range args {
		cArgs[i] = C.CString(arg)
	}
	argc := C.int(len(args))
	argv := (**C.char)(unsafe.Pointer(&cArgs[0]))
	var code C.int
	if helper {
		code = C.zen_desktop_helper_main(argc, argv)
	} else {
		code = C.zen_desktop_agent_main(argc, argv)
	}
	for _, arg := range cArgs {
		if arg != nil {
			C.free(unsafe.Pointer(arg))
		}
	}
	if code != 0 {
		fmt.Fprintf(os.Stderr, "zen desktop native role exited %d\n", int(code))
	}
	return int(code)
}
