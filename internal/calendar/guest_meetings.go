package calendar

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/meet"
	provider "thura/internal/providers/livekit"
	"thura/schema"
)

// Called inside the event transaction: no orphan room or partially saved invite.
func prepareMeeting(ctx context.Context, queries *q.Queries, w string, e q.CalendarEvent, in schema.EventInput) (q.CalendarEvent, error) {
	invite, err := queries.MeetingInviteByEvent(ctx, e.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		if in.CreateMeeting == nil || !*in.CreateMeeting {
			return e, nil
		}
		if e.MeetingID != nil {
			return e, router.Errorf(409, "this event already has a workspace meeting")
		}
		if _, err = provider.Load(); err != nil {
			return e, err
		}
		if _, err = queries.LockWorkspace(ctx, w); err != nil {
			return e, err
		}
		count, xerr := queries.CountActiveMeetings(ctx, w)
		if xerr != nil {
			return e, xerr
		}
		if count >= 100 {
			return e, router.Errorf(409, "workspace meeting limit reached")
		}
		name := []rune(e.Title)
		if len(name) > 200 {
			name = name[:200]
		}
		m, xerr := queries.AddMeeting(ctx, q.AddMeetingParams{WorkspaceID: w, RequestID: uuid.NewString(), Name: string(name), CreatedBy: auth.CurrentUser(ctx).ID})
		if xerr != nil {
			return e, xerr
		}
		invite, err = meet.NewInvite(ctx, queries, m.ID, e.ID, time.Now())
	}
	if err != nil {
		return e, err
	}
	// A legacy rebind must not silently re-enable a grant for a different room.
	if e.MeetingID != nil && *e.MeetingID != invite.MeetingID {
		return e, router.Errorf(409, "event meeting changed; reload before editing")
	}
	start, end, err := timing(in)
	if err != nil {
		return e, err
	}
	rule, err := Rule(text(in.Rrule), start, in.AllDay)
	if err != nil {
		return e, err
	}
	last := start
	if rule != nil {
		next := rule.Iterator()
		for n := 0; n < 1000; n++ {
			v, ok := next()
			if !ok {
				break
			}
			last = v
		}
	}
	lastEnd := last.Add(end.Sub(start))
	exceptions, xerr := queries.ListEventExceptions(ctx, e.ID)
	if xerr != nil {
		return e, xerr
	}
	for _, ex := range exceptions {
		if ex.Cancelled {
			continue
		}
		if ex.EndsAt != nil && ex.EndsAt.After(lastEnd) {
			lastEnd = *ex.EndsAt
		}
		if ex.EndDate != nil && *ex.EndDate != "" {
			date, xerr := time.Parse("2006-01-02", *ex.EndDate)
			if xerr == nil && date.After(lastEnd) {
				lastEnd = date
			}
		}
	}
	expires := lastEnd.Add(24 * time.Hour)
	if err = queries.UpdateMeetingInviteExpiry(ctx, q.UpdateMeetingInviteExpiryParams{ID: invite.ID, ExpiresAt: expires}); err != nil {
		return e, err
	}
	instructions, err := meet.InviteInstructions(ctx, invite, queries)
	if err != nil {
		return e, err
	}
	link, _, err := meet.InviteLink(ctx, invite, queries)
	if err != nil {
		return e, err
	}
	description := e.Description
	if at := strings.Index(description, "[Thura Meet]"); at >= 0 {
		if end := strings.Index(description[at:], "[/Thura Meet]"); end >= 0 {
			description = description[:at] + description[at+end+len("[/Thura Meet]"):]
		}
	}
	description = strings.TrimSpace(description)
	if e.Location != "" && e.Location != link {
		description += "\n\nLocation: " + e.Location
	}
	description = strings.TrimSpace(description + "\n\n" + instructions)
	if len([]rune(description)) > 20000 {
		return e, router.Errorf(422, "description is too long to include meeting instructions")
	}
	return queries.SetEventMeetingDetails(ctx, q.SetEventMeetingDetailsParams{ID: e.ID, MeetingID: &invite.MeetingID, Location: link, Description: description})
}

func finishEvent(ctx context.Context, tx pgx.Tx, queries *q.Queries, w string, e q.CalendarEvent, in schema.EventInput) (schema.EventDetail, error) {
	var err error
	e, err = prepareMeeting(ctx, queries, w, e, in)
	if err != nil {
		return schema.EventDetail{}, err
	}
	out, err := Detail(ctx, queries, e)
	if err != nil {
		return out, err
	}
	if in.SendInvitations != nil && *in.SendInvitations {
		dispatch, xerr := inviteTx(ctx, tx, queries, w, e)
		if xerr != nil {
			return out, xerr
		}
		out.Dispatch = &dispatch
	}
	return out, tx.Commit(ctx)
}
