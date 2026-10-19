package setup

import (
	"net/http/httptest"
	"testing"
)

func TestMailRoutingSeparatesReceivingAndEveryOutgoingProvider(t *testing.T) {
	t.Setenv("THURA_POSTFIX_TOKEN", "PUBLIC-private-sidecar-token-with-32-characters")
	for _, provider := range []string{"smtp", "mailgun", "sendgrid", "postmark", "resend"} {
		v := validSetup()
		v["THURA_MAIL_INBOUND"] = "postfix"
		v["THURA_POSTFIX_HOSTNAME"] = "mail.team.example"
		v["THURA_MAIL_ADDRESS"] = "inbox@team.example"
		v["MAIL_PROVIDER"] = provider
		v["MAIL_API_KEY"] = "PUBLIC-provider-key"
		v["MAIL_DOMAIN"] = "team.example"
		if err := validate(v); err != nil {
			t.Fatalf("Postfix receiving + %s sending: %v", provider, err)
		}
		if inboxAddress(v) != "inbox@team.example" {
			t.Fatal("incoming address conflated with sender")
		}
	}
	v := validSetup()
	v["THURA_MAIL_INBOUND"], v["THURA_MAIL_OUTBOUND"] = "postfix", "postfix"
	v["THURA_POSTFIX_HOSTNAME"] = "mail.team.example"
	v["MAIL_SMTP_HOST"], v["MAIL_SMTP_PORT"] = "127.0.0.1", "2525"
	if err := validate(v); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct{ key, value string }{
		{"THURA_POSTFIX_HOSTNAME", "mail.example\nrelay_domains=*"}, {"THURA_POSTFIX_HOSTNAME", "localhost"},
		{"THURA_MAIL_ADDRESS", "Person <inbox@team.example>"}, {"THURA_MAIL_ADDRESS", "INBOX@team.example"},
		{"MAIL_SMTP_HOST", "other.example"}, {"MAIL_PROVIDER", "mailgun"}, {"MAIL_SMTP_USERNAME", "user"},
	} {
		prior := v[change.key]
		v[change.key] = change.value
		if validate(v) == nil {
			t.Fatalf("invalid managed mail setting %s admitted", change.key)
		}
		v[change.key] = prior
	}
	t.Setenv("THURA_POSTFIX_TOKEN", "")
	if validate(v) == nil {
		t.Fatal("managed service allowed without private deployment token")
	}
}

func TestPostfixRejectsRemoteSourceEvenWithToken(t *testing.T) {
	t.Setenv("THURA_POSTFIX_TOKEN", "PUBLIC-private-sidecar-token-with-32-characters")
	for _, remote := range []string{"192.168.1.5:2000", "[2001:db8::1]:2000", "invalid"} {
		request := httptest.NewRequest("GET", "/api/v1/setup/postfix", nil)
		request.RemoteAddr = remote
		request.Header.Set("Authorization", "Bearer PUBLIC-private-sidecar-token-with-32-characters")
		request.Header.Set("X-Forwarded-For", "127.0.0.1")
		result := httptest.NewRecorder()
		PostfixConfig(result, request)
		if result.Code != 403 {
			t.Fatal("remote sidecar config request admitted")
		}
	}
}

func TestNewWizardRecommendsAPIWithoutChangingExistingSnapshotDefaults(t *testing.T) {
	if draftDefaults()["MAIL_PROVIDER"] != "mailgun" || draftDefaults()["THURA_MAIL_OUTBOUND"] != "provider" {
		t.Fatal("new wizard does not recommend API delivery")
	}
	if defaults()["MAIL_PROVIDER"] != "smtp" {
		t.Fatal("compatibility defaults would change existing installations")
	}
	mailStep, ok := stepByID("mail")
	if !ok {
		t.Fatal("mail step missing")
	}
	for _, field := range mailStep.Fields {
		if field.Name == "MAIL_PROVIDER" {
			if len(field.Options) != 5 || field.Options[0] != "mailgun" || field.Options[len(field.Options)-1] != "smtp" {
				t.Fatal("API providers must appear before SMTP")
			}
		}
		if field.Name == "THURA_MAIL_OUTBOUND" && field.Options[0] != "provider" {
			t.Fatal("external API provider choice should come first")
		}
	}
}
