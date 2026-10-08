package mailbox

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"golang.org/x/net/html/charset"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	stdmail "net/mail"
	"net/textproto"
	"strconv"
	"strings"
	queries "thura/db/queries/gen"
	"time"
)

type ParsedAttachment struct {
	Name, ContentType, ContentID string
	Data                         []byte
}
type ParsedMail struct {
	From, To, Cc, Subject, Text, HTML, Thread, MessageID, InReplyTo, References string
	Attachments                                                                 []ParsedAttachment
}

func Parse(raw []byte) (ParsedMail, error) {
	m, err := stdmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return ParsedMail{}, err
	}
	decoder := &mime.WordDecoder{CharsetReader: func(label string, r io.Reader) (io.Reader, error) { return charset.NewReaderLabel(label, r) }}
	subject, err := decoder.DecodeHeader(m.Header.Get("Subject"))
	if err != nil {
		return ParsedMail{}, err
	}
	out := ParsedMail{From: m.Header.Get("From"), To: m.Header.Get("To"), Cc: m.Header.Get("Cc"), Subject: subject,
		MessageID: firstMessageID(m.Header.Get("Message-ID")), InReplyTo: firstMessageID(m.Header.Get("In-Reply-To")), References: cleanReferences(m.Header.Get("References"))}
	out.Thread = firstMessageID(out.References)
	if out.Thread == "" {
		out.Thread = out.InReplyTo
	}
	if out.Thread == "" {
		out.Thread = out.MessageID
	}
	parts := 0
	var walk func(map[string][]string, io.Reader, int) error
	walk = func(header map[string][]string, r io.Reader, depth int) error {
		parts++
		if depth > 6 || parts > 50 {
			return errors.New("MIME nesting or part count exceeds limit")
		}
		get := func(key string) string { return textproto.MIMEHeader(header).Get(key) }
		ct := get("Content-Type")
		if ct == "" {
			ct = "text/plain"
		}
		media, params, err := mime.ParseMediaType(ct)
		if err != nil {
			return err
		}
		switch strings.ToLower(get("Content-Transfer-Encoding")) {
		case "base64":
			r = base64.NewDecoder(base64.StdEncoding, r)
		case "quoted-printable":
			r = quotedprintable.NewReader(r)
		}
		if strings.HasPrefix(media, "multipart/") {
			if params["boundary"] == "" {
				return errors.New("multipart boundary missing")
			}
			reader := multipart.NewReader(r, params["boundary"])
			for {
				part, err := reader.NextPart()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					return err
				}
				parseErr := walk(part.Header, part, depth+1)
				closeErr := part.Close()
				if err = errors.Join(parseErr, closeErr); err != nil {
					return err
				}
			}
			return nil
		}
		disposition, dp, err := mime.ParseMediaType(get("Content-Disposition"))
		if err != nil && get("Content-Disposition") != "" {
			return err
		}
		filename := dp["filename"]
		if filename == "" {
			filename = params["name"]
		}
		if disposition == "attachment" || filename != "" || media != "text/plain" && media != "text/html" {
			if len(out.Attachments) >= 10 {
				return errors.New("too many MIME attachments")
			}
			b, err := io.ReadAll(io.LimitReader(r, (10<<20)+1))
			if err != nil {
				return err
			}
			if len(b) > 10<<20 {
				return errors.New("MIME attachment too large")
			}
			if filename == "" {
				filename = "attachment"
			}
			out.Attachments = append(out.Attachments, ParsedAttachment{Name: safeName(filename), ContentType: media, ContentID: strings.Trim(get("Content-ID"), "<>"), Data: b})
			return nil
		}
		if label := params["charset"]; label != "" {
			r, err = charset.NewReaderLabel(label, r)
			if err != nil {
				return err
			}
		}
		b, err := io.ReadAll(io.LimitReader(r, 500001))
		if err != nil {
			return err
		}
		if len(b) > 500000 {
			return errors.New("MIME text part too large")
		}
		if media == "text/html" {
			out.HTML = string(b)
		} else {
			out.Text = string(b)
		}
		return nil
	}
	if err = walk(m.Header, m.Body, 0); err != nil {
		return ParsedMail{}, err
	}
	return out, nil
}

// Receive is the boundary for an SMTP edge or verified hosted-provider relay.
// Its HMAC binds timestamp, delivery id, and the complete raw MIME body.
func Receive(ctx context.Context, mailboxID, timestamp, deliveryID, signature string, raw []byte) (queries.MailItem, error) {
	q := queries.New(db.From(ctx))
	m, err := q.GetMailbox(ctx, mailboxID)
	if err != nil {
		return queries.MailItem{}, router.Errorf(404, "mailbox not found")
	}
	if len(deliveryID) < 1 || len(deliveryID) > 200 {
		return queries.MailItem{}, router.Errorf(400, "delivery id required")
	}
	values, err := env.Values(".")
	if err != nil {
		return queries.MailItem{}, err
	}
	secret := values[m.ConfigPrefix+"MAIL_INBOUND_SECRET"]
	if len(secret) < 32 {
		return queries.MailItem{}, router.Errorf(503, "inbound connector is not configured")
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return queries.MailItem{}, router.Errorf(401, "invalid relay signature")
	}
	age := lidza.Now(ctx).Sub(time.Unix(seconds, 0))
	if age > 5*time.Minute || age < -5*time.Minute {
		return queries.MailItem{}, router.Errorf(401, "relay delivery outside replay window")
	}
	expected := hmac.New(sha256.New, []byte(secret))
	expected.Write([]byte(timestamp + "." + deliveryID + "."))
	expected.Write(raw)
	supplied, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal(supplied, expected.Sum(nil)) {
		return queries.MailItem{}, router.Errorf(401, "invalid relay signature")
	}
	parsed, err := Parse(raw)
	if err != nil {
		return queries.MailItem{}, router.Errorf(400, "invalid MIME message")
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return queries.MailItem{}, err
	}
	defer tx.Rollback(ctx)
	tq := queries.New(tx)
	if _, err = tq.LockMailbox(ctx, m.ID); err != nil {
		return queries.MailItem{}, err
	}
	existing, err := tq.FindInboundMail(ctx, queries.FindInboundMailParams{MailboxID: m.ID, ExternalID: &deliveryID})
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return queries.MailItem{}, err
	}
	if parsed.InReplyTo != "" {
		parent, parentErr := tq.FindMailThreadParent(ctx, queries.FindMailThreadParentParams{MailboxID: m.ID, MessageID: parsed.InReplyTo})
		if parentErr == nil && parent.ThreadID != "" {
			parsed.Thread = parent.ThreadID
		}
		if parentErr != nil && !errors.Is(parentErr, pgx.ErrNoRows) {
			return queries.MailItem{}, parentErr
		}
	}
	digest := sha256.Sum256(raw)
	rawKey := "mail/" + m.ID + "/raw/" + hex.EncodeToString(digest[:]) + ".eml"
	if _, err = storage.From(ctx).Put(ctx, rawKey, bytes.NewReader(raw), storage.PutOptions{ContentType: "message/rfc822"}); err != nil {
		return queries.MailItem{}, err
	}
	item, err := tq.InsertInboundMail(ctx, queries.InsertInboundMailParams{MailboxID: m.ID, FromAddress: parsed.From, ToAddress: parsed.To, Cc: parsed.Cc, Subject: parsed.Subject, TextBody: parsed.Text, HTMLBody: parsed.HTML, RawKey: rawKey, ExternalID: &deliveryID, ThreadID: parsed.Thread, MessageID: parsed.MessageID, InReplyTo: parsed.InReplyTo, ReferencesHeader: parsed.References})
	if err != nil {
		return queries.MailItem{}, err
	}
	for n, a := range parsed.Attachments {
		key := "mail/" + m.ID + "/" + item.ID + "/inbound-" + strconv.Itoa(n)
		if _, err = storage.From(ctx).Put(ctx, key, bytes.NewReader(a.Data), storage.PutOptions{ContentType: a.ContentType}); err != nil {
			return queries.MailItem{}, err
		}
		if _, err = tq.AddMailAttachment(ctx, queries.AddMailAttachmentParams{ItemID: item.ID, Name: a.Name, ContentType: a.ContentType, Size: int32(len(a.Data)), ObjectKey: key, ContentID: a.ContentID}); err != nil {
			return queries.MailItem{}, err
		}
	}
	return item, tx.Commit(ctx)
}
