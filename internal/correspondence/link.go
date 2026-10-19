package correspondence

import (
	"context"
	"errors"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/workspace"
)

// Link organizes a source without granting access to it. Every timeline read
// rechecks the source's current visibility, including private rooms/calendars.
func Link(ctx context.Context, w, contact, source, id string, remove bool) error {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return err
	}
	if !workspace.ValidID(contact) || !workspace.ValidID(id) {
		return router.Errorf(404, "item not found")
	}
	switch source {
	case "chat", "document", "meeting", "calendar":
	default:
		return router.Errorf(422, "invalid correspondence source")
	}
	queries := q.New(db.From(ctx))
	if _, err := queries.GetCorrespondenceContact(ctx, q.GetCorrespondenceContactParams{WorkspaceID: w, ID: contact}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return router.Errorf(404, "contact not found")
		}
		return err
	}
	if remove {
		return queries.RemoveCorrespondenceLink(ctx, q.RemoveCorrespondenceLinkParams{ContactID: contact, Source: source, SourceID: id})
	}
	rows, err := queries.CorrespondenceCandidates(ctx, q.CorrespondenceCandidatesParams{WorkspaceID: w, ContactID: contact, Viewer: auth.CurrentUser(ctx).ID, Browse: true, SourceFilter: source, TargetID: id, BeforeID: "00000000-0000-0000-0000-000000000000", PageLimit: 1})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return router.Errorf(404, "item not found")
	}
	return queries.AddCorrespondenceLink(ctx, q.AddCorrespondenceLinkParams{ContactID: contact, Source: source, SourceID: id, CreatedBy: auth.CurrentUser(ctx).ID})
}
