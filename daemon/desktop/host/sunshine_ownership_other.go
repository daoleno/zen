//go:build !linux

package host

import "context"

// The supervised Sunshine host is Linux-only; these adapters keep
// cross-platform builds honest and report closed availability.

func SunshineOwnershipBound() bool { return false }

func SunshineEnrollment(string) (string, bool, error) { return "", false, nil }

func EnrollFromState(string, string) error { return nil }

func RevokeSunshineTarget(context.Context, string) error { return nil }
