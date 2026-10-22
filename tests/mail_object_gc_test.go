package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"github.com/google/uuid"
	"thura/app"
	q "thura/db/queries/gen"
	"thura/internal/mailbox"
	"thura/internal/platform/objectgc"
	"thura/internal/workspace"
	"thura/schema"
)

// Attachments are stored before their mail row commits; a rollback leaves the
// object behind. The sweep removes those after the cutoff and nothing else.
func TestObjectSweepRemovesOrphanedMailAttachments(t *testing.T) {
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	pool := db.From(ctx)
	stamp := time.Now().UnixNano()
	email := fmt.Sprintf("mail-gc-%d@example.com", stamp)
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", "cedar lantern river meadow 4471")
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Mail GC", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup := context.WithoutCancel(ctx)
		for _, sql := range []string{"DELETE FROM mail_attachment WHERE item_id IN (SELECT id FROM mail_item WHERE mailbox_id IN (SELECT id FROM mailbox WHERE workspace_id=$1))", "DELETE FROM mail_item WHERE mailbox_id IN (SELECT id FROM mailbox WHERE workspace_id=$1)", "DELETE FROM mailbox WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, e := pool.Exec(cleanup, sql, w.ID); e != nil {
				t.Error(e)
			}
		}
		if e := auth.From(cleanup).DeleteUser(cleanup, owner.Subject); e != nil {
			t.Error(e)
		}
	})
	box, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: w.ID, Name: "Mail", Address: fmt.Sprintf("gc-%d@example.com", stamp)})
	if err != nil {
		t.Fatal(err)
	}
	queries := q.New(pool)
	external := uuid.NewString()
	raw := "mail/" + box.ID + "/raw/" + uuid.NewString() + ".eml"
	item, err := queries.InsertInboundMail(ctx, q.InsertInboundMailParams{MailboxID: box.ID, FromAddress: "sender@example.com", ToAddress: box.Address, Subject: "Attached", RawKey: raw, ExternalID: &external})
	if err != nil {
		t.Fatal(err)
	}
	referenced := "mail/" + box.ID + "/" + item.ID + "/inbound-0"
	if _, err = queries.AddMailAttachment(ctx, q.AddMailAttachmentParams{ItemID: item.ID, Name: "a.txt", ContentType: "text/plain", Size: 4, ObjectKey: referenced}); err != nil {
		t.Fatal(err)
	}
	orphan := "mail/" + box.ID + "/" + uuid.NewString() + "/inbound-0"
	for _, key := range []string{raw, referenced, orphan} {
		if _, err = storage.From(ctx).Put(ctx, key, bytes.NewReader([]byte("data")), storage.PutOptions{}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { storage.From(context.WithoutCancel(ctx)).Delete(context.WithoutCancel(ctx), key) })
	}
	prefix := "mail/" + box.ID + "/"
	if err = objectgc.SweepPrefix(ctx, prefix, srv.Clock.Now().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.From(ctx).Stat(ctx, orphan); err != nil {
		t.Fatal("sweep deleted a recent mail object")
	}
	if err = objectgc.SweepPrefix(ctx, prefix, srv.Clock.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = storage.From(ctx).Stat(ctx, orphan); err == nil {
		t.Fatal("sweep kept an orphaned attachment")
	}
	if _, err = storage.From(ctx).Stat(ctx, referenced); err != nil {
		t.Fatal("sweep deleted a referenced attachment")
	}
	if _, err = storage.From(ctx).Stat(ctx, raw); err != nil {
		t.Fatal("sweep deleted a raw message it cannot check")
	}
	if err = objectgc.SweepPrefix(ctx, "mail/", srv.Clock.Now().Add(time.Hour)); err == nil {
		t.Fatal("sweep accepted the bare mail namespace")
	}
	if err = objectgc.SweepPrefix(ctx, prefix+"raw/", srv.Clock.Now().Add(time.Hour)); err == nil {
		t.Fatal("sweep accepted the raw message prefix")
	}
	if err = objectgc.Sweep(ctx, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
}
