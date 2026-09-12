//go:build !linux

package host

import "context"

// The supervised Sunshine host is Linux-only; these adapters keep
// cross-platform builds honest and report closed availability.

func SunshineOwnershipBound() bool { return false }

func SunshineEnrollment(string) (string, bool) { return "", false }

func RevokeSunshineTarget(context.Context, string) error { return nil }
