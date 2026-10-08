package tests

import (
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"strings"
	"testing"
	"thura/app"
	"thura/internal/mailbox"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func TestMailOrganizationRepliesAndScopedThreads(t *testing.T) {
	t.Setenv("JOBS_WORKERS", "0")
	srv := lidzatest.Start(t, app.New(nil))
	ctx := srv.Context()
	email := fmt.Sprintf("organization-%d@example.com", time.Now().UnixNano())
	password := "river forest lantern copper 4938"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", password)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Organization workspace", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{
			"DELETE FROM mail_tag WHERE label_id IN (SELECT id FROM mail_label WHERE mailbox_id IN (SELECT id FROM mailbox WHERE workspace_id=$1))",
			"DELETE FROM mail_label WHERE mailbox_id IN (SELECT id FROM mailbox WHERE workspace_id=$1)",
			"DELETE FROM mail_item WHERE mailbox_id IN (SELECT id FROM mailbox WHERE workspace_id=$1)",
			"DELETE FROM mailbox WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1",
		} {
			if _, err := db.From(ctx).Exec(ctx, sql, w.ID); err != nil {
				t.Error(err)
			}
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	var paths []string
	for _, name := range []string{"First", "Second"} {
		box, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: w.ID, Name: name, Address: strings.ToLower(name) + "@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, "/api/v1/workspaces/"+w.ID+"/mailboxes/"+box.ID)
	}
	request := func(method, path string, input, output any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, input, output)
		if res.StatusCode != want {
			t.Fatalf("%s %s: %d want %d", method, path, res.StatusCode, want)
		}
	}
	request("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: password}, nil, 200)
	signature := "The team"
	request("PUT", paths[0]+"/signature", schema.MailSignatureInput{Signature: &signature}, nil, 200)
	var original schema.MailItem
	request("POST", paths[0]+"/messages", schema.DraftInput{}, &original, 201)
	if original.TextBody != "\n\nThe team" || original.ThreadID != original.ID {
		t.Fatal("new draft lost signature or thread root")
	}

	emptySignature := ""
	request("PUT", paths[0]+"/signature", schema.MailSignatureInput{Signature: &emptySignature}, nil, 200)
	var unsigned schema.MailItem
	request("POST", paths[0]+"/messages", schema.DraftInput{}, &unsigned, 201)
	if unsigned.TextBody != "" {
		t.Fatal("cleared signature remained in new draft")
	}
	request("PUT", paths[0]+"/signature", schema.MailSignatureInput{Signature: &signature}, nil, 200)
	_, err = db.From(ctx).Exec(ctx, "UPDATE mail_item SET status='received',folder='inbox',from_address='sender@example.com',to_address='first@example.com, other@example.com',cc='other@example.com, third@example.com',bcc='secret@example.com',message_id='<source@example.com>',subject='Hello',text_body='Original' WHERE id=$1", original.ID)
	if err != nil {
		t.Fatal(err)
	}
	all := true
	var reply schema.MailItem
	request("POST", paths[0]+"/messages", schema.DraftInput{ReplyID: &original.ID, ReplyAll: &all}, &reply, 201)
	if reply.InReplyTo != "<source@example.com>" || reply.ReferencesHeader != "<source@example.com>" || reply.ThreadID != original.ThreadID || reply.Subject != "Re: Hello" || strings.Contains(reply.ToAddress+reply.Cc, "first@") || strings.Contains(reply.ToAddress+reply.Cc, "secret@") || reply.Bcc != "" {
		t.Fatal("reply headers, recipient privacy or thread linkage wrong")
	}
	request("PUT", paths[0]+"/messages/"+reply.ID, schema.DraftInput{To: &reply.ToAddress, Text: &reply.TextBody}, &reply, 200)
	if reply.ThreadID != original.ThreadID || reply.InReplyTo != "<source@example.com>" {
		t.Fatal("saving draft cleared thread")
	}
	request("POST", paths[1]+"/messages", schema.DraftInput{ReplyID: &original.ID}, nil, 404)
	request("POST", paths[0]+"/messages", schema.DraftInput{ReplyAll: &all}, nil, 422)
	request("POST", paths[0]+"/messages", schema.DraftInput{ReplyID: &original.ID, ForwardID: &original.ID}, nil, 422)
	var page schema.MailItemList
	request("GET", paths[0]+"/messages/"+reply.ID+"/thread?limit=1", nil, &page, 200)
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatal("thread pagination missing")
	}
	request("GET", paths[0]+"/messages/"+reply.ID+"/thread?cursor="+page.NextCursor+"&limit=1", nil, &page, 200)
	if len(page.Items) != 1 || page.Items[0].ID != original.ID {
		t.Fatal("thread page lost source")
	}
	var label schema.MailLabel
	request("POST", paths[0]+"/labels", schema.MailLabelInput{Name: "Project"}, &label, 201)
	request("POST", paths[0]+"/labels", schema.MailLabelInput{Name: " project "}, nil, 409)
	request("PATCH", paths[0]+"/messages/"+original.ID+"/labels", schema.MailLabelChange{LabelID: label.ID, Applied: true}, nil, 204)
	request("PATCH", paths[0]+"/messages/"+original.ID+"/labels", schema.MailLabelChange{LabelID: label.ID, Applied: true}, nil, 204)
	var detail schema.MailDetail
	request("GET", paths[0]+"/messages/"+original.ID, nil, &detail, 200)
	if len(detail.Labels) != 1 || detail.Labels[0].ID != label.ID {
		t.Fatal("label application not idempotent")
	}
	request("GET", paths[0]+"/messages?labelId="+label.ID, nil, &page, 200)
	if len(page.Items) != 1 || page.Items[0].ID != original.ID {
		t.Fatal("label filter not applied")
	}

	request("PATCH", paths[0]+"/messages/"+original.ID+"/labels", schema.MailLabelChange{LabelID: label.ID, Applied: false}, nil, 204)
	request("GET", paths[0]+"/messages/"+original.ID, nil, &detail, 200)
	if len(detail.Labels) != 0 {
		t.Fatal("removed assignment remained on message")
	}
	request("PATCH", paths[0]+"/messages/"+original.ID+"/labels", schema.MailLabelChange{LabelID: label.ID, Applied: true}, nil, 204)
	request("GET", paths[1]+"/messages?labelId="+label.ID, nil, nil, 404)
	request("DELETE", paths[1]+"/labels/"+label.ID, nil, nil, 404)
	request("DELETE", paths[0]+"/labels/"+label.ID, nil, nil, 204)
	request("GET", paths[0]+"/messages/"+original.ID, nil, &detail, 200)
	if len(detail.Labels) != 0 {
		t.Fatal("deleted label still applied")
	}
	_, err = db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID)
	if err != nil {
		t.Fatal(err)
	}
	request("GET", paths[0]+"/messages/"+reply.ID+"/thread", nil, nil, 404)
	request("PUT", paths[0]+"/signature", schema.MailSignatureInput{Signature: &signature}, nil, 404)
}
