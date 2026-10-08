package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"thura/internal/mailbox"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func TestPersistentMailAndConnectorRouting(t *testing.T) {
	t.Setenv("JOBS_WORKERS", "0")
	var sendsA, sendsB atomic.Int32
	transport := func(count *atomic.Int32, wantKey, wantSender string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, key, ok := r.BasicAuth()
			if !ok || user != "api" || key != wantKey {
				t.Error("wrong connector credentials")
				w.WriteHeader(401)
				return
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if r.FormValue("from") != wantSender {
				t.Error("wrong mailbox sender")
			}
			count.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"id":"provider-fixture-id","message":"Queued"}`))
		}))
	}
	a := transport(&sendsA, "fixture-key-a", "a@example.com")
	defer a.Close()
	b := transport(&sendsB, "fixture-key-b", "b@example.com")
	defer b.Close()
	// Each mailbox has an operator-controlled credential namespace.
	for prefix, config := range map[string][2]string{"ROUTE_A_": {a.URL, "fixture-key-a"}, "ROUTE_B_": {b.URL, "fixture-key-b"}} {
		t.Setenv(prefix+"MAIL_PROVIDER", "mailgun")
		t.Setenv(prefix+"MAIL_DOMAIN", "example.com")
		t.Setenv(prefix+"MAIL_BASE_URL", config[0])
		t.Setenv(prefix+"MAIL_API_KEY", config[1])
	}
	srv := lidzatest.Start(t, app())
	ctx := srv.Context()
	email := fmt.Sprintf("mail-owner-%d@example.com", time.Now().UnixNano())
	pw := "copper meadow waterfall lantern 9134"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Mail workspace", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	boxes := []schema.Mailbox{}
	for n, prefix := range []string{"ROUTE_A_", "ROUTE_B_"} {
		address := []string{"a@example.com", "b@example.com"}[n]
		m, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: w.ID, Name: prefix, Address: address, ConfigPrefix: &prefix})
		if err != nil {
			t.Fatal(err)
		}
		boxes = append(boxes, m)
	}
	t.Cleanup(func() {
		db.From(ctx).Exec(ctx, "DELETE FROM mail_attachment WHERE item_id IN (SELECT id FROM mail_item WHERE mailbox_id IN (SELECT id FROM mailbox WHERE workspace_id=$1))", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM mail_item WHERE mailbox_id IN (SELECT id FROM mailbox WHERE workspace_id=$1)", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM mailbox WHERE workspace_id=$1", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM workspace WHERE id=$1", w.ID)
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	status := func(method, path string, body, out any, want int) {
		t.Helper()
		r := srv.JSON(t, method, path, body, out)
		if r.StatusCode != want {
			t.Fatalf("%s %s=%d want %d", method, path, r.StatusCode, want)
		}
	}
	base := func(m schema.Mailbox) string {
		return "/api/v1/workspaces/" + w.ID + "/mailboxes/" + m.ID + "/messages"
	}
	status("GET", base(boxes[0]), nil, nil, 401)
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	to, subject, text := "recipient@example.com", "Persistent draft", "A real message"
	var draft schema.MailItem
	status("POST", base(boxes[0]), schema.DraftInput{To: &to, Subject: &subject, Text: &text}, &draft, 201)
	status("GET", base(boxes[1])+"/"+draft.ID, nil, nil, 404)
	status("POST", base(boxes[1])+"/"+draft.ID+"/send", schema.SendMailInput{}, nil, 404)
	payload, _ := json.Marshal(map[string]string{"id": draft.ID})
	status("POST", base(boxes[0])+"/"+draft.ID+"/send", schema.SendMailInput{}, &draft, 200)
	if draft.Status != "queued" || draft.SendAt == nil {
		t.Fatal("draft was not queued")
	}
	if err = mailbox.Deliver(ctx, payload); err == nil {
		t.Fatal("early delivery must be refused")
	}
	status("POST", base(boxes[0])+"/"+draft.ID+"/undo", nil, &draft, 200)
	if draft.Status != "draft" {
		t.Fatal("undo did not restore draft")
	}
	if err = mailbox.Deliver(ctx, payload); err != nil {
		t.Fatal(err)
	}
	if sendsA.Load() != 0 {
		t.Fatal("cancelled message was sent")
	}
	status("POST", base(boxes[0])+"/"+draft.ID+"/send", schema.SendMailInput{}, &draft, 200)
	status("POST", base(boxes[0])+"/"+draft.ID+"/send", schema.SendMailInput{}, nil, 409)
	status("PUT", base(boxes[0])+"/"+draft.ID, schema.DraftInput{To: &to, Subject: &subject, Text: &text}, nil, 409)
	srv.Clock.Advance(11 * time.Second)
	status("POST", base(boxes[0])+"/"+draft.ID+"/undo", nil, nil, 409)
	if err = mailbox.Deliver(ctx, payload); err != nil {
		t.Fatal(err)
	}
	if err = mailbox.Deliver(ctx, payload); err != nil {
		t.Fatal(err)
	}
	if sendsA.Load() != 1 || sendsB.Load() != 0 {
		t.Fatal("mailbox A routing or duplicate delivery failed")
	}
	status("POST", base(boxes[1]), schema.DraftInput{To: &to, Subject: &subject, Text: &text}, &draft, 201)
	status("POST", base(boxes[1])+"/"+draft.ID+"/send", schema.SendMailInput{}, &draft, 200)
	srv.Clock.Advance(11 * time.Second)
	payload, _ = json.Marshal(map[string]string{"id": draft.ID})
	if err = mailbox.Deliver(ctx, payload); err != nil {
		t.Fatal(err)
	}
	if sendsA.Load() != 1 || sendsB.Load() != 1 {
		t.Fatal("mailbox B routing failed")
	}
}

func TestSignedInboundMIME(t *testing.T) {
	t.Setenv("MAIL_INBOUND_SECRET", "fixture-inbound-secret-with-at-least-32-characters")
	srv := lidzatest.Start(t, app())
	ctx := srv.Context()
	email := fmt.Sprintf("inbound-owner-%d@example.com", time.Now().UnixNano())
	pw := "copper waterfall mountain lamp 8624"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Inbound", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	m, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: w.ID, Name: "Inbox", Address: "inbox@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.From(ctx).Exec(ctx, "DELETE FROM mail_attachment WHERE item_id IN (SELECT id FROM mail_item WHERE mailbox_id=$1)", m.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM mail_item WHERE mailbox_id=$1", m.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM mailbox WHERE id=$1", m.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM workspace WHERE id=$1", w.ID)
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	raw := []byte("From: Sender <sender@example.com>\r\nTo: inbox@example.com\r\nSubject: =?UTF-8?B?SGVsbG8gVGh1cmE=?=\r\nMessage-ID: <fixture@example.com>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=outer\r\n\r\n--outer\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nHello=20Thura\r\n--outer\r\nContent-Type: text/plain; name=note.txt\r\nContent-Disposition: attachment; filename=note.txt\r\nContent-Transfer-Encoding: base64\r\n\r\naGVsbG8=\r\n--outer--\r\n")
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	delivery := "fixture-inbound-delivery"
	sign := func(ts string, body []byte) string {
		h := hmac.New(sha256.New, []byte("fixture-inbound-secret-with-at-least-32-characters"))
		h.Write([]byte(ts + "." + delivery + "."))
		h.Write(body)
		return hex.EncodeToString(h.Sum(nil))
	}
	send := func(ts, sig string, body []byte, want int) schema.MailItem {
		t.Helper()
		req, err := http.NewRequest("POST", srv.URL+"/api/v1/inbound/"+m.ID, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "message/rfc822")
		req.Header.Set("X-Thura-Timestamp", ts)
		req.Header.Set("X-Thura-Delivery", delivery)
		req.Header.Set("X-Thura-Signature", sig)
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("inbound=%d want%d", res.StatusCode, want)
		}
		var i schema.MailItem
		if want == 200 {
			if err = json.NewDecoder(res.Body).Decode(&i); err != nil {
				t.Fatal(err)
			}
		}
		return i
	}
	send(stamp, "bad", raw, 401)
	stale := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	send(stale, sign(stale, raw), raw, 401)
	send(stamp, sign(stamp, raw), append(append([]byte{}, raw...), []byte("tamper")...), 401)
	item := send(stamp, sign(stamp, raw), raw, 200)
	repeat := send(stamp, sign(stamp, raw), raw, 200)
	if repeat.ID != item.ID || item.Subject != "Hello Thura" || !strings.Contains(item.TextBody, "Hello Thura") || item.RawKey == "" {
		t.Fatal("MIME decoding or duplicate handling failed")
	}
	var count int
	if err = db.From(ctx).QueryRow(ctx, "SELECT count(*) FROM mail_attachment WHERE item_id=$1", item.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("attachment was duplicated or missing")
	}
	var detail schema.MailDetail
	srv.JSON(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil)
	res := srv.JSON(t, "GET", "/api/v1/workspaces/"+w.ID+"/mailboxes/"+m.ID+"/messages/"+item.ID, nil, &detail)
	if res.StatusCode != 200 || len(detail.Attachments) != 1 {
		t.Fatal("inbound detail missing attachment")
	}
	var file schema.FileContent
	res = srv.JSON(t, "GET", "/api/v1/workspaces/"+w.ID+"/mailboxes/"+m.ID+"/messages/"+item.ID+"/attachments/"+detail.Attachments[0].ID, nil, &file)
	if res.StatusCode != 200 || file.Data != "aGVsbG8=" || file.Name != "note.txt" {
		t.Fatal("private attachment download failed")
	}
}
