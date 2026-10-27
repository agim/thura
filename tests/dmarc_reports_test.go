package tests

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agim/lidza/packs/db"
	"thura/handlers"
	"thura/internal/dmarcreports"
)

func TestDMARCReportPrivateSubmissionIsIdempotentAndUsesNormalOutbox(t *testing.T) {
	n := newMailNode(t, "reports.example")
	ctx := n.server.Context()
	t.Setenv("THURA_POSTFIX_TOKEN", postfixFixtureToken)
	t.Setenv("THURA_MAIL_ADDRESS", n.box.Address)
	if _, err := db.From(ctx).Exec(ctx, "UPDATE server_setup SET subject=$1,workspace_id=$2,published_revision=1 WHERE id='00000000-0000-4000-8000-000000000001'", n.owner.Subject, n.workspace.ID); err != nil {
		t.Fatal(err)
	}
	xml := `<feedback><report_metadata><report_id>real-aggregate-shape</report_id></report_metadata><policy_published><domain>sender.example</domain></policy_published><record><row><source_ip>203.0.113.7</source_ip></row></record></feedback>`
	raw := []byte(fmt.Sprintf("From: postmaster@reports.example\r\nTo: reports@collector.example\r\nSubject: Report Domain: sender.example\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=report\r\n\r\n--report\r\nContent-Type: text/plain\r\n\r\nAuthentication counts only.\r\n--report\r\nContent-Type: application/xml\r\nContent-Disposition: attachment; filename=\"report.xml\"\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n--report--\r\n", base64.StdEncoding.EncodeToString([]byte(xml))))
	send := func(token, peer string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/v1/setup/postfix/reports", bytes.NewReader(body)).WithContext(ctx)
		r.RemoteAddr = peer
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Thura-Report-Recipients", `["reports@collector.example"]`)
		w := httptest.NewRecorder()
		handlers.ReceiveDMARCReport(w, r)
		return w
	}
	for _, tc := range []struct{ token, peer string }{{"wrong", "127.0.0.1:23"}, {postfixFixtureToken, "203.0.113.7:23"}} {
		if w := send(tc.token, tc.peer, raw); w.Code != 403 {
			t.Fatal("private report authorization bypass", w.Code)
		}
	}
	var id string
	for i := 0; i < 2; i++ {
		w := send(postfixFixtureToken, "127.0.0.1:23", raw)
		if w.Code != http.StatusAccepted {
			_, err := dmarcreports.Queue(ctx, raw, []string{"reports@collector.example"})
			t.Fatal(w.Code, w.Body.String(), err)
		}
		var response map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			id = response["id"]
		} else if id != response["id"] {
			t.Fatal("report retry created another message")
		}
	}
	var count, files int
	var status, to, from string
	if err := db.From(ctx).QueryRow(ctx, "SELECT count(*) FROM mail_item WHERE external_id LIKE 'dmarc:%'").Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate report", count, err)
	}
	if err := db.From(ctx).QueryRow(ctx, "SELECT status,to_address,from_address FROM mail_item WHERE id=$1", id).Scan(&status, &to, &from); err != nil || status != "queued" || to != "reports@collector.example" || from != n.box.Address {
		t.Fatal("report did not use immutable outbox", status, to, from, err)
	}
	if err := db.From(ctx).QueryRow(ctx, "SELECT count(*) FROM mail_attachment WHERE item_id=$1", id).Scan(&files); err != nil || files != 1 {
		t.Fatal("report attachment duplicated", files, err)
	}
	// Invalid aggregate input must never become arbitrary outbound relay mail.
	if w := send(postfixFixtureToken, "127.0.0.1:23", []byte("From: attacker@example.com\r\n\r\nPrivate message")); w.Code == 202 {
		t.Fatal("arbitrary message accepted as report")
	}
}
