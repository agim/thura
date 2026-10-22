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

// ErrNoPrivateMailbox reports a private-only notice whose author has no
// managed personal inbox in the team.
var ErrNoPrivateMailbox = errors.New("the author has no personal team mailbox for this notification")

// Notice is a team notification. Message is what is transmitted. Its sender
// copy is filed in the chosen mailbox like composed mail, so when that
// mailbox is shared, every member reads it: Stored, when set, replaces the
// filed subject and bodies there (an invitation link, a reply grant or a
// meeting PIN must never be readable by other members), while the complete
// Message is still frozen and sent. Private refuses shared mailboxes with
// ErrNoPrivateMailbox instead.
type Notice struct {
	Message mail.Message
	Stored  *mail.Message
	Private bool
}

// SendTx files and queues a notice from the author's own managed inbox when
// they have one, and only otherwise from the first shared managed mailbox by
// name. Legacy workspaces without team setup use the deployment mail pack.
func SendTx(ctx context.Context, tx pgx.Tx, id, author string, n Notice) (string, error) {
	queries := q.New(tx)
	msg := n.Message
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
	var box, shared q.Mailbox
	var cfg, sharedCfg mail.Config
	for _, m := range boxes {
		if !mailaccess.Owns(m, author) {
			continue
		}
		resolved, managed, err := tenancy.ResolveMail(ctx, m)
		if !managed || err != nil {
			continue
		}
		if m.Private {
			box, cfg = m, resolved
			break
		}
		if shared.ID == "" {
			shared, sharedCfg = m, resolved
		}
	}
	if box.ID == "" {
		if n.Private {
			return "", ErrNoPrivateMailbox
		}
		box, cfg = shared, sharedCfg
	}
	if box.ID == "" {
		return "", errors.New("configure a team mailbox and provider before sending notifications")
	}
	msg.From = box.Address
	filed := msg
	if !box.Private && n.Stored != nil {
		filed.Subject, filed.Text, filed.HTML = n.Stored.Subject, n.Stored.Text, n.Stored.HTML
	}
	item, e := queries.CreateDraft(ctx, q.CreateDraftParams{MailboxID: box.ID, AuthorID: author, FromAddress: box.Address, ToAddress: msg.To, Cc: strings.Join(msg.Cc, ", "), Bcc: strings.Join(msg.Bcc, ", "), Subject: filed.Subject, TextBody: filed.Text, HTMLBody: filed.HTML, ThreadID: ""})
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
