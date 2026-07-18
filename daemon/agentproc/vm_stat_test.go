//go:build linux || darwin

package agentproc

import "testing"

func TestParseDarwinVMStatAvailableMemory(t *testing.T) {
	fixture := []byte(`Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               45231.
Pages active:                            892341.
Pages inactive:                          201234.
Pages speculative:                         1234.
Pages throttled:                              0.
Pages wired down:                        234567.
Pages purgeable:                          45678.
"Translation faults":                  11132566.
`)
	got, err := parseDarwinVMStat(fixture)
	if err != nil {
		t.Fatal(err)
	}
	const pageSize = uint64(16384)
	want := (45231 + 201234 + 1234) * pageSize
	if got != want {
		t.Fatalf("available = %d, want %d (must not include purgeable)", got, want)
	}
}

func TestParseDarwinVMStatLegacyFourKilobytePages(t *testing.T) {
	fixture := []byte(`Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages free:                               438386.
Pages active:                             236438.
Pages inactive:                           113750.
Pages speculative:                         34293.
Pages wired down:                         225027.
`)
	got, err := parseDarwinVMStat(fixture)
	if err != nil {
		t.Fatal(err)
	}
	want := uint64((438386 + 113750 + 34293) * 4096)
	if got != want {
		t.Fatalf("available = %d, want %d", got, want)
	}
}

func TestParseDarwinVMStatRejectsMissingPageSize(t *testing.T) {
	if _, err := parseDarwinVMStat([]byte("Pages free: 1.\n")); err == nil {
		t.Fatal("expected missing page size error")
	}
}

func TestParseDarwinVMStatIgnoresCommaGroupedCounts(t *testing.T) {
	fixture := []byte(`Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages free:                             1,234.
Pages inactive:                         5,678.
Pages speculative:                          9.
`)
	got, err := parseDarwinVMStat(fixture)
	if err != nil {
		t.Fatal(err)
	}
	want := uint64((1234 + 5678 + 9) * 4096)
	if got != want {
		t.Fatalf("available = %d, want %d", got, want)
	}
}
