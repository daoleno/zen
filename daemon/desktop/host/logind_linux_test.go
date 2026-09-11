package host

import (
	"context"
	"os"
	"strings"
	"testing"
)

const greeterReply = `{"type":"a{sv}","data":[{"Id":{"type":"s","data":"c1"},"User":{"type":"(uo)","data":[959,"/org/freedesktop/login1/user/_959"]},"Seat":{"type":"(so)","data":["seat0","/org/freedesktop/login1/seat/seat0"]},"Display":{"type":"s","data":":7"},"Type":{"type":"s","data":"x11"},"Class":{"type":"s","data":"greeter"},"Active":{"type":"b","data":true},"State":{"type":"s","data":"active"},"LockedHint":{"type":"b","data":false}}]}`

func TestLogindGreeterIsMetadataNotPermission(t *testing.T) {
	s, err := DecodeLinuxObservation([]byte(greeterReply), "fixture-boot")
	if err != nil {
		t.Fatal(err)
	}
	if s.Session.Surface != Greeter || s.Session.UID != 959 || s.Display != ":7" || s.Session.Ready || s.Session.Control {
		t.Fatal("incorrect greeter or fabricated permission")
	}
	for _, bad := range []string{
		strings.Replace(greeterReply, `"data":"x11"`, `"data":"tty"`, 1),
		strings.Replace(greeterReply, `"data":true`, `"data":false`, 1),
		strings.Replace(greeterReply, `"data":["seat0",`, `"data":["seat1",`, 1),
		strings.Replace(greeterReply, `"data":[959,`, `"data":[0,`, 1),
		strings.Replace(greeterReply, `"LockedHint"`, `"Ignored"`, 1),
		strings.Replace(greeterReply, `"type":"b"`, `"type":"s"`, 1),
		strings.Replace(greeterReply, `"data":false`, `"data":null`, 1),
		strings.Repeat(" ", 65537),
	} {
		if bad == greeterReply {
			t.Fatal("negative fixture did not change")
		}
		if _, err := DecodeLinuxObservation([]byte(bad), "fixture-boot"); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
	user := strings.Replace(greeterReply, `"data":"greeter"`, `"data":"user"`, 1)
	user = strings.Replace(user, `"data":false`, `"data":true`, 1)
	s, err = DecodeLinuxObservation([]byte(user), "fixture-boot")
	if err != nil || s.Session.Surface != Locked || s.Session.Ready {
		t.Fatal("lock hint mistaken for ready backend")
	}
}

func TestReadOnlyLinuxHostPreflight(t *testing.T) {
	if os.Getenv("ZEN_DESKTOP_READONLY_PREFLIGHT") != "1" {
		t.Skip("explicit read-only host metadata check")
	}
	s, err := InspectLinux(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Session.Ready || s.Session.Control {
		t.Fatal("preflight granted screen access")
	}
	t.Logf("active class=%s backend=%s surface=%s ready=false control=false", s.Class, s.Session.Backend, s.Session.Surface)
}
