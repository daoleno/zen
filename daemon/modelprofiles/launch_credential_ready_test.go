package modelprofiles

import (
	"errors"
	"strings"
	"testing"
)

const testStoreOnlySecret = "sk-test-store-only-never-on-wire"

func TestPrepareLaunchCustomConnectionUsesStoreCredential(t *testing.T) {
	owner := startTestOwner(t, readyLookup(""))
	store := NewMemoryCredentialStore()
	owner.SetCredentialStore(store)

	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "custom-gw", Name: "Custom Gateway", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		ModelID: "up-1", Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderCredential("custom-gw", testStoreOnlySecret); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderDefault(ClientCodex, "custom-gw", "up-1", proj.Revision); err != nil {
		t.Fatal(err)
	}

	plan, err := owner.PrepareLaunch(ExecutorCodex, "", "codex")
	if err != nil {
		t.Fatalf("store secret must satisfy launch auth: %v", err)
	}
	if plan.Bypass {
		t.Fatal("custom default must not bypass profiles")
	}
	if plan.Env[EnvOpenAIAPIKey] != LoopbackAuthPlaceholder {
		t.Fatalf("launch env=%v", plan.Env)
	}
	if strings.Contains(plan.Command, testStoreOnlySecret) {
		t.Fatal("store secret leaked into launch command")
	}
	for _, value := range plan.Env {
		if strings.Contains(value, testStoreOnlySecret) {
			t.Fatal("store secret leaked into launch env")
		}
	}
	if !plan.State.Binding.CredentialReady {
		t.Fatal("binding must report credential ready from store")
	}
}

func TestPrepareLaunchCustomConnectionFailClosedWithoutStoreOrEnv(t *testing.T) {
	owner := startTestOwner(t, readyLookup(""))
	proj, err := owner.UpsertProviderConnection(ProviderConnectionInput{
		ID: "custom-gw", Name: "Custom Gateway", Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: "https://gateway.example/v1",
		ModelID: "up-1", Advanced: true,
	}, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderDefault(ClientCodex, "custom-gw", "up-1", proj.Revision); err != nil {
		t.Fatal(err)
	}
	_, err = owner.PrepareLaunch(ExecutorCodex, "", "codex")
	if !errors.Is(err, ErrCredentialNotReady) {
		t.Fatalf("want ErrCredentialNotReady got %v", err)
	}
	if !strings.Contains(err.Error(), "ZEN_PROVIDER_API_KEY") {
		t.Fatalf("err=%v", err)
	}
}

func TestPrepareLaunchCuratedEnvOnlyStillWorks(t *testing.T) {
	owner := startTestOwner(t, readyLookup("host-exported-openai-key"))
	profile := Profile{
		ID: "openai-host", Name: "OpenAI", ExecutorID: ExecutorCodex,
		ProviderID: "openai", ProviderLabel: "OpenAI",
		Protocol: ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "gpt-5",
		ClientModelProvenance: ContractProvenanceConfiguredCompatibility,
		BaseURL:               "https://api.openai.com/v1",
		AuthMode:              AuthModeBearerEnv,
		CredentialEnv:         "OPENAI_API_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Env[EnvOpenAIAPIKey] != LoopbackAuthPlaceholder {
		t.Fatalf("env=%v", plan.Env)
	}
	if strings.Contains(plan.Command, "host-exported-openai-key") {
		t.Fatal("host env secret leaked into launch command")
	}
}
