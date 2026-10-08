package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/mail"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"time"
)

const ReminderJob = "thura.calendar.reminders"

func Remind(ctx context.Context, _ json.RawMessage) error { return RemindAt(ctx, time.Now()) }

// Cursor paging prevents older subscriptions from starving after the first 500.
// The notice ledger and mail queue commit together, including after retries.
func RemindAt(ctx context.Context, now time.Time) error {
	cursor := ""
	queries := q.New(db.From(ctx))
	for {
		rows, err := queries.ListReminderSubscriptions(ctx, cursor)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for _, r := range rows {
			if err = remindOne(ctx, r, now); err != nil {
				return err
			}
			cursor = r.ID
		}
		if len(rows) < 500 {
			return nil
		}
	}
}
func remindOne(ctx context.Context, r q.ListReminderSubscriptionsRow, now time.Time) error {
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	e, err := queries.LockCalendarEvent(ctx, q.LockCalendarEventParams{CalendarID: r.CalendarID, ID: r.EventID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	member, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: r.WorkspaceID, Subject: r.Subject})
	if err != nil {
		return err
	}
	if !member || e.Cancelled || (r.OwnerSubject != nil && *r.OwnerSubject != r.Subject) {
		return nil
	}
	setting, err := queries.GetCalendarReminder(ctx, q.GetCalendarReminderParams{EventID: e.ID, Subject: r.Subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	exceptions, err := queries.ListEventExceptions(ctx, e.ID)
	if err != nil {
		return err
	}
	items, err := Instances(e, "", exceptions, now.Add(-48*time.Hour), now.Add(8*24*time.Hour))
	if err != nil {
		return err
	}
	email, err := queries.CalendarAccountEmail(ctx, r.Subject)
	if err != nil {
		return err
	}
	if email == "" {
		return nil
	}
	for _, i := range items {
		var start time.Time
		if i.AllDay {
			loc, xerr := time.LoadLocation(e.TimeZone)
			if xerr != nil {
				return xerr
			}
			start, err = time.ParseInLocation("2006-01-02 15:04", i.Start+" 09:00", loc)
		} else {
			start, err = time.Parse(time.RFC3339, i.Start)
		}
		if err != nil {
			return err
		}
		due := start.Add(-time.Duration(setting.MinutesBefore) * time.Minute)
		if now.Before(due) || now.Sub(due) > 15*time.Minute {
			continue
		}
		inserted, xerr := queries.AddReminderNotice(ctx, q.AddReminderNoticeParams{EventID: e.ID, Subject: r.Subject, Sequence: e.Sequence, InstanceKey: i.InstanceKey})
		if xerr != nil {
			return xerr
		}
		if inserted == 0 {
			continue
		}
		_, err = mail.From(ctx).SendTx(ctx, tx, mail.Message{To: email, Subject: "Reminder: " + i.Title, Text: fmt.Sprintf("%s\nStarts: %s (%s)\nLocation: %s\n\n%s", i.Title, start.In(mustLocation(e.TimeZone)).Format(time.RFC3339), e.TimeZone, e.Location, mail.From(ctx).Link("/app"))})
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func mustLocation(zone string) *time.Location {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.UTC
	}
	return loc
}
