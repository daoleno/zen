//go:build linux && cgo && zen_desktop

package nativebind

import "github.com/daoleno/zen/daemon/desktop/nativebind/impl"

const NativeLinked = true

func RunHelper(args []string) error {
	return impl.RunHelper(args)
}

func RunAgent(args []string) error {
	return impl.RunAgent(args)
}
