package setup

import "testing"

func validSetup() map[string]string {
	v := defaults()
	v["APP_URL"] = "https://team.example"
	v["THURA_OPERATOR_EMAIL"] = "operator@team.example"
	v["THURA_BACKUP_PLAN"] = "Private daily backups and quarterly restore checks"
	v["MAIL_FROM"] = "team@team.example"
	v["MAIL_SMTP_HOST"] = "localhost"
	v["MAIL_SMTP_PORT"] = "25"
	v["MAIL_SMTP_SECURITY"] = "none"
	v["MAIL_INBOUND_SECRET"] = "PUBLIC-signing-test-secret-at-least-32-bytes"
	return v
}
func TestValidateSetupProviderRequirementsAndTransportRestrictions(t *testing.T) {
	for _, provider := range []string{"smtp", "mailgun", "sendgrid", "postmark", "resend"} {
		t.Run(provider, func(t *testing.T) {
			v := validSetup()
			v["MAIL_PROVIDER"] = provider
			v["MAIL_DOMAIN"] = "team.example"
			v["MAIL_API_KEY"] = "PUBLIC-provider-test-key"
			if e := validate(v); e != nil {
				t.Fatal(e)
			}
			if provider != "smtp" {
				v["MAIL_API_KEY"] = ""
				if validate(v) == nil {
					t.Fatal("missing API credential admitted")
				}
			}
		})
	}
	for _, change := range []struct{ name, value string }{{"APP_URL", "http://team.example"}, {"APP_URL", "https://user:password@team.example"}, {"APP_URL", "https://team.example/?token=private"}, {"MAIL_SMTP_HOST", "external.example"}, {"THURA_MAX_MEMBERS", "0"}, {"DRIVE_QUOTA_BYTES", "1099511627777"}, {"MAIL_MAX_RECIPIENTS", "51"}, {"THURA_TIMEZONE", "Imaginary/Zone"}, {"THURA_BACKUP_PLAN", ""}, {"MAIL_INBOUND_SECRET", "short"}, {"MAIL_PROVIDER", "log"}} {
		t.Run(change.name+change.value, func(t *testing.T) {
			v := validSetup()
			v[change.name] = change.value
			if validate(v) == nil {
				t.Fatal("invalid deployment configuration admitted")
			}
		})
	}
	v := validSetup()
	v["STORAGE_PROVIDER"] = "s3"
	v["STORAGE_BUCKET"] = "private-team"
	v["STORAGE_ACCESS_KEY"] = "PUBLIC-test-access"
	v["STORAGE_SECRET_KEY"] = "PUBLIC-test-secret"
	if e := validate(v); e != nil {
		t.Fatal(e)
	}
	v["STORAGE_SECRET_KEY"] = ""
	if validate(v) == nil {
		t.Fatal("incomplete S3 credentials admitted")
	}
	v = validSetup()
	v["MATRIX_SERVER_URL"] = "https://matrix.example"
	if validate(v) == nil {
		t.Fatal("incomplete enabled Matrix integration admitted")
	}
	v = validSetup()
	v["SMIP_ENABLED"] = "true"
	v["SMIP_CONFIG"] = "{}"
	if validate(v) == nil {
		t.Fatal("unpaired SMIP admitted")
	}
}
