package main

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestRunRejectsNonPositiveAndOverflowingCapacityFlagsBeforeAllocation(
	t *testing.T,
) {
	t.Setenv(
		"ZEN_LINK_CONNECTOR_TOKEN",
		"abcdef0123456789abcdef0123456789",
	)
	flags := []string{
		"max-routes",
		"max-clients",
		"max-clients-per-route",
		"max-client-handshakes",
		"max-connector-handshakes",
		"max-admissions",
		"max-admissions-per-route",
		"max-nonces",
		"max-pending-streams",
	}
	for _, name := range flags {
		for _, value := range []int{0, -1, math.MaxInt} {
			t.Run(name+"/"+strconv.Itoa(value), func(t *testing.T) {
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Fatalf("run panicked: %v", recovered)
					}
				}()
				err := run([]string{
					"-" + name + "=" + strconv.Itoa(value),
				})
				if err == nil ||
					!strings.Contains(err.Error(), name) {
					t.Fatalf("invalid flag error=%v", err)
				}
			})
		}
	}
}

func TestRunRejectsNonPositiveDurationFlags(t *testing.T) {
	t.Setenv(
		"ZEN_LINK_CONNECTOR_TOKEN",
		"abcdef0123456789abcdef0123456789",
	)
	for _, name := range []string{
		"handshake-timeout",
		"attach-timeout",
		"idle-timeout",
		"sweep-interval",
	} {
		for _, value := range []string{"0s", "-1s"} {
			t.Run(name+"/"+value, func(t *testing.T) {
				err := run([]string{"-" + name + "=" + value})
				if err == nil ||
					!strings.Contains(err.Error(), name) {
					t.Fatalf("invalid duration error=%v", err)
				}
			})
		}
	}
}
