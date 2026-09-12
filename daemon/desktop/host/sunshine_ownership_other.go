//go:build !linux

package host

// The supervised Sunshine host is Linux-only; the ownership API stays present
// for cross-platform builds and always reports closed availability.

func SunshineOwnershipBound() bool { return false }

func SunshineOwner() (string, string) { return "", "" }
