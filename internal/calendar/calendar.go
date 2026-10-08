package calendar

import (
	"context"
	"errors"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	q "thura/db/queries/gen"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func View(c q.Calendar) schema.Calendar {
	return schema.Calendar{ID: c.ID, WorkspaceID: c.WorkspaceID, Name: c.Name, Color: c.Color, OwnerSubject: c.OwnerSubject, CreatedAt: c.CreatedAt}
}
func ViewEvent(e q.CalendarEvent) schema.CalendarEvent {
	return schema.CalendarEvent{MeetingID: e.MeetingID, ID: e.ID, CalendarID: e.CalendarID, Uid: e.Uid, Organizer: e.Organizer, Title: e.Title, Description: e.Description, Location: e.Location, AllDay: e.AllDay, StartDate: e.StartDate, EndDate: e.EndDate, StartsAt: e.StartsAt, EndsAt: e.EndsAt, TimeZone: e.TimeZone, Rrule: e.Rrule, Sequence: int(e.Sequence), Cancelled: e.Cancelled, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
}
func ViewAttendee(a q.EventAttendee) schema.EventAttendee {
	return schema.EventAttendee{ID: a.ID, EventID: a.EventID, Email: a.Email, Response: schema.Attendance(a.Response), ResponseSequence: int(a.ResponseSequence)}
}
func ViewException(e q.EventException) schema.EventException {
	return schema.EventException{ID: e.ID, EventID: e.EventID, InstanceKey: e.InstanceKey, Cancelled: e.Cancelled, Title: e.Title, StartsAt: e.StartsAt, EndsAt: e.EndsAt, StartDate: e.StartDate, EndDate: e.EndDate}
}
func Access(ctx context.Context, w, id string) (q.Calendar, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return q.Calendar{}, err
	}
	if !workspace.ValidID(id) {
		return q.Calendar{}, router.Errorf(404, "calendar not found")
	}
	subject := auth.CurrentUser(ctx).ID
	c, err := q.New(db.From(ctx)).GetCalendar(ctx, q.GetCalendarParams{WorkspaceID: w, ID: id, OwnerSubject: &subject})
	if errors.Is(err, pgx.ErrNoRows) {
		err = router.Errorf(404, "calendar not found")
	}
	return c, err
}
func Create(ctx context.Context, w string, in schema.CalendarInput) (schema.Calendar, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return schema.Calendar{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if err := in.Validate(); err != nil {
		return schema.Calendar{}, err
	}
	if !colorPattern.MatchString(in.Color) {
		return schema.Calendar{}, router.Errorf(422, "calendar color must be a six-digit hex color")
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.Calendar{}, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return schema.Calendar{}, err
	}
	count, err := queries.CountWorkspaceCalendars(ctx, w)
	if err != nil {
		return schema.Calendar{}, err
	}
	if count >= 100 {
		return schema.Calendar{}, router.Errorf(409, "workspace calendar limit reached")
	}
	var owner *string
	if in.Personal {
		v := auth.CurrentUser(ctx).ID
		owner = &v
	}
	c, err := queries.CreateCalendar(ctx, q.CreateCalendarParams{WorkspaceID: w, Name: in.Name, Color: in.Color, OwnerSubject: owner})
	if err != nil {
		return schema.Calendar{}, err
	}
	return View(c), tx.Commit(ctx)
}
func Get(ctx context.Context, w, calendarID, id string) (q.CalendarEvent, error) {
	if _, err := Access(ctx, w, calendarID); err != nil {
		return q.CalendarEvent{}, err
	}
	if !workspace.ValidID(id) {
		return q.CalendarEvent{}, router.Errorf(404, "event not found")
	}
	e, err := q.New(db.From(ctx)).GetCalendarEvent(ctx, q.GetCalendarEventParams{CalendarID: calendarID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		err = router.Errorf(404, "event not found")
	}
	return e, err
}
func Detail(ctx context.Context, queries *q.Queries, e q.CalendarEvent) (schema.EventDetail, error) {
	out := schema.EventDetail{Event: ViewEvent(e), Attendees: []schema.EventAttendee{}, Exceptions: []schema.EventException{}}
	attendees, err := queries.ListEventAttendees(ctx, e.ID)
	if err != nil {
		return out, err
	}
	exceptions, err := queries.ListEventExceptions(ctx, e.ID)
	if err != nil {
		return out, err
	}
	for _, a := range attendees {
		out.Attendees = append(out.Attendees, ViewAttendee(a))
	}
	for _, ex := range exceptions {
		out.Exceptions = append(out.Exceptions, ViewException(ex))
	}
	return out, nil
}
func attendees(in []string) ([]string, error) {
	if len(in) > 50 {
		return nil, router.Errorf(422, "at most 50 attendees")
	}
	seen := map[string]bool{}
	out := []string{}
	for _, v := range in {
		a, err := mail.ParseAddress(strings.TrimSpace(v))
		if err != nil || len(a.Address) > 254 {
			return nil, router.Errorf(422, "invalid attendee email")
		}
		address := strings.ToLower(a.Address)
		if !seen[address] {
			seen[address] = true
			out = append(out, address)
		}
	}
	sort.Strings(out)
	return out, nil
}
func saveAttendees(ctx context.Context, queries *q.Queries, id string, values []string) error {
	wanted, err := attendees(values)
	if err != nil {
		return err
	}
	existing, err := queries.ListEventAttendees(ctx, id)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, email := range wanted {
		seen[email] = true
		if err = queries.AddEventAttendee(ctx, q.AddEventAttendeeParams{EventID: id, Email: email}); err != nil {
			return err
		}
	}
	for _, a := range existing {
		if !seen[a.Email] {
			if err = queries.DeleteAttendeeGrants(ctx, a.ID); err != nil {
				return err
			}
			if err = queries.DeleteEventAttendee(ctx, q.DeleteEventAttendeeParams{EventID: id, Email: a.Email}); err != nil {
				return err
			}
		}
	}
	return nil
}
func Insert(ctx context.Context, queries *q.Queries, calendarID, uid, organizer string, in schema.EventInput) (q.CalendarEvent, error) {
	e, err := queries.CreateCalendarEvent(ctx, q.CreateCalendarEventParams{CalendarID: calendarID, Uid: uid, Organizer: organizer, Title: in.Title, Description: text(in.Description), Location: text(in.Location), AllDay: in.AllDay, StartDate: text(in.StartDate), EndDate: text(in.EndDate), StartsAt: in.StartsAt, EndsAt: in.EndsAt, TimeZone: in.TimeZone, Rrule: text(in.Rrule)})
	if err == nil {
		err = saveAttendees(ctx, queries, e.ID, in.Attendees)
	}
	return e, err
}
func CreateEvent(ctx context.Context, w, calendarID string, in schema.EventInput) (schema.EventDetail, error) {
	if _, err := Access(ctx, w, calendarID); err != nil {
		return schema.EventDetail{}, err
	}
	in, err := Validate(in)
	if err != nil {
		return schema.EventDetail{}, err
	}
	if _, err = attendees(in.Attendees); err != nil {
		return schema.EventDetail{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.EventDetail{}, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return schema.EventDetail{}, err
	}
	count, err := queries.CountWorkspaceEvents(ctx, w)
	if err != nil {
		return schema.EventDetail{}, err
	}
	if count >= 1000 {
		return schema.EventDetail{}, router.Errorf(409, "workspace event limit reached")
	}
	organizer, err := queries.CalendarAccountEmail(ctx, auth.CurrentUser(ctx).ID)
	if err != nil {
		return schema.EventDetail{}, err
	}
	e, err := Insert(ctx, queries, calendarID, uuid.NewString()+"@thura", organizer, in)
	if err != nil {
		return schema.EventDetail{}, err
	}
	out, err := Detail(ctx, queries, e)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func Lock(ctx context.Context, w, calendarID, id string) (pgx.Tx, *q.Queries, q.CalendarEvent, error) {
	if _, err := Get(ctx, w, calendarID, id); err != nil {
		return nil, nil, q.CalendarEvent{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return nil, nil, q.CalendarEvent{}, err
	}
	queries := q.New(tx)
	e, err := queries.LockCalendarEvent(ctx, q.LockCalendarEventParams{CalendarID: calendarID, ID: id})
	if err != nil {
		return nil, nil, e, errors.Join(err, tx.Rollback(ctx))
	}
	return tx, queries, e, nil
}
func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
func Update(ctx context.Context, w, calendarID, id string, in schema.EventInput) (schema.EventDetail, error) {
	in, err := Validate(in)
	if err != nil {
		return schema.EventDetail{}, err
	}
	if _, err = attendees(in.Attendees); err != nil {
		return schema.EventDetail{}, err
	}
	tx, queries, e, err := Lock(ctx, w, calendarID, id)
	if err != nil {
		return schema.EventDetail{}, err
	}
	defer tx.Rollback(ctx)
	if e.Cancelled || int(e.Sequence) != in.Sequence {
		return schema.EventDetail{}, router.Errorf(409, "event changed or was cancelled; reload before editing")
	}
	changed := e.AllDay != in.AllDay || e.StartDate != text(in.StartDate) || e.EndDate != text(in.EndDate) || !sameTime(e.StartsAt, in.StartsAt) || !sameTime(e.EndsAt, in.EndsAt) || e.TimeZone != in.TimeZone || e.Rrule != text(in.Rrule)
	if changed {
		exceptions, xerr := queries.ListEventExceptions(ctx, id)
		if xerr != nil {
			return schema.EventDetail{}, xerr
		}
		if len(exceptions) > 0 && !in.ResetExceptions {
			return schema.EventDetail{}, router.Errorf(409, "changing the series requires explicitly resetting its exceptions")
		}
		if err = queries.ClearEventExceptions(ctx, id); err != nil {
			return schema.EventDetail{}, err
		}
	}
	e, err = queries.UpdateCalendarEvent(ctx, q.UpdateCalendarEventParams{CalendarID: calendarID, ID: id, Title: in.Title, Description: text(in.Description), Location: text(in.Location), AllDay: in.AllDay, StartDate: text(in.StartDate), EndDate: text(in.EndDate), StartsAt: in.StartsAt, EndsAt: in.EndsAt, TimeZone: in.TimeZone, Rrule: text(in.Rrule)})
	if err == nil {
		err = saveAttendees(ctx, queries, id, in.Attendees)
	}
	if err != nil {
		return schema.EventDetail{}, err
	}
	out, err := Detail(ctx, queries, e)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func Cancel(ctx context.Context, w, calendarID, id string, sequence int) (schema.EventDetail, error) {
	tx, queries, e, err := Lock(ctx, w, calendarID, id)
	if err != nil {
		return schema.EventDetail{}, err
	}
	defer tx.Rollback(ctx)
	if int(e.Sequence) != sequence {
		return schema.EventDetail{}, router.Errorf(409, "event changed; reload before cancelling")
	}
	if !e.Cancelled {
		e, err = queries.CancelCalendarEvent(ctx, id)
	}
	if err != nil {
		return schema.EventDetail{}, err
	}
	out, err := Detail(ctx, queries, e)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
func Exception(ctx context.Context, w, calendarID, id string, in schema.ExceptionInput) (schema.EventDetail, error) {
	tx, queries, e, err := Lock(ctx, w, calendarID, id)
	if err != nil {
		return schema.EventDetail{}, err
	}
	defer tx.Rollback(ctx)
	if e.Cancelled || int(e.Sequence) != in.Sequence {
		return schema.EventDetail{}, router.Errorf(409, "event changed or was cancelled")
	}
	if err = ValidateException(e, in); err != nil {
		return schema.EventDetail{}, err
	}
	_, err = queries.SaveEventException(ctx, q.SaveEventExceptionParams{EventID: id, InstanceKey: in.InstanceKey, Cancelled: in.Cancelled, Title: in.Title, StartsAt: in.StartsAt, EndsAt: in.EndsAt, StartDate: in.StartDate, EndDate: in.EndDate})
	if err == nil {
		e, err = queries.BumpEventSequence(ctx, id)
	}
	if err != nil {
		return schema.EventDetail{}, err
	}
	out, err := Detail(ctx, queries, e)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

// Snapshot reads bounded calendar data in three queries rather than issuing
// two additional queries for every event in a month view or export.
func Snapshot(ctx context.Context, queries *q.Queries, id string) ([]q.CalendarEvent, map[string][]q.EventAttendee, map[string][]q.EventException, error) {
	events, err := queries.ListCalendarEvents(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	attendees, err := queries.ListCalendarAttendees(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	exceptions, err := queries.ListCalendarExceptions(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	a := map[string][]q.EventAttendee{}
	x := map[string][]q.EventException{}
	for _, v := range attendees {
		a[v.EventID] = append(a[v.EventID], v)
	}
	for _, v := range exceptions {
		x[v.EventID] = append(x[v.EventID], v)
	}
	return events, a, x, nil
}
