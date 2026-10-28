package labels

import (
	"context"
	"errors"

	"github.com/agim/lidza/packs/db"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
)

// RealPerson is the system label mail screening applies to inbound mail from
// people rather than machines. One per workspace, created lazily, pinned in
// Mail for every member until they unpin it, and never deletable.
const RealPerson = "real_person"

type systemSpec struct{ name, app string }

// systemKeys are the system labels; systemLabel names each and the app it is
// pinned in by default.
func systemKeys() []string { return []string{RealPerson} }

func systemLabel(key string) systemSpec {
	switch key {
	case RealPerson:
		return systemSpec{"Real person", "mail"}
	}
	return systemSpec{}
}

// EnsureSystemLabel returns the workspace's system label, creating it (or
// adopting a member-made label of the same name) on first use. The caller
// either holds the workspace lock (ensureLocked) or this takes it.
func EnsureSystemLabel(ctx context.Context, queries *q.Queries, w, key string) (q.MailLabel, error) {
	l, err := queries.GetSystemLabel(ctx, q.GetSystemLabelParams{WorkspaceID: &w, SystemKey: &key})
	if !errors.Is(err, pgx.ErrNoRows) {
		return l, err
	}
	spec := systemLabel(key)
	n, err := queries.AdoptSystemLabel(ctx, q.AdoptSystemLabelParams{SystemKey: &key, WorkspaceID: &w, Name: spec.name})
	if err != nil {
		return l, err
	}
	if n == 0 {
		if err = queries.InsertSystemLabel(ctx, q.InsertSystemLabelParams{WorkspaceID: &w, Name: spec.name, SystemKey: &key}); err != nil {
			return l, err
		}
	}
	return queries.GetSystemLabel(ctx, q.GetSystemLabelParams{WorkspaceID: &w, SystemKey: &key})
}

// ensureAll makes every system label exist for the workspace (pins need its
// id for the default pin; the catalog list itself never creates one). The common case
// is one read; the first call takes the workspace lock so two requests cannot
// adopt or create twice.
func ensureAll(ctx context.Context, w string) error {
	queries := q.New(db.From(ctx))
	missing := false
	for _, key := range systemKeys() {
		k := key
		if _, err := queries.GetSystemLabel(ctx, q.GetSystemLabelParams{WorkspaceID: &w, SystemKey: &k}); errors.Is(err, pgx.ErrNoRows) {
			missing = true
		} else if err != nil {
			return err
		}
	}
	if !missing {
		return nil
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tq := q.New(tx)
	if _, err = tq.LockWorkspace(ctx, w); err != nil {
		return err
	}
	for _, key := range systemKeys() {
		if _, err = EnsureSystemLabel(ctx, tq, w, key); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ApplySystemLabel tags a mail item with a system label, or removes the tag.
func ApplySystemLabel(ctx context.Context, queries *q.Queries, w, key, item string, applied bool) error {
	if !applied {
		// Removing never creates the label: no label, no tag to remove.
		l, err := queries.GetSystemLabel(ctx, q.GetSystemLabelParams{WorkspaceID: &w, SystemKey: &key})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return queries.RemoveMailTag(ctx, q.RemoveMailTagParams{ItemID: item, LabelID: l.ID})
	}
	l, err := EnsureSystemLabel(ctx, queries, w, key)
	if err != nil {
		return err
	}
	return queries.AddMailTag(ctx, q.AddMailTagParams{ItemID: item, LabelID: l.ID})
}
