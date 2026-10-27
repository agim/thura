package tests

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"thura/app"
	"thura/internal/mailbox"
	"thura/internal/workspace"
	"thura/schema"
	"thura/tests/testsupport"
)

func TestSMTPEnvelopeSignatureCannotBeRemovedOrForged(t *testing.T) {
	const secret = "PUBLIC-synthetic-SMTP-envelope-secret-1234567890"
	t.Setenv("MAIL_INBOUND_SECRET", secret)
	srv := testsupport.StartConfigured(t, app.New(nil))
	ctx := srv.Context()
	email := fmt.Sprintf("smtp-evidence-%d@example.com", time.Now().UnixNano())
	user, err := auth.From(ctx).CreateUser(ctx, email, "Owner", "public synthetic evidence phrase 918273")
	if err != nil {
		t.Fatal(err)
	}
	space, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "SMTP envelope", OwnerEmail: user.Email})
	if err != nil {
		t.Fatal(err)
	}
	box, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: space.ID, Name: "Inbox", Address: email})
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("From: attacker@claimed.example\r\nTo: " + email + "\r\nAuthentication-Results: fake; dmarc=pass\r\nSubject: Original signed SMTP envelope\r\n\r\nUnchanged original bytes\r\n")
	stamp := strconv.FormatInt(srv.Clock.Now().Unix(), 10)
	envelope := base64.RawURLEncoding.EncodeToString([]byte(`{"ip":"203.0.113.9","from":"bounce@real.example","helo":"mail.real.example"}`))
	sign := hmac.New(sha256.New, []byte(secret))
	sign.Write([]byte("smtp2." + stamp + ".smtp-immutable." + envelope + "."))
	sign.Write(raw)
	signature := hex.EncodeToString(sign.Sum(nil))
	item, err := mailbox.ReceiveSMTP(ctx, box.ID, stamp, "smtp-immutable", signature, raw, envelope)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err = db.From(ctx).QueryRow(ctx, "SELECT smtp_envelope FROM mail_item WHERE id=$1", item.ID).Scan(&stored); err != nil || stored != envelope {
		t.Fatal("original envelope was not persisted", err)
	}
	stream, _, err := storage.From(ctx).Get(ctx, item.RawKey)
	if err != nil {
		t.Fatal(err)
	}
	original, err := io.ReadAll(stream)
	stream.Close()
	if err != nil || !bytes.Equal(original, raw) {
		t.Fatal("original MIME bytes changed", err)
	}
	if _, err = mailbox.ReceiveSMTP(ctx, box.ID, stamp, "smtp-immutable", signature, raw, ""); err == nil {
		t.Fatal("removing signed metadata bypassed verification")
	}
	if _, err = mailbox.ReceiveSMTP(ctx, box.ID, stamp, "smtp-immutable.smtp2."+envelope, signature, raw, ""); err == nil {
		t.Fatal("moving signed envelope into delivery ID downgraded the protocol")
	}
	other := base64.RawURLEncoding.EncodeToString([]byte(`{"ip":"127.0.0.1","from":"bounce@real.example","helo":"mail.real.example"}`))
	if _, err = mailbox.ReceiveSMTP(ctx, box.ID, stamp, "smtp-immutable", signature, raw, other); err == nil {
		t.Fatal("client IP tampering bypassed verification")
	}
	again, err := mailbox.ReceiveSMTP(ctx, box.ID, stamp, "smtp-immutable", signature, raw, envelope)
	if err != nil || again.ID != item.ID {
		t.Fatal("relay retry duplicated delivery", err)
	}
	legacy := hmac.New(sha256.New, []byte(secret))
	legacy.Write([]byte(stamp + ".smtp-legacy."))
	legacy.Write(raw)
	old, err := mailbox.Receive(ctx, box.ID, stamp, "smtp-legacy", hex.EncodeToString(legacy.Sum(nil)), raw)
	if err != nil {
		t.Fatal(err)
	}
	if old.SmtpEnvelope != "" {
		t.Fatal("forged Authentication-Results became SMTP evidence")
	}
}
