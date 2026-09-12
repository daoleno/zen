//go:build !linux

package host

import (
	"context"
	"errors"
)

// The supervised Sunshine host is Linux-only; these adapters keep
// cross-platform builds honest and report closed availability.

var ErrSunshineEnrollmentNotFound = errors.New("sunshine_enrollment_not_found")
var ErrSunshineStateCorrupt = errors.New("sunshine_state_unreadable")

func SunshineOwnershipBound() bool { return false }

func SunshineEnrollment(string) (string, bool, error) { return "", false, nil }

func EnrollFromState(string, string, string) error { return errors.New("sunshine_admin_unsupported") }

func CancelSunshineEnrollment(string) error { return errors.New("sunshine_admin_unsupported") }

func NewEnrollmentNonce() (string, error) { return "", errors.New("sunshine_admin_unsupported") }

func CertFingerprint(string) (string, error) { return "", errors.New("sunshine_admin_unsupported") }

func VerifyEnrollmentProof(string, string, string, string) error {
	return errors.New("sunshine_admin_unsupported")
}

func SunshineEnrollmentByCert(string) (string, bool, error) { return "", false, nil }

func SunshineAdmission(string) (string, error) { return "pending", nil }

func RevokeSunshineTarget(context.Context, string) error { return nil }
