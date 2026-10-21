package setup

import (
	"os"
	"testing"
)

func TestFreshInstallationStorageNamespacesAreStableAndDistinct(t *testing.T) {
	t.Setenv("STORAGE_PREFIX", "")
	os.Unsetenv("STORAGE_PREFIX")
	first := installationDraft("10000000-0000-4000-8000-000000000001")
	again := installationDraft("10000000-0000-4000-8000-000000000001")
	other := installationDraft("10000000-0000-4000-8000-000000000002")
	if first["STORAGE_PREFIX"] != "thura/10000000-0000-4000-8000-000000000001/" || first["STORAGE_PREFIX"] != again["STORAGE_PREFIX"] || first["STORAGE_PREFIX"] == other["STORAGE_PREFIX"] {
		t.Fatal("installation namespace must be stable and unique across databases")
	}
	for _, prefix := range []string{"operator-chosen/", ""} {
		t.Setenv("STORAGE_PREFIX", prefix)
		if installationDraft("10000000-0000-4000-8000-000000000001")["STORAGE_PREFIX"] != prefix {
			t.Fatal("explicit operator namespace changed")
		}
	}
	if defaults()["STORAGE_PREFIX"] != "" {
		t.Fatal("older snapshots must retain their original unprefixed location")
	}
}

func TestPrivateIntegrationOriginsRequireExplicitOperatorAllowlist(t *testing.T) {
	t.Setenv("THURA_SERVICE_HTTP_ORIGINS", "")
	for _, name := range []string{"COLLABORA_SERVER_URL", "COLLABORA_APP_URL", "LIVEKIT_SERVER_URL"} {
		if integrationOrigin(name, "http://collabora:9980") {
			t.Fatal("unlisted private HTTP origin accepted")
		}
	}
	t.Setenv("THURA_SERVICE_HTTP_ORIGINS", "http://collabora:9980,http://thura:3000,http://127.0.0.1:7880")
	for name, raw := range map[string]string{"COLLABORA_SERVER_URL": "http://collabora:9980", "COLLABORA_APP_URL": "http://thura:3000", "LIVEKIT_SERVER_URL": "http://127.0.0.1:7880"} {
		if !integrationOrigin(name, raw) {
			t.Fatal("explicit private origin rejected", name)
		}
	}
	for _, raw := range []string{"http://collabora:9999", "http://collabora:9980/redirect", "http://collabora:9980?token=secret", "http://user:secret@collabora:9980", "http://outside.example", "http://169.254.169.254", "http://127.0.0.1:7881"} {
		if integrationOrigin("COLLABORA_SERVER_URL", raw) {
			t.Fatal("unlisted or unsafe origin accepted", raw)
		}
	}
	for _, name := range []string{"APP_URL", "MATRIX_SERVER_URL", "STORAGE_ENDPOINT"} {
		if integrationOrigin(name, "http://collabora:9980") {
			t.Fatal("private exception widened public validation", name)
		}
	}
	v := validSetup()
	v["COLLABORA_SERVER_URL"] = "http://collabora:9980"
	v["COLLABORA_APP_URL"] = "http://thura:3000"
	v["COLLABORA_TOKEN_SECRET"] = "public-synthetic-collabora-secret-at-least-32"
	v["LIVEKIT_SERVER_URL"] = "http://127.0.0.1:7880"
	v["LIVEKIT_PUBLIC_URL"] = "wss://meet.team.example"
	v["LIVEKIT_API_KEY"] = "synthetic"
	v["LIVEKIT_API_SECRET"] = "public-synthetic-livekit-secret-at-least-32"
	if err := validate(v); err != nil {
		t.Fatal(err)
	}
	v["LIVEKIT_PUBLIC_URL"] = "ws://meet.team.example"
	if validate(v) == nil {
		t.Fatal("public meeting URL lost secure WSS requirement")
	}
	v["LIVEKIT_PUBLIC_URL"] = "wss://meet.team.example"
	v["APP_URL"] = "http://thura:3000"
	if validate(v) == nil {
		t.Fatal("public app URL lost HTTPS requirement")
	}
}

func TestBootstrapDraftSeedsOnlyCataloguedOperatorSettings(t *testing.T) {
	t.Setenv("APP_URL", "https://managed.team.example")
	t.Setenv("STORAGE_DIR", "/var/lib/lidza-storage")
	t.Setenv("MAIL_PROVIDER", "smtp")
	t.Setenv("COLLABORA_SERVER_URL", "http://collabora:9980")
	t.Setenv("COLLABORA_TOKEN_SECRET", "public-synthetic-private-seed")
	t.Setenv("UNCATALOGUED_PRIVATE_TOKEN", "must-not-be-saved")
	got := bootstrapDraft()
	for key, want := range map[string]string{"APP_URL": "https://managed.team.example", "STORAGE_DIR": "/var/lib/lidza-storage", "MAIL_PROVIDER": "smtp", "COLLABORA_SERVER_URL": "http://collabora:9980", "COLLABORA_TOKEN_SECRET": "public-synthetic-private-seed"} {
		if got[key] != want {
			t.Fatal("operator setting not seeded", key)
		}
	}
	if _, ok := got["UNCATALOGUED_PRIVATE_TOKEN"]; ok {
		t.Fatal("unrelated process secret captured")
	}
	if draftDefaults()["MAIL_PROVIDER"] != "mailgun" {
		t.Fatal("ordinary setup API-first preference changed")
	}
}
