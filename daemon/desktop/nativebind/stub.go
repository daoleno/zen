//go:build !zen_desktop || !linux || !cgo

package nativebind

func RunHelper(args []string) error {
	_ = args
	return ErrNativeUnavailable
}

func RunAgent(args []string) error {
	_ = args
	return ErrNativeUnavailable
}

// NativeLinked is false when this ELF was built without Linux desktop CGO.
const NativeLinked = false

func NativeBuildInput() string {
	return ""
}
