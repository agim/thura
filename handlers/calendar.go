package handlers

import (
	"context"
	"encoding/base64"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"io"
	"strconv"
	q "thura/db/queries/gen"
	"thura/internal/calendar"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func ListCalendars(ctx context.Context, r *router.Request[router.None]) (schema.CalendarList, error) {
	w := r.Param("workspaceId")
	if err := workspace.RequireMember(ctx, w); err != nil {
		return schema.CalendarList{}, err
	}
	subject := auth.CurrentUser(ctx).ID
	rows, err := q.New(db.From(ctx)).ListCalendars(ctx, q.ListCalendarsParams{WorkspaceID: w, OwnerSubject: &subject})
	out := schema.CalendarList{Items: []schema.Calendar{}}
	for _, c := range rows {
		out.Items = append(out.Items, calendar.View(c))
	}
	return out, err
}
func CreateCalendar(ctx context.Context, r *router.Request[schema.CalendarInput]) (schema.Calendar, error) {
	return calendar.Create(ctx, r.Param("workspaceId"), r.Body)
}
func ListCalendarEvents(ctx context.Context, r *router.Request[router.None]) (schema.CalendarEvents, error) {
	c, err := calendar.Access(ctx, r.Param("workspaceId"), r.Param("calendarId"))
	if err != nil {
		return schema.CalendarEvents{}, err
	}
	queries := q.New(db.From(ctx))
	events, attendees, exceptions, err := calendar.Snapshot(ctx, queries, c.ID)
	out := schema.CalendarEvents{Items: []schema.EventDetail{}}
	if err != nil {
		return out, err
	}
	for _, e := range events {
		d := schema.EventDetail{Event: calendar.ViewEvent(e), Attendees: []schema.EventAttendee{}, Exceptions: []schema.EventException{}}
		for _, v := range attendees[e.ID] {
			d.Attendees = append(d.Attendees, calendar.ViewAttendee(v))
		}
		for _, v := range exceptions[e.ID] {
			d.Exceptions = append(d.Exceptions, calendar.ViewException(v))
		}
		out.Items = append(out.Items, d)
	}
	return out, nil
}
func CalendarInstances(ctx context.Context, r *router.Request[router.None]) (schema.CalendarInstances, error) {
	c, err := calendar.Access(ctx, r.Param("workspaceId"), r.Param("calendarId"))
	if err != nil {
		return schema.CalendarInstances{}, err
	}
	from, err := time.Parse(time.RFC3339, r.Query("from"))
	if err != nil {
		return schema.CalendarInstances{}, router.Errorf(422, "from must be an ISO timestamp")
	}
	to, err := time.Parse(time.RFC3339, r.Query("to"))
	if err != nil || !to.After(from) || to.Sub(from) > 93*24*time.Hour || from.Year() < 2000 || to.Year() > 2100 {
		return schema.CalendarInstances{}, router.Errorf(422, "calendar range must be ordered and at most 93 days")
	}
	queries := q.New(db.From(ctx))
	events, _, exceptions, err := calendar.Snapshot(ctx, queries, c.ID)
	out := schema.CalendarInstances{Items: []schema.EventInstance{}}
	if err != nil {
		return out, err
	}
	for _, e := range events {
		items, eerr := calendar.Instances(e, c.Color, exceptions[e.ID], from, to)
		if eerr != nil {
			return out, eerr
		}
		out.Items = append(out.Items, items...)
		if len(out.Items) > 5000 {
			return out, router.Errorf(422, "calendar range exceeds 5000 occurrences; narrow the view")
		}
	}
	return out, nil
}
func GetCalendarEvent(ctx context.Context, r *router.Request[router.None]) (schema.EventDetail, error) {
	e, err := calendar.Get(ctx, r.Param("workspaceId"), r.Param("calendarId"), r.Param("id"))
	if err != nil {
		return schema.EventDetail{}, err
	}
	return calendar.Detail(ctx, q.New(db.From(ctx)), e)
}
func CreateCalendarEvent(ctx context.Context, r *router.Request[schema.EventInput]) (schema.EventDetail, error) {
	return calendar.CreateEvent(ctx, r.Param("workspaceId"), r.Param("calendarId"), r.Body)
}
func UpdateCalendarEvent(ctx context.Context, r *router.Request[schema.EventInput]) (schema.EventDetail, error) {
	return calendar.Update(ctx, r.Param("workspaceId"), r.Param("calendarId"), r.Param("id"), r.Body)
}
func CancelCalendarEvent(ctx context.Context, r *router.Request[router.None]) (schema.EventDetail, error) {
	sequence, err := strconv.Atoi(r.Query("sequence"))
	if err != nil || sequence < 0 {
		return schema.EventDetail{}, router.Errorf(422, "current event sequence required")
	}
	return calendar.Cancel(ctx, r.Param("workspaceId"), r.Param("calendarId"), r.Param("id"), sequence)
}
func ChangeCalendarOccurrence(ctx context.Context, r *router.Request[schema.ExceptionInput]) (schema.EventDetail, error) {
	return calendar.Exception(ctx, r.Param("workspaceId"), r.Param("calendarId"), r.Param("id"), r.Body)
}
func ImportCalendar(ctx context.Context, r *router.Request[router.File]) (schema.CalendarImportResult, error) {
	raw, err := io.ReadAll(r.Body.Body)
	if err != nil {
		return schema.CalendarImportResult{}, err
	}
	zone := r.Query("timeZone")
	if zone == "" {
		zone = "America/New_York"
	}
	return calendar.Import(ctx, r.Param("workspaceId"), r.Param("calendarId"), raw, zone)
}
func ExportCalendar(ctx context.Context, r *router.Request[router.None]) (schema.FileContent, error) {
	c, err := calendar.Access(ctx, r.Param("workspaceId"), r.Param("calendarId"))
	if err != nil {
		return schema.FileContent{}, err
	}
	queries := q.New(db.From(ctx))
	events, attendees, exceptions, err := calendar.Snapshot(ctx, queries, c.ID)
	if err != nil {
		return schema.FileContent{}, err
	}
	raw, err := calendar.ExportICS(events, attendees, exceptions, "PUBLISH")
	return schema.FileContent{Name: c.ID + ".ics", ContentType: "text/calendar; charset=utf-8", Data: base64.StdEncoding.EncodeToString(raw)}, err
}

func SendCalendarInvitations(ctx context.Context, r *router.Request[schema.CalendarInviteInput]) (schema.CalendarDispatchResult, error) {
	return calendar.Invite(ctx, r.Param("workspaceId"), r.Param("calendarId"), r.Param("id"), r.Body)
}
func OpenCalendarReply(ctx context.Context, r *router.Request[schema.OpenShareInput]) (schema.CalendarReplyView, error) {
	return calendar.ReplyView(ctx, r.Body.Token)
}
func ReplyCalendar(ctx context.Context, r *router.Request[schema.CalendarReplyInput]) (schema.CalendarReplyView, error) {
	return calendar.Reply(ctx, r.Body)
}
func ReplyCalendarICS(ctx context.Context, r *router.Request[schema.CalendarReplyICS]) (schema.CalendarReplyView, error) {
	return calendar.ReplyICS(ctx, r.Body)
}
func GetCalendarReminder(ctx context.Context, r *router.Request[router.None]) (schema.ReminderSetting, error) {
	return calendar.Reminder(ctx, r.Param("workspaceId"), r.Param("calendarId"), r.Param("id"))
}
func SetCalendarReminder(ctx context.Context, r *router.Request[schema.ReminderInput]) (schema.ReminderSetting, error) {
	return calendar.SetReminder(ctx, r.Param("workspaceId"), r.Param("calendarId"), r.Param("id"), r.Body)
}
