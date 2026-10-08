package calendar

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/mail"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"strings"
	q "thura/db/queries/gen"
	"thura/schema"
	"time"
)

func grantHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func Captured() bool {
	var cfg mail.Config
	if env.Load(".", &cfg) != nil {
		return false
	}
	return cfg.Provider == "log" || cfg.Provider == "outbox"
}

// Invitations are explicitly sent, separately from saving or importing events.
// The event lock serializes sends, edits, and replies. Mail and grants commit together.
func Invite(ctx context.Context, w, c, id string, in schema.CalendarInviteInput) (schema.CalendarDispatchResult, error) {
	out := schema.CalendarDispatchResult{Captured: Captured()}
	if in.InstanceKey != nil && *in.InstanceKey != "" {
		return out, router.Errorf(422, "invitations currently apply to the whole series")
	}
	tx, queries, e, err := Lock(ctx, w, c, id)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if int(e.Sequence) != in.Sequence {
		return out, router.Errorf(409, "event changed; reload before sending")
	}
	dispatch, err := queries.GetCalendarDispatch(ctx, q.GetCalendarDispatchParams{EventID: id, Sequence: e.Sequence})
	if err == nil && time.Since(dispatch.CreatedAt) < 30*24*time.Hour {
		return out, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	if err == nil {
		if err = queries.DeleteCalendarDispatch(ctx, dispatch.ID); err != nil {
			return out, err
		}
	}
	attendees, err := queries.ListEventAttendees(ctx, id)
	if err != nil {
		return out, err
	}
	if len(attendees) == 0 {
		return out, router.Errorf(422, "add attendees before sending invitations")
	}
	exceptions, err := queries.ListEventExceptions(ctx, id)
	if err != nil {
		return out, err
	}
	method := "REQUEST"
	subject := "Invitation: " + e.Title
	if e.Cancelled {
		method = "CANCEL"
		subject = "Cancelled: " + e.Title
	}
	content, err := ExportWorkspaceICS(ctx, w, []q.CalendarEvent{e}, map[string][]q.EventAttendee{id: attendees}, map[string][]q.EventException{id: exceptions}, method)
	if err != nil {
		return out, err
	}
	for _, a := range attendees {
		text := "Calendar event: " + e.Title + "\nOrganizer: " + e.Organizer + "\n\nOpen the attached calendar file."
		if e.MeetingID != nil {
			text += "\n\nMeeting: " + mail.From(ctx).Link("/app?workspace="+w+"&meeting="+*e.MeetingID) + "\nOnly current workspace members can join."
		}
		if !e.Cancelled {
			bytes := make([]byte, 32)
			if _, err = rand.Read(bytes); err != nil {
				return out, err
			}
			token := hex.EncodeToString(bytes)
			if err = queries.DeleteAttendeeGrants(ctx, a.ID); err != nil {
				return out, err
			}
			err = queries.AddCalendarReplyGrant(ctx, q.AddCalendarReplyGrantParams{AttendeeID: a.ID, TokenHash: grantHash(token), Sequence: e.Sequence, ExpiresAt: time.Now().Add(30 * 24 * time.Hour)})
			if err != nil {
				return out, err
			}
			text += "\n\nReply for " + a.Email + ": " + mail.From(ctx).Link("/rsvp#"+token) + "\nThis link grants access to this event only and expires in 30 days."
		}
		_, err = mail.From(ctx).SendTx(ctx, tx, mail.Message{To: a.Email, ReplyTo: e.Organizer, Subject: subject, Text: text, Attachments: []mail.Attachment{{Name: "event.ics", ContentType: "text/calendar; method=" + method + "; charset=utf-8", Data: content}}})
		if err != nil {
			return out, err
		}
		out.Queued++
	}
	if err = queries.CreateCalendarDispatch(ctx, q.CreateCalendarDispatchParams{EventID: id, Sequence: e.Sequence}); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func replyGrant(ctx context.Context, queries *q.Queries, token string) (q.GetCalendarReplyGrantRow, q.CalendarEvent, error) {
	var e q.CalendarEvent
	if len(token) < 20 || len(token) > 200 {
		return q.GetCalendarReplyGrantRow{}, e, router.Errorf(404, "reply link not found")
	}
	g, err := queries.GetCalendarReplyGrant(ctx, grantHash(token))
	if errors.Is(err, pgx.ErrNoRows) {
		err = router.Errorf(404, "reply link not found")
	}
	if err != nil {
		return g, e, err
	}
	e, err = queries.GetReplyEvent(ctx, g.AttendeeID)
	if err != nil {
		return g, e, err
	}
	if !time.Now().Before(g.ExpiresAt) {
		return g, e, router.Errorf(410, "reply link expired")
	}
	if e.Cancelled || e.Sequence != g.Sequence {
		return g, e, router.Errorf(409, "event changed or was cancelled; request a new invitation")
	}
	return g, e, nil
}
func ReplyView(ctx context.Context, token string) (schema.CalendarReplyView, error) {
	queries := q.New(db.From(ctx))
	g, e, err := replyGrant(ctx, queries, token)
	out := schema.CalendarReplyView{}
	if err != nil {
		return out, err
	}
	attendees, err := queries.ListEventAttendees(ctx, e.ID)
	if err != nil {
		return out, err
	}
	for _, a := range attendees {
		if a.ID == g.AttendeeID {
			response := schema.Attendance(a.Response)
			if a.ResponseSequence != e.Sequence {
				response = schema.Attendance("needsaction")
			}
			out = schema.CalendarReplyView{Event: ViewEvent(e), Email: g.Email, InstanceKey: g.InstanceKey, Response: response, ExpiresAt: g.ExpiresAt}
			break
		}
	}
	return out, nil
}
func Reply(ctx context.Context, in schema.CalendarReplyInput) (schema.CalendarReplyView, error) {
	if in.Response != "accepted" && in.Response != "tentative" && in.Response != "declined" {
		return schema.CalendarReplyView{}, router.Errorf(422, "choose accepted, tentative, or declined")
	}
	if in.InstanceKey != nil && *in.InstanceKey != "" {
		return schema.CalendarReplyView{}, router.Errorf(422, "replies currently apply to the whole series")
	}
	queries := q.New(db.From(ctx))
	_, e, err := replyGrant(ctx, queries, in.Token)
	if err != nil {
		return schema.CalendarReplyView{}, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return schema.CalendarReplyView{}, err
	}
	defer tx.Rollback(ctx)
	queries = q.New(tx)
	if _, err = queries.LockCalendarEvent(ctx, q.LockCalendarEventParams{CalendarID: e.CalendarID, ID: e.ID}); err != nil {
		return schema.CalendarReplyView{}, err
	}
	g, e, err := replyGrant(ctx, queries, in.Token)
	if err != nil {
		return schema.CalendarReplyView{}, err
	}
	_, err = queries.ReplyEventAttendee(ctx, q.ReplyEventAttendeeParams{EventID: e.ID, Email: g.Email, Response: q.Attendance(in.Response), ResponseSequence: e.Sequence})
	if err != nil {
		return schema.CalendarReplyView{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return schema.CalendarReplyView{}, err
	}
	return ReplyView(ctx, in.Token)
}
func Reminder(ctx context.Context, w, c, id string) (schema.ReminderSetting, error) {
	if _, err := Get(ctx, w, c, id); err != nil {
		return schema.ReminderSetting{}, err
	}
	r, err := q.New(db.From(ctx)).GetCalendarReminder(ctx, q.GetCalendarReminderParams{EventID: id, Subject: auth.CurrentUser(ctx).ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return schema.ReminderSetting{}, nil
	}
	v := int(r.MinutesBefore)
	return schema.ReminderSetting{MinutesBefore: &v}, err
}
func SetReminder(ctx context.Context, w, c, id string, in schema.ReminderInput) (schema.ReminderSetting, error) {
	if err := in.Validate(); err != nil {
		return schema.ReminderSetting{}, err
	}
	tx, queries, e, err := Lock(ctx, w, c, id)
	if err != nil {
		return schema.ReminderSetting{}, err
	}
	defer tx.Rollback(ctx)
	if e.Cancelled {
		return schema.ReminderSetting{}, router.Errorf(409, "event was cancelled")
	}
	subject := auth.CurrentUser(ctx).ID
	if _, err = queries.LockReminderSubject(ctx, subject); err != nil {
		return schema.ReminderSetting{}, err
	}
	if in.MinutesBefore == nil {
		err = queries.DeleteCalendarReminder(ctx, q.DeleteCalendarReminderParams{EventID: id, Subject: subject})
	} else {
		count, xerr := queries.CountSubjectReminders(ctx, subject)
		if xerr != nil {
			return schema.ReminderSetting{}, xerr
		}
		_, xerr = queries.GetCalendarReminder(ctx, q.GetCalendarReminderParams{EventID: id, Subject: subject})
		if errors.Is(xerr, pgx.ErrNoRows) && count >= 100 {
			return schema.ReminderSetting{}, router.Errorf(409, "at most 100 reminder subscriptions")
		}
		err = queries.SetCalendarReminder(ctx, q.SetCalendarReminderParams{EventID: id, Subject: subject, MinutesBefore: int32(*in.MinutesBefore)})
	}
	if err != nil {
		return schema.ReminderSetting{}, err
	}
	return schema.ReminderSetting(in), tx.Commit(ctx)
}

// Each recipient sees only their own response. The bearer grant supplies identity;
// ordinary incoming mail headers never authorize a calendar reply.
func ReplyICS(ctx context.Context, in schema.CalendarReplyICS) (schema.CalendarReplyView, error) {
	parsed, err := ParseReplyICS(in.Content)
	if err != nil {
		return schema.CalendarReplyView{}, router.Errorf(422, "%s", err)
	}
	g, e, err := replyGrant(ctx, q.New(db.From(ctx)), in.Token)
	if err != nil {
		return schema.CalendarReplyView{}, err
	}
	if parsed.UID != e.Uid || parsed.Sequence != int(e.Sequence) || !strings.EqualFold(parsed.Email, g.Email) {
		return schema.CalendarReplyView{}, router.Errorf(409, "reply does not match this invitation")
	}
	if parsed.Recurrence != "" {
		return schema.CalendarReplyView{}, router.Errorf(422, "occurrence-specific replies are not supported yet")
	}
	return Reply(ctx, schema.CalendarReplyInput{Token: in.Token, Response: schema.Attendance(parsed.Response)})
}
