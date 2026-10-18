package calendar

import (
	"context"
	"errors"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/mail"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/meet"
	"thura/schema"
)

// BindMeeting preserves the event's physical location and updates its version.
func BindMeeting(ctx context.Context, w, calendarID, id string, in schema.BindMeetingInput) (schema.EventDetail, error) {
	if _, err := meet.Get(ctx, w, in.MeetingID); err != nil {
		return schema.EventDetail{}, err
	}
	tx, queries, event, err := Lock(ctx, w, calendarID, id)
	if err != nil {
		return schema.EventDetail{}, err
	}
	defer tx.Rollback(ctx)
	meeting, err := queries.LockMeeting(ctx, q.LockMeetingParams{WorkspaceID: w, ID: in.MeetingID})
	if err != nil {
		return schema.EventDetail{}, err
	}
	if meeting.EndedAt != nil {
		return schema.EventDetail{}, router.Errorf(410, "meeting has ended")
	}
	if event.Cancelled || int(event.Sequence) != in.Sequence {
		return schema.EventDetail{}, router.Errorf(409, "event changed or was cancelled; reload before linking")
	}
	if event.MeetingID == nil || *event.MeetingID != meeting.ID {
		event, err = queries.BindCalendarMeeting(ctx, q.BindCalendarMeetingParams{ID: id, MeetingID: &meeting.ID})
		if err != nil {
			return schema.EventDetail{}, err
		}
	}
	out, err := Detail(ctx, queries, event)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func ExportWorkspaceICS(ctx context.Context, w string, events []q.CalendarEvent, attendees map[string][]q.EventAttendee, exceptions map[string][]q.EventException, method string) ([]byte, error) {
	return exportWorkspaceICS(ctx, q.New(db.From(ctx)), w, events, attendees, exceptions, method)
}
func exportWorkspaceICS(ctx context.Context, queries *q.Queries, w string, events []q.CalendarEvent, attendees map[string][]q.EventAttendee, exceptions map[string][]q.EventException, method string) ([]byte, error) {
	links := map[string]string{}
	for _, event := range events {
		if event.MeetingID != nil {
			links[event.ID] = mail.From(ctx).Link("/app?workspace=" + w + "&meeting=" + *event.MeetingID)
			invite, err := queries.MeetingInviteByEvent(ctx, event.ID)
			if err == nil && invite.MeetingID == *event.MeetingID {
				link, _, xerr := meet.InviteLink(ctx, invite)
				if xerr != nil {
					return nil, xerr
				}
				links[event.ID] = link
			} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
		}
	}
	return ExportICS(events, attendees, exceptions, method, links)
}
