package tests

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/lidzatest"
	"thura/app"
	"thura/internal/mailbox"
	"thura/internal/workspace"
	"thura/schema"
)

func TestMailReaderSanitizationAndScopedCIDImages(t *testing.T) {
	const secret = "synthetic-reader-secret-at-least-32-characters"
	t.Setenv("MAIL_INBOUND_SECRET", secret)
	srv := lidzatest.Start(t, app.New(nil))
	ctx := srv.Context()
	email := fmt.Sprintf("reader-%d@example.com", time.Now().UnixNano())
	pw := "copper waterfall mountain lamp 8624"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Reader", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	m, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: w.ID, Name: "Inbox", Address: "reader@local.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM mail_attachment WHERE item_id IN (SELECT id FROM mail_item WHERE mailbox_id=$1)", "DELETE FROM mail_item WHERE mailbox_id=$1", "DELETE FROM mailbox WHERE id=$1"} {
			if _, e := db.From(ctx).Exec(ctx, sql, m.ID); e != nil {
				t.Error(e)
			}
		}
		db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM workspace WHERE id=$1", w.ID)
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	var original bytes.Buffer
	if err = png.Encode(&original, image.NewNRGBA(image.Rect(0, 0, 512, 256))); err != nil {
		t.Fatal(err)
	}
	body := `<p onclick="alert(1)" style="background:url(https://tracking.invalid)">Safe reader body</p><script>alert(1)</script><img src="cid:logo@local"><img src="https://tracking.invalid"><a href="https://tracking.invalid">Link text</a>`
	raw := []byte("From: sender@local.invalid\r\nTo: reader@local.invalid\r\nSubject: Reader fixture\r\nMIME-Version: 1.0\r\nContent-Type: multipart/related; boundary=reader\r\n\r\n--reader\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" + body + "\r\n--reader\r\nContent-Type: image/png; name=logo.png\r\nContent-ID: <logo@local>\r\nContent-Transfer-Encoding: base64\r\n\r\n" + base64.StdEncoding.EncodeToString(original.Bytes()) + "\r\n--reader\r\nContent-Type: image/svg+xml; name=active.svg\r\nContent-ID: <active@local>\r\n\r\n<svg onload=\"alert(1)\"/>\r\n--reader--\r\n")
	stamp := strconv.FormatInt(srv.Clock.Now().Unix(), 10)
	sign := hmac.New(sha256.New, []byte(secret))
	sign.Write([]byte(stamp + ".reader-fixture."))
	sign.Write(raw)
	item, err := mailbox.Receive(ctx, m.ID, stamp, "reader-fixture", hex.EncodeToString(sign.Sum(nil)), raw)
	if err != nil {
		t.Fatal(err)
	}
	status := func(method, path string, in, out any, want int) {
		t.Helper()
		if res := srv.JSON(t, method, path, in, out); res.StatusCode != want {
			t.Fatalf("%s %s: %d want %d", method, path, res.StatusCode, want)
		}
	}
	base := "/api/v1/workspaces/" + w.ID + "/mailboxes/" + m.ID + "/messages"
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	var detail schema.MailDetail
	status("GET", base+"/"+item.ID, nil, &detail, 200)
	if len(detail.Attachments) != 2 || detail.Attachments[0].ContentID == "" {
		t.Fatal("CID metadata lost")
	}
	for _, unsafe := range []string{"<script", "onclick", "tracking.invalid", "style=", "href="} {
		if strings.Contains(detail.Item.HTMLBody, unsafe) {
			t.Fatalf("API exposed unsafe HTML: %s", detail.Item.HTMLBody)
		}
	}
	var list schema.MailItemList
	status("GET", base, nil, &list, 200)
	if len(list.Items) != 1 || list.Items[0].HTMLBody != detail.Item.HTMLBody {
		t.Fatal("list and detail have different reader policy")
	}
	if !strings.Contains(item.HTMLBody, "<script>") {
		t.Fatal("normalization unexpectedly changed stored original")
	}
	stream, _, err := storage.From(ctx).Get(ctx, item.RawKey)
	if err != nil {
		t.Fatal(err)
	}
	rawRead, err := io.ReadAll(stream)
	stream.Close()
	if err != nil || !bytes.Equal(rawRead, raw) {
		t.Fatal("stored raw MIME changed")
	}
	var imageID, svgID string
	for _, a := range detail.Attachments {
		if a.ContentID == "logo@local" {
			imageID = a.ID
		} else if a.ContentID == "active@local" {
			svgID = a.ID
		}
	}
	if imageID == "" || svgID == "" {
		t.Fatal("missing MIME CID attachments")
	}
	path := base + "/" + item.ID + "/attachments/" + imageID
	var content schema.FileContent
	status("GET", path+"/inline", nil, &content, 200)
	data, err := base64.StdEncoding.DecodeString(content.Data)
	if err != nil {
		t.Fatal(err)
	}
	image, err := png.Decode(bytes.NewReader(data))
	if err != nil || image.Bounds().Dx() != 256 || image.Bounds().Dy() != 128 || content.ContentType != "image/png" {
		t.Fatalf("unsafe or unbounded inline image: %v", err)
	}
	status("GET", path, nil, &content, 200)
	if content.Data != base64.StdEncoding.EncodeToString(original.Bytes()) {
		t.Fatal("reader changed original attachment bytes")
	}
	status("GET", base+"/"+item.ID+"/attachments/"+svgID+"/inline", nil, nil, 422)
	var draft schema.MailItem
	status("POST", base, schema.DraftInput{}, &draft, 201)
	status("GET", base+"/"+draft.ID+"/attachments/"+imageID+"/inline", nil, nil, 404)
	status("GET", "/api/v1/workspaces/00000000-0000-4000-8000-000000000000/mailboxes/"+m.ID+"/messages/"+item.ID+"/attachments/"+imageID+"/inline", nil, nil, 404)
	if _, err = db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1 AND subject=$2", w.ID, owner.Subject); err != nil {
		t.Fatal(err)
	}
	status("GET", path+"/inline", nil, nil, 404)
	status("POST", "/api/v1/auth/logout", nil, nil, 204)
	status("GET", path+"/inline", nil, nil, 401)
}
