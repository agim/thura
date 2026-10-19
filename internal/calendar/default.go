package calendar

import (
	"context"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	q "thura/db/queries/gen"
	"thura/internal/workspace"
	"thura/schema"
)

// List initializes a stable private default for existing and new members.
// The deterministic primary key makes concurrent first visits idempotent.
func List(ctx context.Context, w string) (schema.CalendarList, error) {
	out := schema.CalendarList{Items: []schema.Calendar{}}
	if err := workspace.RequireMember(ctx, w); err != nil {
		return out, err
	}
	subject := auth.CurrentUser(ctx).ID
	namespace, err := uuid.Parse(w)
	if err != nil {
		return out, router.Errorf(404, "workspace not found")
	}
	id := uuid.NewSHA1(namespace, []byte("thura/personal-calendar/"+subject)).String()
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return out, err
	}
	member, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: w, Subject: subject})
	if err != nil {
		return out, err
	}
	if !member {
		return out, router.Errorf(404, "workspace not found")
	}
	if err = queries.EnsurePersonalCalendar(ctx, q.EnsurePersonalCalendarParams{ID: id, WorkspaceID: w, OwnerSubject: &subject}); err != nil {
		return out, err
	}
	personal, err := queries.GetCalendar(ctx, q.GetCalendarParams{WorkspaceID: w, ID: id, OwnerSubject: &subject})
	if err != nil {
		return out, err
	}
	if personal.OwnerSubject == nil || *personal.OwnerSubject != subject {
		return out, router.Errorf(409, "personal calendar ownership conflict")
	}
	rows, err := queries.ListCalendars(ctx, q.ListCalendarsParams{WorkspaceID: w, OwnerSubject: &subject})
	if err != nil {
		return out, err
	}
	out.DefaultCalendarID = id
	out.Items = append(out.Items, View(personal))
	for _, row := range rows {
		if row.ID != id {
			out.Items = append(out.Items, View(row))
		}
	}
	return out, tx.Commit(ctx)
}
