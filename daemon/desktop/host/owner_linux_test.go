package host

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidSystemUnitCgroup(t *testing.T) {
	accept := []string{"/system.slice", "/system.slice/zen-fixture-owner.service"}
	for _, group := range accept {
		if !validSystemUnitCgroup(group) {
			t.Fatalf("system unit cgroup rejected: %q", group)
		}
	}
	reject := []string{
		"",
		"/",
		"/user.slice",
		"/user.slice/user-1000.slice/session-3.scope",
		"/user.slice/user@1000.service",
		"/system.slice/../user.slice/evil.service",
		"system.slice/zen-fixture-owner.service",
	}
	for _, group := range reject {
		if validSystemUnitCgroup(group) {
			t.Fatalf("foreign cgroup accepted: %q", group)
		}
	}
}

func writeProcCgroup(t *testing.T, pid, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cgroup"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestOwnerCgroupMember(t *testing.T) {
	group := "/system.slice/zen-fixture-owner.service"
	root := writeProcCgroup(t, "4242", "0::"+group+"\n")
	if !ownerCgroupMember(root, 4242, group) {
		t.Fatal("enrolled unit member rejected")
	}
	child := writeProcCgroup(t, "4243", "0::"+group+"/inner\n")
	if !ownerCgroupMember(child, 4243, group) {
		t.Fatal("unit child member rejected")
	}
	foreign := writeProcCgroup(t, "4244", "0::/user.slice/user-1000.slice/session-3.scope\n")
	if ownerCgroupMember(foreign, 4244, group) {
		t.Fatal("foreign user-scope member accepted")
	}
	trick := writeProcCgroup(t, "4245", "0::/system.slice/zen-fixture-owner.service.evil\n")
	if ownerCgroupMember(trick, 4245, group) {
		t.Fatal("prefix-trick cgroup accepted")
	}
	if ownerCgroupMember(writeProcCgroup(t, "4246", "garbage\n"), 4246, group) {
		t.Fatal("malformed cgroup accepted")
	}
	if ownerCgroupMember(t.TempDir(), 999999999, group) {
		t.Fatal("missing pid accepted")
	}
	if ownerCgroupMember(root, 0, group) {
		t.Fatal("zero pid accepted")
	}
}

func TestVerifyCanonicalOwner(t *testing.T) {
	group := "/system.slice/zen-fixture-owner.service"
	// The enrolled MainPID itself and a watcher-spawned daemon child (same
	// unit cgroup, different PID, e.g. after a DEV rebuild) are admitted.
	if !verifyCanonicalOwner(4242, 1000, 4242, 1000, group, true) {
		t.Fatal("canonical owner rejected")
	}
	if !verifyCanonicalOwner(4243, 1000, 4242, 1000, group, true) {
		t.Fatal("watcher-spawned daemon child rejected")
	}
	cases := map[string]struct {
		pid      int32
		peerUid  uint32
		mainPID  uint32
		ownerUID uint32
		group    string
		member   bool
	}{
		"same uid outside enrolled cgroup": {7777, 1000, 4242, 1000, group, false},
		"wrong uid in enrolled cgroup":     {4242, 1001, 4242, 1000, group, true},
		"missing unit mainpid":             {4242, 1000, 0, 1000, group, true},
		"replaced unit cgroup":             {4242, 1000, 4242, 1000, "/system.slice/other.service", false},
		"foreign user-manager unit":        {4242, 1000, 4242, 1000, "/user.slice/user-1000.slice/app.service", false},
		"zero owner uid":                   {4242, 1000, 4242, 0, group, true},
	}
	for name, tc := range cases {
		if verifyCanonicalOwner(tc.pid, tc.peerUid, tc.mainPID, tc.ownerUID, tc.group, tc.member) {
			t.Fatalf("%s accepted", name)
		}
	}
	if verifyCanonicalOwner(0, 1000, 4242, 1000, group, true) {
		t.Fatal("zero peer pid accepted")
	}
	if verifyCanonicalOwner(-1, 1000, 4242, 1000, group, true) {
		t.Fatal("negative peer pid accepted")
	}
}
