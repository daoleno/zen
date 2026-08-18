package modelprofiles

import (
	"encoding/json"
	"testing"
)

// The Codex CLI >= 0.147 ModelInfo serde contract requires a fixed set of
// fields on every model_catalog_json / GET /v1/models entry. Missing fields
// make codex exit at config load, which kills the host tmux session and
// drives the brain host missing_tmux replacement loop, so the wire shape is
// pinned here (mirrors codex-rs protocol/src/openai_models.rs ModelInfo).
func TestCodexModelCatalogWireContractRequiredFields(t *testing.T) {
	resp := CodexModelsResponseForModels([]string{"gpt-5.2", "gpt-5.4-mini", "deepseek-v4-flash"})
	if len(resp.Models) != 3 {
		t.Fatalf("models=%d want 3", len(resp.Models))
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Models []map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	required := []string{
		"slug",
		"display_name",
		"supported_reasoning_levels",
		"shell_type",
		"visibility",
		"supported_in_api",
		"priority",
		"base_instructions",
		"support_verbosity",
		"truncation_policy",
		"supports_parallel_tool_calls",
		"experimental_supported_tools",
	}
	for i, entry := range parsed.Models {
		for _, field := range required {
			if _, ok := entry[field]; !ok {
				t.Fatalf("model entry %d (%s) missing required field %q", i, entry["slug"], field)
			}
		}
		for _, field := range []string{"slug", "display_name", "shell_type", "visibility", "base_instructions"} {
			var value string
			if err := json.Unmarshal(entry[field], &value); err != nil || value == "" {
				t.Fatalf("model entry %d field %q must be a non-empty string, got: %s", i, field, entry[field])
			}
		}
		var levels []json.RawMessage
		if err := json.Unmarshal(entry["supported_reasoning_levels"], &levels); err != nil {
			t.Fatalf("model entry %d supported_reasoning_levels must be a sequence, got: %s", i, entry["supported_reasoning_levels"])
		}
		if string(entry["supported_reasoning_levels"]) == "null" {
			t.Fatalf("model entry %d supported_reasoning_levels must not be null", i)
		}
		var tools []json.RawMessage
		if err := json.Unmarshal(entry["experimental_supported_tools"], &tools); err != nil {
			t.Fatalf("model entry %d experimental_supported_tools must be a sequence", i)
		}
		if string(entry["experimental_supported_tools"]) == "null" {
			t.Fatalf("model entry %d experimental_supported_tools must not be null", i)
		}
		var trunc struct {
			Mode  string `json:"mode"`
			Limit int64  `json:"limit"`
		}
		if err := json.Unmarshal(entry["truncation_policy"], &trunc); err != nil {
			t.Fatalf("model entry %d truncation_policy must be {mode,limit}: %s", i, entry["truncation_policy"])
		}
		if trunc.Mode != "tokens" || trunc.Limit <= 0 {
			t.Fatalf("model entry %d truncation_policy=%+v want tokens mode with a positive limit", i, trunc)
		}
	}
}
