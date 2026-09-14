//go:build !linux

package host

import (
	"context"
	"errors"
)

// The supervised Sunshine/Moonlight host is not implemented outside Linux.
// These adapters keep cross-platform builds honest: the capability endpoint
// never advertises Moonlight on unsupported platforms.

func SunshineConfigured() bool { return false }

func ValidateSunshineRuntime() error { return errors.New("Sunshine unsupported on this platform") }

func SunshineAvailable() bool { return false }

func SunshineSnapshot() SunshineRuntimeSnapshot { return SunshineRuntimeSnapshot{} }

func EnsureSunshineRuntime(context.Context, SunshineSpawner) (SunshineRuntimeSnapshot, error) {
	return SunshineRuntimeSnapshot{}, nil
}

func SunshineAvailabilityReason() string { return "unsupported_platform" }

func StopSunshineRuntime(context.Context) error { return nil }

func RevokeSunshineRuntime(context.Context) error { return nil }
