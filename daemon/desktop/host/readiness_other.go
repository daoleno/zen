//go:build !linux

package host

func InspectReadiness() Readiness {
	return Readiness{
		Status:  ReadinessUnsupported,
		Surface: string(Unavailable),
		Session: "unavailable",
	}
}
