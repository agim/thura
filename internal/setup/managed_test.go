package setup

import "testing"

func TestPrivateMatrixOriginRequiresManagedRegistrationAndExactAllowlist(t *testing.T) {
	t.Setenv("THURA_MANAGED_SETTINGS", "")
	t.Setenv("THURA_SERVICE_HTTP_ORIGINS", "http://127.0.0.1:8008")
	if integrationOrigin("MATRIX_SERVER_URL", "http://127.0.0.1:8008") {
		t.Fatal("unmanaged Matrix allowed cleartext")
	}
	t.Setenv("THURA_MANAGED_SETTINGS", "MATRIX_SERVER_URL")
	if !integrationOrigin("MATRIX_SERVER_URL", "http://127.0.0.1:8008") {
		t.Fatal("managed registered origin rejected")
	}
	if integrationOrigin("MATRIX_SERVER_URL", "http://127.0.0.1:8009") || integrationOrigin("APP_URL", "http://127.0.0.1:8008") {
		t.Fatal("managed registration widened another origin")
	}
}

func TestManagedRepairPreservesOperatorChoicesAndStableKeys(t *testing.T) {
	t.Setenv("THURA_MANAGED_SETTINGS", "MATRIX_SERVER_URL,MATRIX_AS_TOKEN,MAIL_PROVIDER,STORAGE_PREFIX,LIVEKIT_API_SECRET")
	t.Setenv("MATRIX_SERVER_URL", "http://matrix:8008")
	t.Setenv("MATRIX_AS_TOKEN", "deployment-token")
	t.Setenv("LIVEKIT_API_SECRET", "different-deployment-key")
	t.Setenv("MAIL_PROVIDER", "smtp")
	t.Setenv("STORAGE_PREFIX", "wrong-prefix")
	v := map[string]string{"MAIL_PROVIDER": "resend", "STORAGE_PREFIX": "thura/stable/", "THURA_WORKSPACE_NAME": "Chosen team", "LLM_PROVIDER": "anthropic", "MAIL_AI_SCREENING": "false", "LIVEKIT_API_SECRET": "saved-key"}
	if !fillManaged(v) {
		t.Fatal("missing internal configuration not filled")
	}
	if v["MATRIX_SERVER_URL"] != "http://matrix:8008" || v["MATRIX_AS_TOKEN"] != "deployment-token" {
		t.Fatal("managed service not repaired")
	}
	if v["MAIL_PROVIDER"] != "resend" || v["STORAGE_PREFIX"] != "thura/stable/" || v["LIVEKIT_API_SECRET"] != "saved-key" || v["LLM_PROVIDER"] != "anthropic" || v["MAIL_AI_SCREENING"] != "false" || v["THURA_WORKSPACE_NAME"] != "Chosen team" {
		t.Fatal("redeploy replaced a saved choice or credential")
	}
	if fillManaged(v) {
		t.Fatal("repeat reconciliation changed stable settings")
	}
	if managedField("MAIL_PROVIDER") || managedField("STORAGE_PREFIX") {
		t.Fatal("deployment attempted to own user choices")
	}
}

func TestManagedLocalModelDoesNotOverrideRemoteProvider(t *testing.T) {
	t.Setenv("THURA_MANAGED_OLLAMA", "true")
	remote := map[string]string{"LLM_PROVIDER": "anthropic", "LLM_MODEL": "chosen-cloud-model", "LLM_BASE_URL": ""}
	if localModelDefaults(remote) || remote["LLM_MODEL"] != "chosen-cloud-model" || remote["LLM_BASE_URL"] != "" {
		t.Fatal("remote choice changed")
	}
	local := map[string]string{"LLM_PROVIDER": "ollama"}
	if !localModelDefaults(local) || local["LLM_MODEL"] != minimumModel || local["LLM_BASE_URL"] != managedModelURL {
		t.Fatal("managed local choice did not get a usable default")
	}
	if localModelDefaults(local) {
		t.Fatal("local model changed on repeat deployment")
	}
}
