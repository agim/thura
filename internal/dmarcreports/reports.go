// Package dmarcreports queues aggregate reports through the configured mail
// provider, preserving the application's immutable, no-duplicate send protocol.
package dmarcreports

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"net/mail"
	"path/filepath"
	"strings"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/env"
	q "thura/db/queries/gen"
	"thura/internal/mailbox"
	"thura/internal/platform/objectstore"
	"thura/schema"
)

func Validate(raw []byte, recipients []string) (mailbox.ParsedMail, error) {
	if len(raw) > 4<<20 || len(recipients) == 0 || len(recipients) > 10 {
		return mailbox.ParsedMail{}, errors.New("invalid aggregate report limits")
	}
	for _, r := range recipients {
		a, e := mail.ParseAddress(r)
		if e != nil || a.Address != r {
			return mailbox.ParsedMail{}, errors.New("invalid aggregate report recipient")
		}
	}
	parsed, err := mailbox.Parse(raw)
	if err != nil || len(parsed.Attachments) != 1 || !strings.HasPrefix(strings.ToLower(parsed.Subject), "report domain:") {
		return parsed, errors.New("expected one aggregate DMARC report attachment")
	}
	file := parsed.Attachments[0]
	if len(file.Data) > 2<<20 || filepath.Base(file.Name) != file.Name {
		return parsed, errors.New("invalid aggregate report attachment")
	}
	var stream io.Reader = bytes.NewReader(file.Data)
	if strings.HasSuffix(strings.ToLower(file.Name), ".gz") {
		reader, e := gzip.NewReader(stream)
		if e != nil {
			return parsed, e
		}
		defer reader.Close()
		stream = reader
	} else if !strings.HasSuffix(strings.ToLower(file.Name), ".xml") {
		return parsed, errors.New("aggregate report must be XML or gzipped XML")
	}
	content, err := io.ReadAll(io.LimitReader(stream, (4<<20)+1))
	if err != nil || len(content) > 4<<20 {
		return parsed, errors.New("aggregate report expansion limit exceeded")
	}
	var feedback struct {
		XMLName xml.Name `xml:"feedback"`
		ID      string   `xml:"report_metadata>report_id"`
		Domain  string   `xml:"policy_published>domain"`
		Records []struct {
			IP string `xml:"row>source_ip"`
		} `xml:"record"`
	}
	if xml.Unmarshal(content, &feedback) != nil || feedback.ID == "" || feedback.Domain == "" || len(feedback.Records) == 0 {
		return parsed, errors.New("invalid aggregate report XML")
	}
	return parsed, nil
}

func Queue(ctx context.Context, raw []byte, recipients []string) (string, error) {
	parsed, err := Validate(raw, recipients)
	if err != nil {
		return "", err
	}
	values, err := env.Values(".")
	if err != nil {
		return "", err
	}
	address := values["THURA_MAIL_ADDRESS"]
	if address == "" {
		a, e := mail.ParseAddress(values["MAIL_FROM"])
		if e != nil {
			return "", e
		}
		address = a.Address
	}
	queries := q.New(db.From(ctx))
	setup, err := queries.GetServerSetupRouting(ctx)
	if err != nil || setup.PublishedRevision == 0 {
		return "", errors.New("report sending requires activated setup")
	}
	boxes, err := queries.ListMailboxesByAddress(ctx, address)
	if err != nil || len(boxes) != 1 {
		return "", errors.New("configure a unique operator report mailbox")
	}
	box := boxes[0]
	ctx = auth.WithUser(ctx, &auth.User{ID: setup.Subject})
	if _, err = mailbox.Access(ctx, box.WorkspaceID, box.ID); err != nil {
		return "", err
	}
	digest := sha256.Sum256(append(append([]byte(strings.Join(recipients, ",")), 0), raw...))
	identity := "dmarc:" + hex.EncodeToString(digest[:])
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	queries = q.New(tx)
	if _, err = queries.LockMailbox(ctx, box.ID); err != nil {
		return "", err
	}
	item, err := queries.CreateDMARCReportDraft(ctx, q.CreateDMARCReportDraftParams{MailboxID: box.ID, AuthorID: setup.Subject, FromAddress: box.Address, ToAddress: strings.Join(recipients, ","), Subject: parsed.Subject, TextBody: parsed.Text, ExternalID: &identity})
	if err != nil {
		return "", err
	}
	if item.Status == "draft" {
		files, e := queries.ListMailAttachments(ctx, item.ID)
		if e != nil {
			return "", e
		}
		if len(files) == 0 {
			file := parsed.Attachments[0]
			key := "mail/" + box.ID + "/" + item.ID + "/aggregate-report"
			if _, err = objectstore.Put(ctx, key, bytes.NewReader(file.Data), storage.PutOptions{ContentType: file.ContentType}); err != nil {
				return "", err
			}
			if _, err = queries.AddMailAttachment(ctx, q.AddMailAttachmentParams{ItemID: item.ID, Name: file.Name, ContentType: file.ContentType, Size: int32(len(file.Data)), ObjectKey: key}); err != nil {
				return "", err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	if item.Status != "draft" {
		return item.ID, nil
	}
	if _, err = mailbox.Queue(ctx, box.WorkspaceID, box.ID, item.ID, schema.SendMailInput{}); err != nil {
		existing, e := q.New(db.From(ctx)).GetMailItem(ctx, item.ID)
		if e == nil && existing.Status != "draft" {
			return item.ID, nil
		}
		return "", err
	}
	return item.ID, nil
}
