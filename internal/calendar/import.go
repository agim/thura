package calendar

import (
	"context"
	"errors"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/schema"
)

func Import(ctx context.Context, w, calendarID string, raw []byte, zone string) (schema.CalendarImportResult, error) {
	if _, err := Access(ctx, w, calendarID); err != nil {
		return schema.CalendarImportResult{}, err
	}
	records, method, err := ParseICS(raw, zone)
	if err != nil {
		return schema.CalendarImportResult{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.CalendarImportResult{}, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return schema.CalendarImportResult{}, err
	}
	count, err := queries.CountWorkspaceEvents(ctx, w)
	if err != nil {
		return schema.CalendarImportResult{}, err
	}
	organizer, err := queries.CalendarAccountEmail(ctx, auth.CurrentUser(ctx).ID)
	if err != nil {
		return schema.CalendarImportResult{}, err
	}
	out := schema.CalendarImportResult{}
	bases := map[string]q.CalendarEvent{}
	for _, r := range records {
		if r.Instance != "" {
			continue
		}
		e, eerr := queries.FindCalendarEventUID(ctx, q.FindCalendarEventUIDParams{CalendarID: calendarID, Uid: r.UID})
		exists := eerr == nil
		if eerr != nil && !errors.Is(eerr, pgx.ErrNoRows) {
			return out, eerr
		}
		cancelled := r.Cancelled || method == "CANCEL"
		if exists && int(e.Sequence) >= r.Sequence {
			out.Ignored++
			bases[r.UID] = e
			continue
		}
		if !exists && cancelled {
			out.Ignored++
			continue
		}
		if !exists {
			if count >= 1000 {
				return out, router.Errorf(409, "workspace event limit reached")
			}
			count++
			owner := r.Organizer
			if owner == "" {
				owner = organizer
			}
			e, err = Insert(ctx, queries, calendarID, r.UID, owner, r.Input)
			if err == nil {
				e, err = queries.SetImportedEventSequence(ctx, q.SetImportedEventSequenceParams{ID: e.ID, Sequence: int32(r.Sequence), Cancelled: cancelled})
			}
		} else {
			e, err = queries.LockCalendarEvent(ctx, q.LockCalendarEventParams{CalendarID: calendarID, ID: e.ID})
			if err != nil {
				return out, err
			}
			in := r.Input
			if cancelled {
				in = eventInput(e)
				in.Title = e.Title
				in.Description = &e.Description
				in.Location = &e.Location
			}
			owner := r.Organizer
			if owner == "" {
				owner = e.Organizer
			}
			e, err = queries.ImportCalendarEvent(ctx, q.ImportCalendarEventParams{CalendarID: calendarID, ID: e.ID, Organizer: owner, Title: in.Title, Description: text(in.Description), Location: text(in.Location), AllDay: in.AllDay, StartDate: text(in.StartDate), EndDate: text(in.EndDate), StartsAt: in.StartsAt, EndsAt: in.EndsAt, TimeZone: in.TimeZone, Rrule: text(in.Rrule), Sequence: int32(r.Sequence), Cancelled: cancelled})
			if err == nil && !cancelled {
				err = saveAttendees(ctx, queries, e.ID, in.Attendees)
			}
			if err == nil {
				err = queries.ClearEventExceptions(ctx, e.ID)
			}
		}
		if err != nil {
			return out, err
		}
		bases[r.UID] = e
		for email, response := range r.Responses {
			if _, err = queries.ReplyEventAttendee(ctx, q.ReplyEventAttendeeParams{EventID: e.ID, Email: email, Response: response, ResponseSequence: e.Sequence}); err != nil {
				return out, err
			}
		}
		for _, instance := range r.Exdates {
			ex := schema.ExceptionInput{InstanceKey: instance, Cancelled: true}
			if err = ValidateException(e, ex); err != nil {
				return out, err
			}
			if _, err = queries.SaveEventException(ctx, q.SaveEventExceptionParams{EventID: e.ID, InstanceKey: instance, Cancelled: true}); err != nil {
				return out, err
			}
		}
		out.Imported++
	}
	for _, r := range records {
		if r.Instance == "" {
			continue
		}
		e, ok := bases[r.UID]
		if !ok {
			e, err = queries.FindCalendarEventUID(ctx, q.FindCalendarEventUIDParams{CalendarID: calendarID, Uid: r.UID})
			if err != nil {
				return out, router.Errorf(422, "recurrence exception has no matching series")
			}
		}
		if r.Sequence < int(e.Sequence) {
			out.Ignored++
			continue
		}
		if r.Sequence > int(e.Sequence) {
			return out, router.Errorf(409, "exception sequence must match the imported series")
		}
		var title *string
		if r.HasTitle {
			title = &r.Input.Title
		}
		ex := schema.ExceptionInput{InstanceKey: r.Instance, Cancelled: r.Cancelled, Title: title}
		if !ex.Cancelled {
			if e.AllDay {
				ex.StartDate, ex.EndDate = r.Input.StartDate, r.Input.EndDate
			} else {
				ex.StartsAt, ex.EndsAt = r.Input.StartsAt, r.Input.EndsAt
			}
		}
		if err = ValidateException(e, ex); err != nil {
			return out, err
		}
		if _, err = queries.SaveEventException(ctx, q.SaveEventExceptionParams{EventID: e.ID, InstanceKey: r.Instance, Cancelled: ex.Cancelled, Title: ex.Title, StartsAt: ex.StartsAt, EndsAt: ex.EndsAt, StartDate: ex.StartDate, EndDate: ex.EndDate}); err != nil {
			return out, err
		}
		out.Imported++
	}
	return out, tx.Commit(ctx)
}
