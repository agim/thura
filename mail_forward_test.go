package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"thura/internal/mailbox"
	"thura/internal/workspace"
	"thura/schema"
)

func TestForwardAttachmentsAreAtomicAndMailboxScoped(t *testing.T) {
	t.Setenv("JOBS_WORKERS", "0")
	srv := lidzatest.Start(t, app())
	ctx := srv.Context()
	email := fmt.Sprintf("forward-%d@example.com", time.Now().UnixNano())
	password := "river forest lantern copper 4938"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", password)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Forward workspace", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.From(ctx).Exec(ctx, "DELETE FROM mail_attachment WHERE item_id IN (SELECT id FROM mail_item WHERE mailbox_id IN (SELECT id FROM mailbox WHERE workspace_id=$1))", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM mail_item WHERE mailbox_id IN (SELECT id FROM mailbox WHERE workspace_id=$1)", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM mailbox WHERE workspace_id=$1", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM workspace WHERE id=$1", w.ID)
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	var paths []string
	for _, name := range []string{"First", "Second"} {
		box, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: w.ID, Name: name, Address: name + "@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, "/api/v1/workspaces/"+w.ID+"/mailboxes/"+box.ID+"/messages")
	}
	request := func(method, path string, input, output any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, input, output)
		if res.StatusCode != want {
			t.Fatalf("%s %s: %d want %d", method, path, res.StatusCode, want)
		}
	}
	request("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: password}, nil, 200)
	var original schema.MailItem
	request("POST", paths[0], schema.DraftInput{}, &original, 201)
	data := []byte("Forward these original attachment bytes\n")
	req, err := http.NewRequest("PUT", srv.URL+paths[0]+"/"+original.ID+"/attachments", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Content-Disposition", `attachment; filename="forward.txt"`)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 201 && res.StatusCode != 200 {
		t.Fatalf("upload=%d", res.StatusCode)
	}
	var source schema.MailDetail
	request("GET", paths[0]+"/"+original.ID, nil, &source, 200)
	if len(source.Attachments) != 1 {
		t.Fatal("missing source attachment")
	}
	var forwarded schema.MailItem
	request("POST", paths[0], schema.DraftInput{ForwardID: &original.ID}, &forwarded, 201)
	var detail schema.MailDetail
	request("GET", paths[0]+"/"+forwarded.ID, nil, &detail, 200)
	if len(detail.Attachments) != 1 || detail.Attachments[0].ID == source.Attachments[0].ID || detail.Attachments[0].Size != len(data) {
		t.Fatal("forwarded metadata is missing or aliases the source row")
	}
	var download schema.FileContent
	request("GET", paths[0]+"/"+forwarded.ID+"/attachments/"+detail.Attachments[0].ID, nil, &download, 200)
	decoded, err := base64.StdEncoding.DecodeString(download.Data)
	if err != nil || !bytes.Equal(decoded, data) {
		t.Fatal("forwarded bytes changed")
	}
	request("GET", paths[0]+"/"+forwarded.ID+"/attachments/"+source.Attachments[0].ID, nil, nil, 404)
	request("PUT", paths[0]+"/"+forwarded.ID, schema.DraftInput{ForwardID: &original.ID}, nil, 422)
	request("POST", paths[1], schema.DraftInput{ForwardID: &original.ID}, nil, 404)
	missing := "00000000-0000-4000-8000-000000000099"
	request("POST", paths[0], schema.DraftInput{ForwardID: &missing}, nil, 404)
	var before, after int
	if err := db.From(ctx).QueryRow(ctx, "SELECT count(*) FROM mail_item WHERE mailbox_id=$1", original.MailboxID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := db.From(ctx).Exec(ctx, "UPDATE mail_attachment SET size=$1 WHERE item_id=$2", (10<<20)+1, original.ID); err != nil {
		t.Fatal(err)
	}
	request("POST", paths[0], schema.DraftInput{ForwardID: &original.ID}, nil, 413)
	if err := db.From(ctx).QueryRow(ctx, "SELECT count(*) FROM mail_item WHERE mailbox_id=$1", original.MailboxID).Scan(&after); err != nil || after != before {
		t.Fatal("rejected forward left an orphan draft")
	}
	if _, err := db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID); err != nil {
		t.Fatal(err)
	}
	request("POST", paths[0], schema.DraftInput{ForwardID: &original.ID}, nil, 404)
	request("GET", paths[0]+"/"+forwarded.ID+"/attachments/"+detail.Attachments[0].ID, nil, nil, 404)
}
