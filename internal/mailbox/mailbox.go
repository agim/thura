package mailbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/packs/mail"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	stdmail "net/mail"
	"regexp"
	"strings"
	queries "thura/db/queries/gen"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

const DeliveryJob = "thura.mail.deliver"

var prefixPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,98}_$`)

func Provision(ctx context.Context, in schema.ProvisionMailboxInput) (schema.Mailbox, error) {
	prefix := Text(in.ConfigPrefix)
	if prefix != "" && !prefixPattern.MatchString(prefix) {
		return schema.Mailbox{}, router.Errorf(422, "configPrefix must be uppercase and end in an underscore")
	}
	q := queries.New(db.From(ctx))
	if _, err := q.LockWorkspace(ctx, in.WorkspaceID); err != nil {
		return schema.Mailbox{}, err
	}
	m, err := q.CreateMailbox(ctx, queries.CreateMailboxParams{WorkspaceID: in.WorkspaceID, Name: strings.TrimSpace(in.Name), Address: strings.ToLower(in.Address), ConfigPrefix: prefix})
	return ViewMailbox(m), err
}

func Access(ctx context.Context, workspaceID, mailboxID string) (queries.Mailbox, error) {
	if err := workspace.RequireMember(ctx, workspaceID); err != nil {
		return queries.Mailbox{}, err
	}
	if !workspace.ValidID(mailboxID) {
		return queries.Mailbox{}, router.Errorf(404, "mailbox not found")
	}
	m, err := queries.New(db.From(ctx)).GetMailbox(ctx, mailboxID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && m.WorkspaceID != workspaceID) {
		return m, router.Errorf(404, "mailbox not found")
	}
	return m, err
}
func ItemAccess(ctx context.Context, workspaceID, mailboxID, itemID string) (queries.MailItem, error) {
	if _, err := Access(ctx, workspaceID, mailboxID); err != nil {
		return queries.MailItem{}, err
	}
	if !workspace.ValidID(itemID) {
		return queries.MailItem{}, router.Errorf(404, "message not found")
	}
	item, err := queries.New(db.From(ctx)).GetMailItem(ctx, itemID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && item.MailboxID != mailboxID) {
		return item, router.Errorf(404, "message not found")
	}
	return item, err
}
func Text(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func ViewMailbox(m queries.Mailbox) schema.Mailbox {
	return schema.Mailbox{ID: m.ID, WorkspaceID: m.WorkspaceID, Name: m.Name, Address: m.Address, ConfigPrefix: m.ConfigPrefix, CreatedAt: m.CreatedAt}
}
func View(i queries.MailItem) schema.MailItem {
	return schema.MailItem{ID: i.ID, MailboxID: i.MailboxID, AuthorID: i.AuthorID, Folder: schema.MailFolder(i.Folder), FromAddress: i.FromAddress, ToAddress: i.ToAddress, Cc: i.Cc, Bcc: i.Bcc, Subject: i.Subject, TextBody: i.TextBody, HTMLBody: i.HTMLBody, Status: i.Status, Starred: i.Starred, Unread: i.Unread, ProviderID: i.ProviderID, RawKey: i.RawKey, ExternalID: i.ExternalID, ThreadID: i.ThreadID, SendAt: i.SendAt, CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt}
}
func Attachment(a queries.MailAttachment) schema.AttachmentView {
	return schema.AttachmentView{ID: a.ID, Name: a.Name, ContentType: a.ContentType, Size: int(a.Size)}
}

func Config(m queries.Mailbox) (mail.Config, error) {
	values, err := env.Values(".")
	if err != nil {
		return mail.Config{}, err
	}
	filtered := map[string]string{}
	for key, value := range values {
		if m.ConfigPrefix == "" {
			if strings.HasPrefix(key, "MAIL_") {
				filtered[key] = value
			}
		} else if strings.HasPrefix(key, m.ConfigPrefix) {
			filtered[strings.TrimPrefix(key, m.ConfigPrefix)] = value
		}
	}
	var cfg mail.Config
	if err = env.Fill(&cfg, filtered); err != nil {
		return cfg, err
	}
	cfg.From = m.Address
	return cfg, nil
}
func addresses(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := stdmail.ParseAddressList(value)
	if err != nil {
		return nil, router.Errorf(422, "invalid recipient address")
	}
	out := make([]string, 0, len(parsed))
	for _, a := range parsed {
		out = append(out, a.String())
	}
	return out, nil
}

func Queue(ctx context.Context, workspaceID, mailboxID, itemID string, in schema.SendMailInput) (schema.MailItem, error) {
	tx, q, i, err := Lock(ctx, workspaceID, mailboxID, itemID)
	if err != nil {
		return schema.MailItem{}, err
	}
	defer tx.Rollback(ctx)
	if i.Status != "draft" || i.Folder != queries.MailFolderDrafts {
		return schema.MailItem{}, router.Errorf(409, "only a draft can be sent")
	}
	to, err := addresses(i.ToAddress)
	if err != nil {
		return schema.MailItem{}, err
	}
	cc, err := addresses(i.Cc)
	if err != nil {
		return schema.MailItem{}, err
	}
	bcc, err := addresses(i.Bcc)
	if err != nil {
		return schema.MailItem{}, err
	}
	if len(to)+len(cc)+len(bcc) == 0 || len(to)+len(cc)+len(bcc) > 50 {
		return schema.MailItem{}, router.Errorf(422, "provide between one and 50 recipients")
	}
	if strings.TrimSpace(i.Subject) == "" {
		return schema.MailItem{}, router.Errorf(422, "subject is required")
	}
	when := lidza.Now(ctx).Add(10 * time.Second)
	if in.SendAt != nil {
		if in.SendAt.Before(when) || in.SendAt.After(lidza.Now(ctx).Add(365*24*time.Hour)) {
			return schema.MailItem{}, router.Errorf(422, "schedule must be between ten seconds and one year from now")
		}
		when = *in.SendAt
	}
	i, err = q.QueueMailItem(ctx, queries.QueueMailItemParams{ID: itemID, SendAt: &when})
	if err != nil {
		return schema.MailItem{}, err
	}
	if _, err = jobs.From(ctx).EnqueueTx(ctx, tx, DeliveryJob, map[string]string{"id": itemID}, jobs.RunAt(when)); err != nil {
		return schema.MailItem{}, err
	}
	return View(i), tx.Commit(ctx)
}
func Cancel(ctx context.Context, workspaceID, mailboxID, itemID string) (schema.MailItem, error) {
	tx, q, i, err := Lock(ctx, workspaceID, mailboxID, itemID)
	if err != nil {
		return schema.MailItem{}, err
	}
	defer tx.Rollback(ctx)
	if i.Status != "queued" || i.SendAt == nil || !i.SendAt.After(lidza.Now(ctx)) {
		return schema.MailItem{}, router.Errorf(409, "undo window has closed")
	}
	i, err = q.CancelMailItem(ctx, itemID)
	if err != nil {
		return schema.MailItem{}, err
	}
	return View(i), tx.Commit(ctx)
}

// No queue is passed to each mailbox's Mail instance: the application's job
// chooses the connector without replacing the pack's transactional job handler.
func Deliver(ctx context.Context, payload json.RawMessage) error {
	var data struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		return err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	i, err := q.LockMailItem(ctx, data.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if i.Status != "queued" && i.Status != "failed" {
		return nil
	}
	if i.SendAt != nil && i.SendAt.After(lidza.Now(ctx)) {
		return errors.New("delivery is not due yet")
	}
	m, err := q.GetMailbox(ctx, i.MailboxID)
	if err != nil {
		return err
	}
	cfg, err := Config(m)
	if err != nil {
		return err
	}
	transport, err := mail.New(cfg, nil, nil)
	if err != nil {
		return err
	}
	cc, err := addresses(i.Cc)
	if err != nil {
		return err
	}
	bcc, err := addresses(i.Bcc)
	if err != nil {
		return err
	}
	attachments, err := q.ListMailAttachments(ctx, i.ID)
	if err != nil {
		return err
	}
	msg := mail.Message{To: i.ToAddress, Cc: cc, Bcc: bcc, From: m.Address, Subject: i.Subject, Text: i.TextBody, HTML: i.HTMLBody}
	var total int64
	for _, a := range attachments {
		r, _, err := storage.From(ctx).Get(ctx, a.ObjectKey)
		if err != nil {
			return err
		}
		b, readErr := io.ReadAll(io.LimitReader(r, (10<<20)+1))
		closeErr := r.Close()
		if err = errors.Join(readErr, closeErr); err != nil {
			return err
		}
		total += int64(len(b))
		if total > 10<<20 {
			return errors.New("attachments exceed ten megabytes")
		}
		msg.Attachments = append(msg.Attachments, mail.Attachment{Name: a.Name, ContentType: a.ContentType, Data: b, ContentID: a.ContentID})
	}
	providerID, deliveryErr := transport.Send(ctx, msg)
	status, folder := "sent", queries.MailFolderSent
	if deliveryErr != nil {
		status = "failed"
		folder = i.Folder
	} else if cfg.Provider == "outbox" || cfg.Provider == "log" {
		status = "captured"
	}
	if err = q.SetMailOutcome(ctx, queries.SetMailOutcomeParams{ID: i.ID, Status: status, ProviderID: providerID, Folder: folder}); err != nil {
		return errors.Join(deliveryErr, err)
	}
	return errors.Join(deliveryErr, tx.Commit(ctx))
}
func CheckEditable(i queries.MailItem) error {
	if i.Status != "draft" {
		return router.Errorf(http.StatusConflict, "message is no longer editable")
	}
	return nil
}
func safeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	parts := strings.Split(name, "/")
	name = parts[len(parts)-1]
	name = strings.Map(func(r rune) rune {
		if r < ' ' || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "" {
		name = "attachment"
	}
	if len(name) > 200 {
		name = name[:200]
	}
	return name
}
func SaveAttachment(ctx context.Context, workspaceID, mailboxID, itemID string, file router.File) (schema.AttachmentView, error) {
	if _, err := ItemAccess(ctx, workspaceID, mailboxID, itemID); err != nil {
		return schema.AttachmentView{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.AttachmentView{}, err
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	i, err := q.LockMailItem(ctx, itemID)
	if err != nil {
		return schema.AttachmentView{}, err
	}
	if err = CheckEditable(i); err != nil {
		return schema.AttachmentView{}, err
	}
	rows, err := q.ListMailAttachments(ctx, itemID)
	if err != nil {
		return schema.AttachmentView{}, err
	}
	if len(rows) >= 10 {
		return schema.AttachmentView{}, router.Errorf(422, "at most ten attachments are allowed")
	}
	var size int64
	for _, a := range rows {
		size += int64(a.Size)
	}
	key := fmt.Sprintf("mail/%s/%s/attachment-%d", mailboxID, itemID, time.Now().UnixNano())
	object, err := storage.From(ctx).Put(ctx, key, file.Body, storage.PutOptions{ContentType: file.ContentType})
	if err != nil {
		return schema.AttachmentView{}, err
	}
	if size+object.Size > 10<<20 {
		return schema.AttachmentView{}, errors.Join(router.Errorf(413, "attachments exceed ten megabytes"), storage.From(ctx).Delete(ctx, key))
	}
	a, err := q.AddMailAttachment(ctx, queries.AddMailAttachmentParams{ItemID: itemID, Name: safeName(file.Name), ContentType: object.ContentType, Size: int32(object.Size), ObjectKey: key})
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		return schema.AttachmentView{}, errors.Join(err, storage.From(ctx).Delete(ctx, key))
	}
	return Attachment(a), nil
}

// Lock authorizes and serializes mutable message operations.
func Lock(ctx context.Context, workspaceID, mailboxID, itemID string) (pgx.Tx, *queries.Queries, queries.MailItem, error) {
	if _, err := ItemAccess(ctx, workspaceID, mailboxID, itemID); err != nil {
		return nil, nil, queries.MailItem{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return nil, nil, queries.MailItem{}, err
	}
	q := queries.New(tx)
	item, err := q.LockMailItem(ctx, itemID)
	if err != nil {
		return nil, nil, item, errors.Join(err, tx.Rollback(ctx))
	}
	return tx, q, item, nil
}
