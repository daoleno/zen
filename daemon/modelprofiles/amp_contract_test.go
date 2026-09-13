package modelprofiles

import "testing"

func TestAmpHasNoModelProfileCredentialContract(t *testing.T) {
	capabilities := CapabilitiesFor("amp")
	if capabilities.Supported || len(capabilities.Protocols) != 0 || len(capabilities.RouteProtocols) != 0 {
		t.Fatalf("Amp must not advertise a provider or credential route: %#v", capabilities)
	}
}
