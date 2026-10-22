package mailbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
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
	"net/textproto"
	"net/url"
	"regexp"
	"strings"
	queries "thura/db/queries/gen"
	"thura/internal/mailabuse"
	"thura/internal/mailaccess"
	"thura/internal/platform/objectstore"
	"thura/internal/richtext"
	"thura/internal/smipmail"
	"thura/internal/tenancy"
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
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.Mailbox{}, err
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	if _, err := q.LockWorkspace(ctx, in.WorkspaceID); err != nil {
		return schema.Mailbox{}, err
	}
	_, domainName, _ := strings.Cut(strings.ToLower(in.Address), "@")
	if err = tenancy.LockDomain(ctx, tx, domainName); err != nil {
		return schema.Mailbox{}, err
	}
	if exists, err := q.TeamMailboxAddressExists(ctx, strings.ToLower(in.Address)); err != nil {
		return schema.Mailbox{}, err
	} else if exists {
		return schema.Mailbox{}, router.Errorf(409, "This email address is already assigned to a mailbox or alias")
	}
	if d, e := q.BoundTeamDomain(ctx, domainName); e == nil && d.WorkspaceID != in.WorkspaceID {
		return schema.Mailbox{}, router.Errorf(409, "This email domain belongs to another team")
	} else if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return schema.Mailbox{}, e
	}
	m, err := q.CreateMailbox(ctx, queries.CreateMailboxParams{WorkspaceID: in.WorkspaceID, Name: strings.TrimSpace(in.Name), Address: strings.ToLower(in.Address), ConfigPrefix: prefix})
	if err != nil {
		return schema.Mailbox{}, err
	}
	return ViewMailbox(m), tx.Commit(ctx)
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
	if err == nil {
		err = mailaccess.Check(ctx, queries.New(db.From(ctx)), m, auth.CurrentUser(ctx).ID)
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
	return schema.Mailbox{ID: m.ID, WorkspaceID: m.WorkspaceID, Name: m.Name, Address: m.Address, ConfigPrefix: m.ConfigPrefix, Private: m.Private, OwnerSubject: m.OwnerSubject, Signature: m.Signature, CreatedAt: m.CreatedAt}
}
func View(i queries.MailItem) schema.MailItem {
	formatted := ReaderHTML(i.HTMLBody)
	if i.Folder == queries.MailFolderDrafts {
		formatted = richtext.Clean(i.HTMLBody)
	}
	return schema.MailItem{ID: i.ID, MailboxID: i.MailboxID, AuthorID: i.AuthorID, Folder: schema.MailFolder(i.Folder), FromAddress: i.FromAddress, ToAddress: i.ToAddress, Cc: i.Cc, Bcc: i.Bcc, Subject: i.Subject, TextBody: i.TextBody, HTMLBody: formatted, Status: i.Status, Starred: i.Starred, Unread: i.Unread, ProviderID: i.ProviderID, DeliveryReason: i.DeliveryReason, RawKey: i.RawKey, ExternalID: i.ExternalID, ThreadID: i.ThreadID, MessageID: i.MessageID, InReplyTo: i.InReplyTo, ReferencesHeader: i.ReferencesHeader, SendAt: i.SendAt, CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt}
}
func Attachment(a queries.MailAttachment) schema.AttachmentView {
	return schema.AttachmentView{ID: a.ID, Name: a.Name, ContentType: a.ContentType, Size: int(a.Size), ContentID: ContentID(a.ContentID)}
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
	var recipientLimits struct {
		Max int `env:"MAIL_MAX_RECIPIENTS" default:"50"`
	}
	if err := env.Load(".", &recipientLimits); err != nil {
		return schema.MailItem{}, err
	}
	if recipientLimits.Max < 1 || recipientLimits.Max > 50 {
		return schema.MailItem{}, router.Errorf(503, "invalid server recipient limit")
	}
	if len(to)+len(cc)+len(bcc) == 0 || len(to)+len(cc)+len(bcc) > recipientLimits.Max {
		return schema.MailItem{}, router.Errorf(422, "recipient count exceeds the configured email limit")
	}

	if strings.TrimSpace(i.Subject) == "" {
		return schema.MailItem{}, router.Errorf(422, "subject is required")
	}
	if strings.ContainsAny(i.Subject, "\r\n") {
		return schema.MailItem{}, router.Errorf(422, "subject cannot contain line breaks")
	}
	// Refuse now what delivery would hold: the queued message is immutable.
	box, err := q.GetMailbox(ctx, mailboxID)
	if err != nil {
		return schema.MailItem{}, err
	}
	msg, err := compose(box, i)
	if err != nil {
		return schema.MailItem{}, err
	}
	if err = smipmail.Check(msg); err != nil {
		return schema.MailItem{}, router.Errorf(422, "message cannot be sent: %v", err)
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
	// Another node's clock may have found the item due: a committed transmission
	// is a delivery intent, whatever this node thinks of the schedule.
	if _, err = q.GetMailTransmission(ctx, itemID); err == nil {
		return schema.MailItem{}, router.Errorf(409, "delivery has already started")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return schema.MailItem{}, err
	}
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
	// The workspace is locked before the item, the order Lock takes for every
	// user action (undo, move, flags); an unlocked read only finds the workspace.
	peek, err := queries.New(db.From(ctx)).GetMailItem(ctx, data.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	box, err := queries.New(db.From(ctx)).GetMailbox(ctx, peek.MailboxID)
	if err != nil {
		return err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	if _, err = q.LockWorkspace(ctx, box.WorkspaceID); err != nil {
		return err
	}
	i, err := q.LockMailItem(ctx, data.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	existing, existingErr := q.GetMailTransmission(ctx, i.ID)
	if existingErr == nil {
		if err = tx.Rollback(ctx); err != nil {
			return err
		}
		return smipmail.Dispatch(ctx, smipmail.Job(existing.ID))
	}
	if !errors.Is(existingErr, pgx.ErrNoRows) {
		return existingErr
	}
	// Only a queued item enters delivery. A provider error is no longer
	// recorded as "failed" and retried: once a submission has started the
	// pack cannot prove the message was refused, and a retry would send a
	// second copy. The outcome is held as delivery_uncertain (unknown) or
	// delivery_blocked (refused before or by the provider), both terminal.
	if i.Status != "queued" {
		return nil
	}
	if i.SendAt != nil && i.SendAt.After(lidza.Now(ctx)) {
		return errors.New("delivery is not due yet")
	}
	m, err := q.GetMailbox(ctx, i.MailboxID)
	if err != nil {
		return err
	}
	if err = mailaccess.Check(ctx, q, m, i.AuthorID); err != nil {
		return err
	}
	cfg, managed, err := tenancy.ResolveMail(ctx, m)
	if !managed && err == nil {
		cfg, err = Config(m)
	}
	if err != nil {
		if permanent(err) {
			return hold(ctx, tx, q, i, "delivery_blocked", reason("", err))
		}
		return err
	}
	msg, err := compose(m, i)
	if err == nil {
		err = smipmail.Check(msg)
	}
	if err != nil {
		return hold(ctx, tx, q, i, "delivery_blocked", reason("Message cannot be sent", err))
	}
	attachments, err := q.ListMailAttachments(ctx, i.ID)
	if err != nil {
		return err
	}
	var total int64
	for _, a := range attachments {
		if err := mailabuse.GuardObject(ctx, a.ObjectKey); err != nil {
			if permanent(err) {
				return hold(ctx, tx, q, i, "delivery_blocked", reason("", err))
			}
			return err
		}
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
			return hold(ctx, tx, q, i, "delivery_blocked", "Attachments exceed ten megabytes")
		}
		msg.Attachments = append(msg.Attachments, mail.Attachment{Name: a.Name, ContentType: a.ContentType, Data: b, ContentID: a.ContentID})
	}
	if managed || smipmail.From(ctx).Enabled() {
		transmission, err := smipmail.Prepare(ctx, tx, m, i, msg, cfg)
		if err != nil {
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		return smipmail.Dispatch(ctx, smipmail.Job(transmission.ID))
	}
	transport, err := mail.New(cfg, nil, nil)
	if err != nil {
		return err
	}
	// The marker commits BEFORE the one provider call, as the SMIP outbox does:
	// a timeout, a crash or a lost commit after it leaves the item uncertain,
	// never queued, so no run can submit the message a second time.
	if err = hold(ctx, tx, q, i, "delivery_uncertain", "Provider outcome unknown; not retried automatically"); err != nil {
		return err
	}
	providerID, sendErr := transport.Send(smipmail.ProviderContext(ctx), msg)
	return record(ctx, m, i, cfg.Provider, providerID, sendErr)
}

// compose builds the outgoing message without its attachments: Queue checks it
// and Deliver adds the stored files.
func compose(m queries.Mailbox, i queries.MailItem) (mail.Message, error) {
	cc, err := addresses(i.Cc)
	if err != nil {
		return mail.Message{}, err
	}
	bcc, err := addresses(i.Bcc)
	if err != nil {
		return mail.Message{}, err
	}
	msg := mail.Message{To: i.ToAddress, Cc: cc, Bcc: bcc, From: m.Address, Subject: i.Subject, Text: i.TextBody, HTML: i.HTMLBody, Headers: map[string]string{}}
	if i.InReplyTo != "" {
		msg.Headers["In-Reply-To"] = i.InReplyTo
	}
	if i.ReferencesHeader != "" {
		msg.Headers["References"] = i.ReferencesHeader
	}
	return msg, nil
}

// hold commits a terminal delivery state for the item locked in tx.
func hold(ctx context.Context, tx pgx.Tx, q *queries.Queries, i queries.MailItem, status, why string) error {
	if err := q.SetMailOutcome(ctx, queries.SetMailOutcomeParams{ID: i.ID, Status: status, ProviderID: i.ProviderID, Folder: i.Folder, DeliveryReason: why}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// record stores the provider's answer after the uncertain marker committed.
// Without a definite refusal the item stays uncertain: the job never retries.
func record(ctx context.Context, m queries.Mailbox, i queries.MailItem, provider, providerID string, sendErr error) error {
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	if _, err = q.LockWorkspace(ctx, m.WorkspaceID); err != nil {
		return err
	}
	current, err := q.LockMailItem(ctx, i.ID)
	if err != nil {
		return err
	}
	if current.Status != "delivery_uncertain" {
		return errors.New("provider attempt was superseded")
	}
	status, folder, why := "sent", queries.MailFolderSent, ""
	switch {
	case sendErr == nil && (provider == "outbox" || provider == "log"):
		status = "captured"
	case sendErr == nil:
	case definite(sendErr):
		status, folder, why = "delivery_blocked", current.Folder, reason("Provider refused the message", sendErr)
	default:
		lidza.Log(ctx).Warn("mail provider outcome unknown; not retried", "item", i.ID, "error", sendErr)
		status, folder, why = "delivery_uncertain", current.Folder, reason("Provider outcome unknown; not retried automatically", sendErr)
	}
	if err = q.SetMailOutcome(ctx, queries.SetMailOutcomeParams{ID: i.ID, Status: status, ProviderID: providerID, Folder: folder, DeliveryReason: why}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// permanent is a refusal the job cannot outwait: a conflict or validation reply.
func permanent(err error) bool {
	var httpErr *router.HTTPError
	return errors.As(err, &httpErr) && (httpErr.Status == 403 || httpErr.Status == 409 || httpErr.Status == 422)
}

var providerStatusPattern = regexp.MustCompile(`: ([45]\d\d) `)

// definite reports whether the provider answered and refused the message: an
// SMTP reply code, or an HTTP 4xx response the pack reports as "host: 4xx
// body". A transport error or a 5xx leaves the outcome unknown.
func definite(err error) bool {
	var reply *textproto.Error
	if errors.As(err, &reply) {
		return true
	}
	var transport *url.Error
	if errors.As(err, &transport) {
		return false
	}
	match := providerStatusPattern.FindStringSubmatch(err.Error())
	return match != nil && match[1][0] == '4'
}

// reason is a bounded, single-line text for the message's delivery state.
func reason(prefix string, err error) string {
	text := err.Error()
	var httpErr *router.HTTPError
	if errors.As(err, &httpErr) {
		text = httpErr.Message
	}
	text = strings.Map(func(r rune) rune {
		if r < ' ' || r == 127 {
			return ' '
		}
		return r
	}, text)
	if prefix != "" {
		text = prefix + ": " + text
	}
	return shortText(text, 300)
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
	object, err := objectstore.Put(ctx, key, file.Body, storage.PutOptions{ContentType: file.ContentType})
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
	if _, err := q.LockWorkspace(ctx, workspaceID); err != nil {
		return nil, nil, queries.MailItem{}, errors.Join(err, tx.Rollback(ctx))
	}
	box, err := q.GetMailbox(ctx, mailboxID)
	if err == nil {
		err = mailaccess.Check(ctx, q, box, auth.CurrentUser(ctx).ID)
	}
	if err != nil {
		return nil, nil, queries.MailItem{}, errors.Join(err, tx.Rollback(ctx))
	}
	item, err := q.LockMailItem(ctx, itemID)
	if err != nil {
		return nil, nil, item, errors.Join(err, tx.Rollback(ctx))
	}
	return tx, q, item, nil
}
