// Package labels owns the workspace catalog shared by all resource types.
package labels

import (
	"context"
	"errors"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"strings"
	q "thura/db/queries/gen"
	"thura/internal/workspace"
	"thura/schema"
	"unicode/utf8"
)

func View(l q.MailLabel) schema.MailLabel {
	return schema.MailLabel{ID: l.ID, WorkspaceID: l.WorkspaceID, MailboxID: l.MailboxID, Name: l.Name, SystemKey: l.SystemKey}
}
func List(ctx context.Context, w string) (schema.MailLabelList, error) {
	out := schema.MailLabelList{Items: []schema.MailLabel{}}
	if err := workspace.RequireMember(ctx, w); err != nil {
		return out, err
	}
	rows, err := q.New(db.From(ctx)).ListWorkspaceLabels(ctx, &w)
	for _, l := range rows {
		out.Items = append(out.Items, View(l))
	}
	return out, err
}

// Every catalog writer takes the workspace lock, including legacy mailbox routes.
// This also serializes case-insensitive duplicate names without changing old IDs.
func locked(ctx context.Context, w string) (pgx.Tx, *q.Queries, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return nil, nil, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err == nil {
		var member bool
		member, err = queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: w, Subject: auth.CurrentUser(ctx).ID})
		if err == nil && !member {
			err = router.Errorf(404, "workspace not found")
		}
	}
	if err != nil {
		return nil, nil, errors.Join(err, tx.Rollback(ctx))
	}
	return tx, queries, nil
}
func create(ctx context.Context, queries *q.Queries, w, name string, reuse bool) (q.MailLabel, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 40 {
		return q.MailLabel{}, router.Errorf(422, "label name must contain 1–40 characters")
	}
	rows, err := queries.ListWorkspaceLabels(ctx, &w)
	if err != nil {
		return q.MailLabel{}, err
	}
	for _, l := range rows {
		if strings.EqualFold(l.Name, name) {
			if reuse {
				return l, nil
			}
			return q.MailLabel{}, router.Errorf(409, "label already exists")
		}
	}
	if len(rows) >= 50 {
		return q.MailLabel{}, router.Errorf(409, "workspace label limit reached")
	}
	return queries.CreateWorkspaceLabel(ctx, q.CreateWorkspaceLabelParams{WorkspaceID: &w, Name: name})
}
func Create(ctx context.Context, w, name string) (schema.MailLabel, error) {
	tx, queries, err := locked(ctx, w)
	if err != nil {
		return schema.MailLabel{}, err
	}
	defer tx.Rollback(ctx)
	l, err := create(ctx, queries, w, name, false)
	if err != nil {
		return schema.MailLabel{}, err
	}
	return View(l), tx.Commit(ctx)
}
func assign(ctx context.Context, queries *q.Queries, source, id, labelID string, applied bool) error {
	if source == "mail" {
		if _, err := queries.LockMailItem(ctx, id); err != nil {
			return err
		}
		if applied {
			return queries.AddMailTag(ctx, q.AddMailTagParams{ItemID: id, LabelID: labelID})
		}
		return queries.RemoveMailTag(ctx, q.RemoveMailTagParams{ItemID: id, LabelID: labelID})
	}
	if applied {
		return queries.AddResourceLabel(ctx, q.AddResourceLabelParams{Source: source, SourceID: id, LabelID: labelID})
	}
	return queries.RemoveResourceLabel(ctx, q.RemoveResourceLabelParams{Source: source, SourceID: id, LabelID: labelID})
}
func CreateAndApply(ctx context.Context, w, source, id, name string) (schema.MailLabel, error) {
	tx, queries, err := locked(ctx, w)
	if err != nil {
		return schema.MailLabel{}, err
	}
	defer tx.Rollback(ctx)
	l, err := create(ctx, queries, w, name, true)
	if err == nil {
		err = assign(ctx, queries, source, id, l.ID, true)
	}
	if err != nil {
		return schema.MailLabel{}, err
	}
	return View(l), tx.Commit(ctx)
}
func lockLabel(ctx context.Context, w, id string) (pgx.Tx, *q.Queries, q.MailLabel, error) {
	if !workspace.ValidID(id) {
		return nil, nil, q.MailLabel{}, router.Errorf(404, "label not found")
	}
	tx, queries, err := locked(ctx, w)
	if err != nil {
		return nil, nil, q.MailLabel{}, err
	}
	l, err := queries.GetWorkspaceLabel(ctx, q.GetWorkspaceLabelParams{ID: id, WorkspaceID: &w})
	if errors.Is(err, pgx.ErrNoRows) {
		err = router.Errorf(404, "label not found")
	}
	if err != nil {
		return nil, nil, l, errors.Join(err, tx.Rollback(ctx))
	}
	return tx, queries, l, nil
}
func Change(ctx context.Context, w, source, id, labelID string, applied bool) error {
	tx, queries, _, err := lockLabel(ctx, w, labelID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = assign(ctx, queries, source, id, labelID, applied); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func Delete(ctx context.Context, w, id string) error {
	tx, queries, l, err := lockLabel(ctx, w, id)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if l.SystemKey != nil {
		return router.Errorf(409, "“%s” is a system label and cannot be deleted; unpin it instead", l.Name)
	}
	if err = queries.DeleteLabelTags(ctx, id); err != nil {
		return err
	}
	if err = queries.DeleteResourceLabelAssignments(ctx, id); err != nil {
		return err
	}
	if err = queries.DeleteWorkspaceLabel(ctx, q.DeleteWorkspaceLabelParams{ID: id, WorkspaceID: &w}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Filter returns the ids of the resources of one source carrying a label of
// workspace w, for list routes that take ?labelId=. No label means no filter
// (nil); a label of another workspace matches nothing.
func Filter(ctx context.Context, w, source, labelID string) (map[string]bool, error) {
	if labelID == "" {
		return nil, nil
	}
	if !workspace.ValidID(labelID) {
		return nil, router.Errorf(422, "labelId must be a label id")
	}
	ids, err := q.New(db.From(ctx)).ListLabeledResources(ctx, q.ListLabeledResourcesParams{LabelID: labelID, Source: source, WorkspaceID: &w})
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}
