package mailbox

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	queries "thura/db/queries/gen"
	"thura/internal/labels"
	"thura/internal/mailfilter"
	"thura/internal/workspace"
	"thura/schema"
)

// Filters are a mailbox's own (internal/mailfilter runs them). A private
// mailbox's filters are its owner's; a shared mailbox files mail for every
// member, so only the workspace's owners and admins change its filters,
// as with its signature. Every reader of the mailbox may see them.
const (
	maxFilters = 100
	// existingLimit bounds "also apply to mail already in the Inbox".
	existingLimit = 5000
	existingPage  = 500
)

func FilterView(f queries.MailFilter) schema.MailFilter {
	return schema.MailFilter{ID: f.ID, MailboxID: f.MailboxID, FromContains: f.FromContains, ToContains: f.ToContains, SubjectContains: f.SubjectContains, HasWords: f.HasWords,
		LabelID: f.LabelID, SkipInbox: f.SkipInbox, MarkRead: f.MarkRead, Star: f.Star, Trash: f.Trash, Enabled: f.Enabled, Position: int(f.Position), CreatedAt: f.CreatedAt}
}

func ListFilters(ctx context.Context, w, m string) (schema.MailFilterList, error) {
	out := schema.MailFilterList{Items: []schema.MailFilter{}}
	if _, err := Access(ctx, w, m); err != nil {
		return out, err
	}
	rows, err := queries.New(db.From(ctx)).ListMailFilters(ctx, m)
	for _, f := range rows {
		out.Items = append(out.Items, FilterView(f))
	}
	return out, err
}

func filterAccess(ctx context.Context, w, m string) error {
	box, err := Access(ctx, w, m)
	if err != nil {
		return err
	}
	if !box.Private {
		return workspace.RequireManager(ctx, w)
	}
	return nil
}

func criterion(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return strings.Join(strings.Fields(*value), " ")
}

func flag(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

// SaveFilter creates a filter (id "") or replaces one. The label is one of
// the workspace's, by id or by name (created when new). With applyExisting
// the filter also files matching mail already in the Inbox, newest first.
func SaveFilter(ctx context.Context, w, m, id string, in schema.MailFilterInput) (schema.MailFilterResult, error) {
	out := schema.MailFilterResult{}
	if err := filterAccess(ctx, w, m); err != nil {
		return out, err
	}
	if err := in.Validate(); err != nil {
		return out, err
	}
	if id != "" && !workspace.ValidID(id) {
		return out, router.Errorf(404, "filter not found")
	}
	pool := queries.New(db.From(ctx))
	current := queries.MailFilter{MailboxID: m, Enabled: true}
	if id != "" {
		var err error
		if current, err = pool.GetMailFilter(ctx, queries.GetMailFilterParams{ID: id, MailboxID: m}); errors.Is(err, pgx.ErrNoRows) {
			return out, router.Errorf(404, "filter not found")
		} else if err != nil {
			return out, err
		}
	}
	next := current
	next.FromContains, next.ToContains = criterion(in.FromContains, current.FromContains), criterion(in.ToContains, current.ToContains)
	next.SubjectContains, next.HasWords = criterion(in.SubjectContains, current.SubjectContains), criterion(in.HasWords, current.HasWords)
	next.SkipInbox, next.MarkRead, next.Star = flag(in.SkipInbox, current.SkipInbox), flag(in.MarkRead, current.MarkRead), flag(in.Star, current.Star)
	next.Trash, next.Enabled = flag(in.Trash, current.Trash), flag(in.Enabled, current.Enabled)
	if mailfilter.Empty(next) {
		return out, router.Errorf(422, "a filter needs at least one of From, To, Subject or Has the words")
	}
	switch {
	case in.LabelName != nil && strings.TrimSpace(*in.LabelName) != "":
		label, err := labels.Ensure(ctx, w, *in.LabelName)
		if err != nil {
			return out, err
		}
		next.LabelID = &label.ID
	case in.LabelID != nil:
		next.LabelID = nil
		if *in.LabelID != "" {
			if !workspace.ValidID(*in.LabelID) {
				return out, router.Errorf(422, "label not found")
			}
			label, err := pool.GetWorkspaceLabel(ctx, queries.GetWorkspaceLabelParams{ID: *in.LabelID, WorkspaceID: &w})
			if errors.Is(err, pgx.ErrNoRows) {
				return out, router.Errorf(422, "label not found")
			}
			if err != nil {
				return out, err
			}
			next.LabelID = &label.ID
		}
	}
	if next.LabelID == nil && !next.SkipInbox && !next.MarkRead && !next.Star && !next.Trash {
		return out, router.Errorf(422, "choose what the filter does: apply a label, skip the Inbox, mark as read, star or move to Trash")
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	q := queries.New(tx)
	if _, err = q.LockMailbox(ctx, m); err != nil {
		return out, err
	}
	var saved queries.MailFilter
	if id == "" {
		count, err := q.CountMailFilters(ctx, m)
		if err != nil {
			return out, err
		}
		if count >= maxFilters {
			return out, router.Errorf(409, "this mailbox has %d filters; delete one first", maxFilters)
		}
		saved, err = q.CreateMailFilter(ctx, queries.CreateMailFilterParams{MailboxID: m, FromContains: next.FromContains, ToContains: next.ToContains, SubjectContains: next.SubjectContains, HasWords: next.HasWords,
			LabelID: next.LabelID, SkipInbox: next.SkipInbox, MarkRead: next.MarkRead, Star: next.Star, Trash: next.Trash, Enabled: next.Enabled})
		if err != nil {
			return out, err
		}
	} else {
		saved, err = q.UpdateMailFilter(ctx, queries.UpdateMailFilterParams{ID: id, MailboxID: m, FromContains: next.FromContains, ToContains: next.ToContains, SubjectContains: next.SubjectContains, HasWords: next.HasWords,
			LabelID: next.LabelID, SkipInbox: next.SkipInbox, MarkRead: next.MarkRead, Star: next.Star, Trash: next.Trash, Enabled: next.Enabled})
		if errors.Is(err, pgx.ErrNoRows) {
			return out, router.Errorf(404, "filter not found")
		}
		if err != nil {
			return out, err
		}
	}
	if flag(in.ApplyExisting, false) && saved.Enabled {
		if out.Applied, err = fileExisting(ctx, q, saved); err != nil {
			return out, err
		}
	}
	out.Filter = FilterView(saved)
	return out, tx.Commit(ctx)
}

// fileExisting runs one filter over the received mail already in the Inbox
// and answers how many messages it filed.
func fileExisting(ctx context.Context, q *queries.Queries, f queries.MailFilter) (int, error) {
	applied, seen := 0, 0
	var before *time.Time
	beforeID := "00000000-0000-0000-0000-000000000000"
	for seen < existingLimit {
		rows, err := q.ListInboxForFilters(ctx, queries.ListInboxForFiltersParams{MailboxID: f.MailboxID, BeforeTime: before, BeforeID: beforeID, PageLimit: existingPage})
		if err != nil {
			return applied, err
		}
		for _, item := range rows {
			_, matched, err := mailfilter.File(ctx, q, []queries.MailFilter{f}, item)
			if err != nil {
				return applied, err
			}
			if matched {
				applied++
			}
		}
		if len(rows) < existingPage {
			break
		}
		seen += len(rows)
		last := rows[len(rows)-1]
		before, beforeID = &last.UpdatedAt, last.ID
	}
	return applied, nil
}

func DeleteFilter(ctx context.Context, w, m, id string) error {
	if err := filterAccess(ctx, w, m); err != nil {
		return err
	}
	if !workspace.ValidID(id) {
		return router.Errorf(404, "filter not found")
	}
	n, err := queries.New(db.From(ctx)).DeleteMailFilter(ctx, queries.DeleteMailFilterParams{ID: id, MailboxID: m})
	if err == nil && n == 0 {
		err = router.Errorf(404, "filter not found")
	}
	return err
}
