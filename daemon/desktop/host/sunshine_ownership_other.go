//go:build !linux

package host

import (
	"context"
	"errors"
)

// The supervised Sunshine host is Linux-only; these adapters keep
// cross-platform builds honest and report closed availability.

func SunshineOwnershipBound() bool { return false }

func SunshineEnrollment(string) (string, bool, error) { return "", false, nil }

func EnrollFromState(string, string) error { return nil }

func RevokeSunshineTarget(context.Context, string) error { return nil }

// ErrSunshineEnrollmentNotFound mirrors the Linux sentinel for cross-platform
// callers; nothing can enroll on unsupported platforms.
var ErrSunshineEnrollmentNotFound = errors.New("sunshine_enrollment_not_found")

func NewEnrollmentNonce() (string, error) { return "", errors.New("sunshine_admin_unsupported") }

func CertFingerprint(string) (string, error) { return "", errors.New("sunshine_admin_unsupported") }

func VerifyEnrollmentProof(string, string, string, string) error {
	return errors.New("sunshine_admin_unsupported")
}

func SunshineEnrollmentByCert(string) (string, bool, error) { return "", false, nil }

func BindEnrollmentCertificate(string, string) error { return errors.New("sunshine_admin_unsupported") }

func SunshineAdmission(string) (string, error) { return "pending", nil }
