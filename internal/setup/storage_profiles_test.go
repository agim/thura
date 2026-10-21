package setup

import (
	"maps"
	"net/url"
	"testing"
)

func TestStorageProfilesDeriveServiceEndpointsAndSigning(t *testing.T) {
	for _, test := range []struct{ profile, location, endpoint, region string }{
		{"aws", "eu-west-1", "https://s3.eu-west-1.amazonaws.com", "eu-west-1"},
		{"digitalocean", "ams3", "https://ams3.digitaloceanspaces.com", "us-east-1"},
		{"google", "", "https://storage.googleapis.com", "auto"},
	} {
		t.Run(test.profile, func(t *testing.T) {
			values := defaults()
			values["STORAGE_PROVIDER"] = "s3"
			values["THURA_STORAGE_PROFILE"] = test.profile
			values["THURA_STORAGE_LOCATION"] = test.location
			values["STORAGE_ENDPOINT"] = "https://untrusted.example"
			values["STORAGE_PREFIX"] = "thura/retained/"
			values["STORAGE_BUCKET"] = "private-bucket"
			previous := maps.Clone(values)
			if err := configureStorageProfile(values, previous, url.Values{}); err != nil {
				t.Fatal(err)
			}
			if values["STORAGE_ENDPOINT"] != test.endpoint || values["STORAGE_REGION"] != test.region || values["STORAGE_PATH_STYLE"] != "" {
				t.Fatalf("wrong profile settings: %s %s %s", values["STORAGE_ENDPOINT"], values["STORAGE_REGION"], values["STORAGE_PATH_STYLE"])
			}
			if values["STORAGE_PREFIX"] != "thura/retained/" || values["STORAGE_BUCKET"] != "private-bucket" {
				t.Fatal("profile changed storage location")
			}
			if err := validateStorageProfile(values); err != nil {
				t.Fatal(err)
			}
			values["STORAGE_ENDPOINT"] = "https://wrong.example"
			if err := validateStorageProfile(values); err == nil {
				t.Fatal("tampered preset endpoint accepted")
			}
		})
	}
	values := defaults()
	values["STORAGE_PROVIDER"] = "s3"
	values["THURA_STORAGE_PROFILE"] = "custom"
	values["STORAGE_ENDPOINT"] = "https://objects.example:9443"
	values["STORAGE_REGION"] = "custom-region"
	values["STORAGE_PATH_STYLE"] = "false"
	before := maps.Clone(values)
	if err := configureStorageProfile(values, before, url.Values{}); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(values, before) {
		t.Fatal("custom profile settings were rewritten")
	}
	for _, profile := range []string{"aws", "digitalocean"} {
		values["THURA_STORAGE_PROFILE"] = profile
		values["THURA_STORAGE_LOCATION"] = "../../attacker"
		if err := configureStorageProfile(values, before, url.Values{}); err == nil {
			t.Fatal("invalid region accepted")
		}
	}
}

func TestStorageProfilesDoNotReuseAnotherVendorsSecrets(t *testing.T) {
	old := defaults()
	old["STORAGE_PROVIDER"] = "s3"
	old["STORAGE_ENDPOINT"] = "https://s3.us-east-1.amazonaws.com"
	old["STORAGE_ACCESS_KEY"] = "PUBLIC-old-access"
	old["STORAGE_SECRET_KEY"] = "PUBLIC-old-secret"
	changed := maps.Clone(old)
	changed["THURA_STORAGE_PROFILE"] = "google"
	if err := configureStorageProfile(changed, old, url.Values{}); err != nil {
		t.Fatal(err)
	}
	if changed["STORAGE_ACCESS_KEY"] != "" || changed["STORAGE_SECRET_KEY"] != "" {
		t.Fatal("old AWS credentials retained for Google")
	}
	changed = maps.Clone(old)
	changed["THURA_STORAGE_PROFILE"] = "aws"
	changed["THURA_STORAGE_LOCATION"] = "eu-west-1"
	if err := configureStorageProfile(changed, old, url.Values{}); err != nil {
		t.Fatal(err)
	}
	if changed["STORAGE_ACCESS_KEY"] != old["STORAGE_ACCESS_KEY"] || changed["STORAGE_SECRET_KEY"] != old["STORAGE_SECRET_KEY"] {
		t.Fatal("same-vendor region change lost keys")
	}
	changed = maps.Clone(old)
	changed["THURA_STORAGE_PROFILE"] = "google"
	changed["STORAGE_ACCESS_KEY"] = "PUBLIC-new-HMAC"
	changed["STORAGE_SECRET_KEY"] = "PUBLIC-new-secret"
	if err := configureStorageProfile(changed, old, url.Values{"STORAGE_ACCESS_KEY": {"PUBLIC-new-HMAC"}, "STORAGE_SECRET_KEY": {"PUBLIC-new-secret"}}); err != nil {
		t.Fatal(err)
	}
	if changed["STORAGE_ACCESS_KEY"] != "PUBLIC-new-HMAC" || changed["STORAGE_SECRET_KEY"] != "PUBLIC-new-secret" {
		t.Fatal("explicit replacements lost")
	}
	changed = maps.Clone(old)
	changed["STORAGE_ENDPOINT"] = "https://custom.example"
	changed["THURA_STORAGE_PROFILE"] = "custom"
	if err := configureStorageProfile(changed, old, url.Values{}); err != nil {
		t.Fatal(err)
	}
	if changed["STORAGE_ACCESS_KEY"] != "" || changed["STORAGE_SECRET_KEY"] != "" {
		t.Fatal("custom endpoint received old keys")
	}
}
