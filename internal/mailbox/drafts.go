package mailbox

import (
	"context"
	"errors"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	queries "thura/db/queries/gen"
	"thura/internal/workspace"
	"thura/schema"
)

// CreateDraft accepts an authorized mailbox. Forwarded attachments receive new
// metadata IDs and retain references to the source's immutable stored objects.
func CreateDraft(ctx context.Context, m queries.Mailbox, authorID string, in schema.DraftInput) (queries.MailItem, error) {
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return queries.MailItem{}, err
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	var attachments []queries.MailAttachment
	if in.ForwardID != nil {
		if !workspace.ValidID(*in.ForwardID) {
			return queries.MailItem{}, router.Errorf(404, "message not found")
		}
		source, err := q.LockMailItem(ctx, *in.ForwardID)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && source.MailboxID != m.ID {
			return queries.MailItem{}, router.Errorf(404, "message not found")
		}
		if err != nil {
			return queries.MailItem{}, err
		}
		attachments, err = q.ListMailAttachments(ctx, source.ID)
		if err != nil {
			return queries.MailItem{}, err
		}
		if len(attachments) > 10 {
			return queries.MailItem{}, router.Errorf(422, "at most ten attachments are allowed")
		}
		var size int64
		for _, a := range attachments {
			if a.Size < 0 {
				return queries.MailItem{}, router.Errorf(422, "invalid source attachment size")
			}
			size += int64(a.Size)
		}
		if size > 10<<20 {
			return queries.MailItem{}, router.Errorf(413, "attachments exceed ten megabytes")
		}
	}
	item, err := q.CreateDraft(ctx, queries.CreateDraftParams{MailboxID: m.ID, AuthorID: authorID, FromAddress: m.Address, ToAddress: Text(in.To), Cc: Text(in.Cc), Bcc: Text(in.Bcc), Subject: Text(in.Subject), TextBody: Text(in.Text), HTMLBody: Text(in.HTML), ThreadID: Text(in.ThreadID)})
	if err != nil {
		return queries.MailItem{}, err
	}
	for _, a := range attachments {
		_, err = q.AddMailAttachment(ctx, queries.AddMailAttachmentParams{ItemID: item.ID, Name: a.Name, ContentType: a.ContentType, Size: a.Size, ObjectKey: a.ObjectKey, ContentID: a.ContentID})
		if err != nil {
			return queries.MailItem{}, err
		}
	}
	return item, tx.Commit(ctx)
}
