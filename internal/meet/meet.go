package meet

import (
	"context"
	"errors"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	q "thura/db/queries/gen"
	provider "thura/internal/providers/livekit"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func Room(id string) string { return "thura_" + id }
func View(m q.Meeting) schema.Meeting {
	return schema.Meeting{ID: m.ID, WorkspaceID: m.WorkspaceID, RequestID: m.RequestID, Name: m.Name, CreatedBy: m.CreatedBy, EndedAt: m.EndedAt, CreatedAt: m.CreatedAt}
}
func Get(ctx context.Context, w, id string) (q.Meeting, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return q.Meeting{}, err
	}
	if !workspace.ValidID(id) {
		return q.Meeting{}, router.Errorf(404, "meeting not found")
	}
	m, err := q.New(db.From(ctx)).GetMeeting(ctx, q.GetMeetingParams{WorkspaceID: w, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		err = router.Errorf(404, "meeting not found")
	}
	return m, err
}
func List(ctx context.Context, w string) (schema.MeetingList, error) {
	out := schema.MeetingList{Items: []schema.Meeting{}}
	if err := workspace.RequireMember(ctx, w); err != nil {
		return out, err
	}
	_, err := provider.Load()
	out.Configured = err == nil
	rows, err := q.New(db.From(ctx)).ListMeetings(ctx, w)
	for _, m := range rows {
		out.Items = append(out.Items, View(m))
	}
	return out, err
}
func Create(ctx context.Context, w string, in schema.MeetingInput) (schema.Meeting, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return schema.Meeting{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if err := in.Validate(); err != nil {
		return schema.Meeting{}, err
	}
	if _, err := uuid.Parse(in.RequestID); err != nil {
		return schema.Meeting{}, router.Errorf(422, "request ID required")
	}
	if _, err := provider.Load(); err != nil {
		return schema.Meeting{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.Meeting{}, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return schema.Meeting{}, err
	}
	subject := auth.CurrentUser(ctx).ID
	member, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: w, Subject: subject})
	if err != nil {
		return schema.Meeting{}, err
	}
	if !member {
		return schema.Meeting{}, router.Errorf(404, "workspace not found")
	}
	existing, err := queries.FindMeetingRequest(ctx, q.FindMeetingRequestParams{WorkspaceID: w, RequestID: in.RequestID})
	if err == nil {
		return View(existing), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return schema.Meeting{}, err
	}
	count, err := queries.CountActiveMeetings(ctx, w)
	if err != nil {
		return schema.Meeting{}, err
	}
	if count >= 100 {
		return schema.Meeting{}, router.Errorf(409, "workspace meeting limit reached")
	}
	m, err := queries.AddMeeting(ctx, q.AddMeetingParams{WorkspaceID: w, RequestID: in.RequestID, Name: in.Name, CreatedBy: subject})
	if err != nil {
		return schema.Meeting{}, err
	}
	return View(m), tx.Commit(ctx)
}
func Join(ctx context.Context, w, id string) (schema.MeetingAccess, error) {
	out := schema.MeetingAccess{}
	m, err := Get(ctx, w, id)
	if err != nil {
		return out, err
	}
	cfg, err := provider.Load()
	if err != nil {
		return out, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	m, err = queries.LockMeeting(ctx, q.LockMeetingParams{WorkspaceID: w, ID: id})
	if err != nil {
		return out, err
	}
	if m.EndedAt != nil {
		return out, router.Errorf(410, "meeting has ended")
	}
	subject := auth.CurrentUser(ctx).ID
	member, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: w, Subject: subject})
	if err != nil {
		return out, err
	}
	if !member {
		return out, router.Errorf(404, "workspace not found")
	}
	// CreateRoom is idempotent for this server-controlled name, so it also
	// recovers an SFU restart without exposing arbitrary client room names.
	err = cfg.Call(ctx, "CreateRoom", Room(id), map[string]any{"name": Room(id), "empty_timeout": 300, "max_participants": 100}, nil)
	if err != nil {
		return out, err
	}
	email, err := queries.CalendarAccountEmail(ctx, subject)
	if err != nil {
		return out, err
	}
	token, err := cfg.Token(subject, email, Room(id))
	if err != nil {
		return out, err
	}
	expires := time.Now().Add(2 * time.Minute)
	err = queries.SaveMeetingToken(ctx, q.SaveMeetingTokenParams{MeetingID: id, Subject: subject, TokenExpiresAt: expires})
	if err != nil {
		return out, err
	}
	return schema.MeetingAccess{Token: token, ServerURL: cfg.PublicURL, ExpiresAt: expires, Meeting: View(m)}, tx.Commit(ctx)
}
func End(ctx context.Context, w, id string) (schema.Meeting, error) {
	m, err := Get(ctx, w, id)
	if err != nil {
		return schema.Meeting{}, err
	}
	if m.CreatedBy != auth.CurrentUser(ctx).ID {
		if err = workspace.RequireManager(ctx, w); err != nil {
			return schema.Meeting{}, err
		}
	}
	cfg, err := provider.Load()
	if err != nil {
		return schema.Meeting{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.Meeting{}, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	m, err = queries.LockMeeting(ctx, q.LockMeetingParams{WorkspaceID: w, ID: id})
	if err != nil {
		return schema.Meeting{}, err
	}
	if m.EndedAt != nil {
		return View(m), nil
	}
	if err = cfg.Call(ctx, "DeleteRoom", Room(id), map[string]string{"room": Room(id)}, nil); err != nil {
		return schema.Meeting{}, err
	}
	m, err = queries.EndMeeting(ctx, id)
	if err != nil {
		return schema.Meeting{}, err
	}
	return View(m), tx.Commit(ctx)
}
