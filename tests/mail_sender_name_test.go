package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	stdmail "net/mail"
	"sync"
	"testing"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"thura/app"
	"thura/internal/mailbox"
	"thura/internal/workspace"
	"thura/schema"
)

// Outgoing mail carries a display name: the mailbox's own sender name, else
// the owner's profile name (private) or the workspace name (shared), always
// as one valid RFC 5322 address encoded by net/mail.
func TestOutgoingMailSenderName(t *testing.T) {
	t.Setenv("JOBS_WORKERS", "0")
	var mu sync.Mutex
	var froms []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		froms = append(froms, r.FormValue("from"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"sender-name-fixture","message":"Queued"}`))
	}))
	defer provider.Close()
	prefix := "SENDER_NAME_"
	t.Setenv(prefix+"MAIL_PROVIDER", "mailgun")
	t.Setenv(prefix+"MAIL_DOMAIN", "example.com")
	t.Setenv(prefix+"MAIL_BASE_URL", provider.URL)
	t.Setenv(prefix+"MAIL_API_KEY", "fixture-sender-name-key")
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("sender-owner-%d@example.com", suffix)
	pw := "harbor violet compass ember 6271"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Ada Lovelace", pw)
	if err != nil {
		t.Fatal(err)
	}
	teamName := "Ada Lovelace, Nörthwind"
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: teamName, OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{
			"DELETE FROM mail_attachment WHERE item_id IN (SELECT id FROM mail_item WHERE mailbox_id IN (SELECT id FROM mailbox WHERE workspace_id=$1))",
			"DELETE FROM mail_item WHERE mailbox_id IN (SELECT id FROM mailbox WHERE workspace_id=$1)",
			"DELETE FROM mailbox WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1",
		} {
			if _, err := db.From(ctx).Exec(ctx, sql, w.ID); err != nil {
				t.Error(err)
			}
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	provision := func(label string) schema.Mailbox {
		t.Helper()
		m, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: w.ID, Name: label, Address: fmt.Sprintf("%s-%d@example.com", label, suffix), ConfigPrefix: &prefix})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	shared := provision("shared")
	private := provision("private")
	if _, err = db.From(ctx).Exec(ctx, "UPDATE mailbox SET private=true, owner_subject=$2 WHERE id=$1", private.ID, owner.Subject); err != nil {
		t.Fatal(err)
	}
	status := func(method, path string, body, out any, want int) {
		t.Helper()
		if r := srv.JSON(t, method, path, body, out); r.StatusCode != want {
			t.Fatalf("%s %s=%d want %d", method, path, r.StatusCode, want)
		}
	}
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	base := func(m schema.Mailbox) string { return "/api/v1/workspaces/" + w.ID + "/mailboxes/" + m.ID }
	// send queues and delivers one message, and returns the From the
	// provider received, checked to be one address of the mailbox.
	send := func(m schema.Mailbox) *stdmail.Address {
		t.Helper()
		to, subject, text := "recipient@example.com", "Sender name", "Body"
		var draft schema.MailItem
		status("POST", base(m)+"/messages", schema.DraftInput{To: &to, Subject: &subject, Text: &text}, &draft, 201)
		status("POST", base(m)+"/messages/"+draft.ID+"/send", schema.SendMailInput{}, &draft, 200)
		srv.Clock.Advance(11 * time.Second)
		payload, _ := json.Marshal(map[string]string{"id": draft.ID})
		mu.Lock()
		before := len(froms)
		mu.Unlock()
		if err := mailbox.Deliver(ctx, payload); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(froms) != before+1 {
			t.Fatalf("provider calls %d want %d", len(froms), before+1)
		}
		raw := froms[len(froms)-1]
		list, err := stdmail.ParseAddressList(raw)
		if err != nil || len(list) != 1 || list[0].Address != m.Address {
			t.Fatalf("From %q is not the single address %s: %v", raw, m.Address, err)
		}
		return list[0]
	}
	settings := func(m schema.Mailbox, name string, want int) schema.MailboxView {
		t.Helper()
		var out schema.MailboxView
		signature := ""
		status("PUT", base(m)+"/signature", schema.MailSignatureInput{Signature: &signature, SenderName: &name}, &out, want)
		return out
	}

	// A shared mailbox defaults to the workspace name, comma and non-ASCII
	// letters included, and never to the mailbox label.
	if got := send(shared); got.Name != teamName {
		t.Fatalf("shared default name %q want %q", got.Name, teamName)
	}
	// A private mailbox defaults to its owner's profile name.
	if got := send(private); got.Name != "Ada Lovelace" {
		t.Fatalf("private default name %q", got.Name)
	}
	// The mailbox list shows each default for the settings placeholder.
	var list schema.MailboxList
	status("GET", "/api/v1/workspaces/"+w.ID+"/mailboxes", nil, &list, 200)
	defaults := map[string]string{}
	for _, item := range list.Items {
		defaults[item.ID] = item.SenderDefault
	}
	if defaults[shared.ID] != teamName || defaults[private.ID] != "Ada Lovelace" {
		t.Fatalf("sender defaults %v", defaults)
	}
	// An explicit name wins, trimmed; quotes and commas stay one address.
	explicit := `Info "Desk", Nörthwind`
	if view := settings(shared, "  "+explicit+"  ", 200); view.SenderName != explicit || view.SenderDefault != teamName {
		t.Fatalf("saved sender name %q default %q", view.SenderName, view.SenderDefault)
	}
	if got := send(shared); got.Name != explicit {
		t.Fatalf("explicit name %q want %q", got.Name, explicit)
	}
	// A line break would split the header: refused, and the saved name stays.
	settings(shared, "Info\r\nBcc: victim@example.com", 422)
	settings(shared, "Info\nDesk", 422)
	if got := send(shared); got.Name != explicit {
		t.Fatalf("refused name changed the sender to %q", got.Name)
	}
	// Clearing the name returns to the default.
	settings(shared, "", 200)
	if got := send(shared); got.Name != teamName {
		t.Fatalf("cleared name %q want %q", got.Name, teamName)
	}
}
