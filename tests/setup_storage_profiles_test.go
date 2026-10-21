package tests

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/agim/lidza/pkg/credentials"
)

func TestSetupStorageProfilesSaveCanonicalSettingsAndKeepNamespace(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	for _, test := range []struct{ profile, location, endpoint, region string }{
		{"aws", "eu-west-1", "https://s3.eu-west-1.amazonaws.com", "eu-west-1"},
		{"digitalocean", "ams3", "https://ams3.digitaloceanspaces.com", "us-east-1"},
		{"google", "", "https://storage.googleapis.com", "auto"},
	} {
		saveStep(t, s, "storage", map[string]string{"STORAGE_PROVIDER": "s3", "THURA_STORAGE_PROFILE": test.profile, "THURA_STORAGE_LOCATION": test.location, "STORAGE_ENDPOINT": "https://browser-supplied.invalid", "STORAGE_REGION": "browser-supplied", "STORAGE_BUCKET": "private-bucket", "STORAGE_PREFIX": "thura/preserved-fixture/", "STORAGE_ACCESS_KEY": "PUBLIC-new-profile-access", "STORAGE_SECRET_KEY": "PUBLIC-new-profile-secret"})
		_, draft, _ := setupRow(t, s)
		key, err := credentials.Key(".")
		if err != nil {
			t.Fatal(err)
		}
		plain, err := credentials.Decrypt(key, draft)
		if err != nil {
			t.Fatal(err)
		}
		var saved map[string]string
		if err := json.Unmarshal(plain, &saved); err != nil {
			t.Fatal(err)
		}
		if saved["STORAGE_ENDPOINT"] != test.endpoint || saved["STORAGE_REGION"] != test.region || saved["THURA_STORAGE_PROFILE"] != test.profile {
			t.Fatalf("%s profile was not derived server-side", test.profile)
		}
		if saved["STORAGE_BUCKET"] != "private-bucket" || saved["STORAGE_PREFIX"] != "thura/preserved-fixture/" {
			t.Fatal("profile rewrote bucket or namespace")
		}
		if saved["STORAGE_ACCESS_KEY"] != "PUBLIC-new-profile-access" || saved["STORAGE_SECRET_KEY"] != "PUBLIC-new-profile-secret" {
			t.Fatal("explicit key pair was not retained in the encrypted draft")
		}
	}
	response, err := s.Client().Get(s.URL + "/admin/setup?step=storage")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, choice := range []string{"AWS S3", "DigitalOcean Spaces", "Google Cloud Storage", "Custom S3-compatible", "data-storage-profile-help"} {
		if !strings.Contains(string(body), choice) {
			t.Fatalf("storage selection missing %q", choice)
		}
	}
	if strings.Contains(string(body), "PUBLIC-new-profile-access") || strings.Contains(string(body), "PUBLIC-new-profile-secret") {
		t.Fatal("storage credentials were exposed in the page")
	}
}
