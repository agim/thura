package mailbox

import (
	"context"
	queries "thura/db/queries/gen"
	"thura/internal/labels"
	"thura/schema"
)

func LabelView(l queries.MailLabel) schema.MailLabel { return labels.View(l) }
func CreateLabel(ctx context.Context, w, m string, in schema.MailLabelInput) (schema.MailLabel, error) {
	if _, err := Access(ctx, w, m); err != nil {
		return schema.MailLabel{}, err
	}
	return labels.Create(ctx, w, in.Name)
}
func ChangeLabel(ctx context.Context, w, m, id string, in schema.MailLabelChange) error {
	if _, err := ItemAccess(ctx, w, m, id); err != nil {
		return err
	}
	return labels.Change(ctx, w, "mail", id, in.LabelID, in.Applied)
}
func DeleteLabel(ctx context.Context, w, m, id string) error {
	if _, err := Access(ctx, w, m); err != nil {
		return err
	}
	return labels.Delete(ctx, w, id)
}
