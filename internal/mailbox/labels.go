package mailbox

import (
	"context"
	"errors"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"strings"
	queries "thura/db/queries/gen"
	"thura/internal/workspace"
	"thura/schema"
)

func LabelView(label queries.MailLabel) schema.MailLabel {
	return schema.MailLabel{ID: label.ID, MailboxID: label.MailboxID, Name: label.Name}
}
func CreateLabel(ctx context.Context, w, m string, in schema.MailLabelInput) (schema.MailLabel, error) {
	if _, err := Access(ctx, w, m); err != nil {
		return schema.MailLabel{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.MailLabel{}, err
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	if _, err = q.LockMailbox(ctx, m); err != nil {
		return schema.MailLabel{}, err
	}
	labels, err := q.ListMailboxLabels(ctx, m)
	if err != nil {
		return schema.MailLabel{}, err
	}
	if len(labels) >= 50 {
		return schema.MailLabel{}, router.Errorf(409, "mailbox label limit reached")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return schema.MailLabel{}, router.Errorf(422, "label name is required")
	}
	for _, label := range labels {
		if strings.EqualFold(label.Name, name) {
			return schema.MailLabel{}, router.Errorf(409, "label already exists")
		}
	}
	label, err := q.CreateMailboxLabel(ctx, queries.CreateMailboxLabelParams{MailboxID: m, Name: name})
	if err != nil {
		return schema.MailLabel{}, err
	}
	return LabelView(label), tx.Commit(ctx)
}
func ChangeLabel(ctx context.Context, w, m, itemID string, in schema.MailLabelChange) error {
	if _, err := ItemAccess(ctx, w, m, itemID); err != nil {
		return err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	if _, err = q.LockMailbox(ctx, m); err != nil {
		return err
	}
	if !workspace.ValidID(in.LabelID) {
		return router.Errorf(404, "label not found")
	}
	if _, err = q.GetMailboxLabel(ctx, queries.GetMailboxLabelParams{ID: in.LabelID, MailboxID: m}); errors.Is(err, pgx.ErrNoRows) {
		return router.Errorf(404, "label not found")
	}
	if err != nil {
		return err
	}
	if _, err = q.LockMailItem(ctx, itemID); err != nil {
		return err
	}
	if in.Applied {
		err = q.AddMailTag(ctx, queries.AddMailTagParams{ItemID: itemID, LabelID: in.LabelID})
	} else {
		err = q.RemoveMailTag(ctx, queries.RemoveMailTagParams{ItemID: itemID, LabelID: in.LabelID})
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func DeleteLabel(ctx context.Context, w, m, id string) error {
	if _, err := Access(ctx, w, m); err != nil {
		return err
	}
	if !workspace.ValidID(id) {
		return router.Errorf(404, "label not found")
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	if _, err = q.LockMailbox(ctx, m); err != nil {
		return err
	}
	if _, err = q.GetMailboxLabel(ctx, queries.GetMailboxLabelParams{ID: id, MailboxID: m}); errors.Is(err, pgx.ErrNoRows) {
		return router.Errorf(404, "label not found")
	}
	if err != nil {
		return err
	}
	if err = q.DeleteLabelTags(ctx, id); err != nil {
		return err
	}
	if err = q.DeleteMailboxLabel(ctx, queries.DeleteMailboxLabelParams{ID: id, MailboxID: m}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
