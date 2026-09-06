package modelprofiles

import (
	"strings"
	"testing"
)

func TestCatalogPromptUsesHarnessCapabilitiesAndScopedVerification(t *testing.T) {
	for _, required := range []string{"active Host or Worker role", "capabilities exposed by the harness", "direct system/developer/user instructions take precedence", "Preserve unrelated edits and user data", "required repository checks", "Broaden or repeat checks only after changes"} {
		if !strings.Contains(codexCatalogBaseInstructions, required) {
			t.Fatalf("base instructions missing %q", required)
		}
	}
	if len(codexCatalogBaseInstructions) > 2500 {
		t.Fatalf("base instructions grew to %d bytes", len(codexCatalogBaseInstructions))
	}
	for _, obsolete := range []string{"Copied verbatim", "A tool named", "High-quality plans", "Low-quality plans"} {
		if strings.Contains(codexCatalogBaseInstructions, obsolete) {
			t.Fatalf("obsolete scaffold %q", obsolete)
		}
	}
}
