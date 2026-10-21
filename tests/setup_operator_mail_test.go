package tests

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/agim/lidza/packs/db"
	q "thura/db/queries/gen"
	"thura/internal/setup"
)

func TestSetupOperatorAliasesUseAdministratorMailboxForPostfixAndSMIP(t *testing.T) {
	s := setupServer(t)
	t.Setenv("THURA_POSTFIX_TOKEN", "PUBLIC-postfix-operator-alias-test-token-32")
	claimSetup(t, s)
	completeSetup(t, s)
	port, _ := setupSMTP(t)
	saveStep(t, s, "mail", map[string]string{"THURA_MAIL_INBOUND": "postfix", "THURA_POSTFIX_HOSTNAME": "mail.setup.example", "MAIL_FROM": "team@setup.example", "MAIL_SMTP_HOST": "127.0.0.1", "MAIL_SMTP_PORT": port, "MAIL_SMTP_SECURITY": "none", "MAIL_INBOUND_SECRET": "PUBLIC-inbound-test-secret-with-32-characters", "MAIL_AI_SCREENING": "false"})
	runSetupChecks(t, s)
	revision, _, _ := setupRow(t, s)
	r := setupForm(t, s, "publish", url.Values{"revision": {strconv.Itoa(revision)}, "confirm": {"publish"}})
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("operator setup publication failed")
	}
	if err := setup.Activate(s.Context(), s.Services); err != nil {
		t.Fatal(err)
	}
	queries := q.New(db.From(s.Context()))
	var target string
	for _, local := range []string{"postmaster", "abuse", "hostmaster", "webmaster", "security", "noc", "admin", "administrator", "root", "mailer-daemon"} {
		boxes, err := queries.FindSmipReceivingMailbox(s.Context(), local+"@setup.example")
		if err != nil || len(boxes) != 1 {
			t.Fatal("operator alias is not a unique SMIP recipient", local)
		}
		if target == "" {
			target = boxes[0].ID
		}
		if boxes[0].ID != target || boxes[0].Address != "team@setup.example" {
			t.Fatal("operator alias escaped its initial administrator workspace mailbox")
		}
	}
	request := httptest.NewRequest("GET", "/api/v1/setup/postfix", nil).WithContext(s.Context())
	request.RemoteAddr = "127.0.0.1:2525"
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("unused", 8))
	denied := httptest.NewRecorder()
	setup.PostfixConfig(denied, request)
	if denied.Code != http.StatusForbidden {
		t.Fatal("operator routes leaked without sidecar authentication")
	}
	request.Header.Set("Authorization", "Bearer PUBLIC-postfix-operator-alias-test-token-32")
	recorder := httptest.NewRecorder()
	setup.PostfixConfig(recorder, request)
	if recorder.Code != 200 {
		t.Fatal("managed alias configuration unavailable")
	}
	var config struct {
		Config struct {
			Mailboxes map[string]struct{ ID, Secret string }
		}
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	route := config.Config.Mailboxes["postmaster@setup.example"]
	if route.ID != target || route.Secret == "" {
		t.Fatal("Postfix alias does not route to administrator mailbox")
	}
	if _, exists := config.Config.Mailboxes["unknown@setup.example"]; exists {
		t.Fatal("operator aliases introduced a catch-all")
	}
	// Exercise the exact signed-relay target that Postfix uses for the alias.
	raw := "From: sender@outside.example\r\nTo: postmaster@setup.example\r\nSubject: Operator notice\r\n\r\nDelivery check."
	stamp := strconv.FormatInt(s.Clock.Now().Unix(), 10)
	delivery := "operator-alias-delivery"
	mac := hmac.New(sha256.New, []byte(route.Secret))
	mac.Write([]byte(stamp + "." + delivery + "." + raw))
	inbound, err := http.NewRequest("POST", s.URL+"/api/v1/inbound/"+target, bytes.NewBufferString(raw))
	if err != nil {
		t.Fatal(err)
	}
	inbound.Header.Set("Content-Type", "message/rfc822")
	inbound.Header.Set("X-Thura-Timestamp", stamp)
	inbound.Header.Set("X-Thura-Delivery", delivery)
	inbound.Header.Set("X-Thura-Signature", hex.EncodeToString(mac.Sum(nil)))
	response, err := s.Client().Do(inbound)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("operator alias delivery failed", response.StatusCode)
	}
	item, err := queries.FindInboundMail(s.Context(), q.FindInboundMailParams{MailboxID: target, ExternalID: &delivery})
	if err != nil || item.ToAddress != "postmaster@setup.example" {
		t.Fatal("operator notice was not stored with its original recipient")
	}
}
