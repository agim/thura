// Package teammail routes team notifications through the same durable,
// no-downgrade outbox as composed mail. Deployment auth mail stays separate.
package teammail

import (
	"context"
	"errors"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/packs/mail"
	"github.com/jackc/pgx/v5"
	"strings"
	q "thura/db/queries/gen"
	"thura/internal/mailaccess"
	"thura/internal/smipmail"
	"thura/internal/tenancy"
)

func SendTx(ctx context.Context, tx pgx.Tx, id, author string, msg mail.Message) (string, error) {
	queries := q.New(tx)
	_, e := queries.GetTeamSetup(ctx, id)
	if errors.Is(e, pgx.ErrNoRows) {
		return mail.From(ctx).SendTx(ctx, tx, msg)
	}
	if e != nil {
		return "", e
	}
	boxes, e := queries.ListMailboxes(ctx, id)
	if e != nil {
		return "", e
	}
	var box q.Mailbox
	var cfg mail.Config
	for _, m := range boxes {
		if !mailaccess.Owns(m, author) {
			continue
		}
		resolved, managed, err := tenancy.ResolveMail(ctx, m)
		if managed && err == nil {
			box = m
			cfg = resolved
			break
		}
	}
	if box.ID == "" {
		return "", errors.New("configure a team mailbox and provider before sending notifications")
	}
	msg.From = box.Address
	item, e := queries.CreateDraft(ctx, q.CreateDraftParams{MailboxID: box.ID, AuthorID: author, FromAddress: box.Address, ToAddress: msg.To, Cc: strings.Join(msg.Cc, ", "), Bcc: strings.Join(msg.Bcc, ", "), Subject: msg.Subject, TextBody: msg.Text, HTMLBody: msg.HTML, ThreadID: ""})
	if e != nil {
		return "", e
	}
	item, e = queries.QueueMailItem(ctx, q.QueueMailItemParams{ID: item.ID, SendAt: nil})
	if e != nil {
		return "", e
	}
	row, e := smipmail.Prepare(ctx, tx, box, item, msg, cfg)
	if e != nil {
		return "", e
	}
	_, e = jobs.From(ctx).EnqueueTx(ctx, tx, smipmail.DispatchJob, map[string]string{"id": row.ID}, jobs.Unique(row.ID))
	return item.ID, e
}

// Configured reports notification capture only for legacy development providers.
func Configured(ctx context.Context, id string) bool {
	_, e := q.New(db.From(ctx)).GetTeamSetup(ctx, id)
	return e == nil
}
