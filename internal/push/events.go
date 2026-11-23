package push

import (
	"context"
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
)

func link(path string, params ...string) string {
	v := url.Values{}
	for i := 0; i+1 < len(params); i += 2 {
		if params[i+1] != "" {
			v.Set(params[i], params[i+1])
		}
	}
	return path + "?" + v.Encode()
}

// senderName is the display name of a From header, else its address.
func senderName(from string) string {
	if a, err := mail.ParseAddress(from); err == nil {
		if strings.TrimSpace(a.Name) != "" {
			return a.Name
		}
		return a.Address
	}
	return strings.TrimSpace(from)
}

// MailArrived notifies the readers of a mailbox that a message reached its
// Inbox: the owner of a personal mailbox, every member for a shared one
// (the mailaccess.Owns rule). Mail in Spam, held for review or anywhere but
// the Inbox never notifies; its readers' logs say it was skipped. The
// preview is the sender's name and the subject; the body never leaves the
// server.
func MailArrived(ctx context.Context, tx pgx.Tx, itemID string, person bool) error {
	return mailEvent(ctx, tx, itemID, person, "")
}

// MailHeld records, for the readers of a mailbox, that a message was held
// (Spam, quarantine) instead of reaching the Inbox, so "Recent
// notifications" can say why it did not notify. Nothing is sent.
func MailHeld(ctx context.Context, tx pgx.Tx, itemID string) error {
	return mailEvent(ctx, tx, itemID, false, SkipSpam)
}

// MailFiltered records, for the readers of a mailbox, that one of its
// filters took a message out of the Inbox as it arrived. Nothing is sent.
func MailFiltered(ctx context.Context, tx pgx.Tx, itemID string) error {
	return mailEvent(ctx, tx, itemID, false, SkipFiltered)
}

func mailEvent(ctx context.Context, tx pgx.Tx, itemID string, person bool, skip string) error {
	queries := q.New(tx)
	m, err := queries.PushMailArrival(ctx, itemID)
	if err != nil {
		return err
	}
	var recipients []string
	if m.Private {
		if m.OwnerSubject == nil {
			return nil
		}
		recipients = []string{*m.OwnerSubject}
	} else if recipients, err = queries.ListPushWorkspaceMembers(ctx, m.WorkspaceID); err != nil {
		return err
	}
	subject := m.Subject
	if strings.TrimSpace(subject) == "" {
		subject = "(no subject)"
	}
	e := Event{
		Kind: KindMail, Title: senderName(m.FromAddress), Body: subject, Person: person,
		URL:  link("/app/mail", "workspace", m.WorkspaceID, "mailbox", m.MailboxID, "folder", "inbox", "message", m.ID),
		Tag:  "mail-" + m.ID,
		View: &View{Path: "/app/mail", Params: map[string]string{"folder": "inbox"}, Defaults: map[string]string{"folder": "inbox"}, Loose: map[string]string{"workspace": m.WorkspaceID, "mailbox": m.MailboxID}},
	}
	if skip == "" && m.Folder != q.MailFolderInbox {
		skip = SkipSpam
	}
	if skip != "" {
		e.URL = link("/app/mail", "workspace", m.WorkspaceID, "mailbox", m.MailboxID, "folder", string(m.Folder), "message", m.ID)
		return Skip(ctx, tx, m.WorkspaceID, recipients, e, skip)
	}
	_, err = Notify(ctx, tx, m.WorkspaceID, recipients, e)
	return err
}

// ChatMessage notifies the members in a room (those who joined it) of a
// message another member sent through Thura.
func ChatMessage(ctx context.Context, w, roomID, roomName string, direct bool, actor, text string) {
	queries := q.New(db.From(ctx))
	recipients, err := queries.ListPushChatParticipants(ctx, q.ListPushChatParticipantsParams{WorkspaceID: w, ID: roomID})
	if err != nil {
		lidza.Log(ctx).Warn("push notification not queued", "error", err, "kind", KindChat, "workspace", w)
		return
	}
	name, err := queries.PushAccountName(ctx, actor)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		lidza.Log(ctx).Warn("push sender name unavailable", "error", err, "workspace", w)
	}
	title := name
	if title == "" {
		title = "New message"
	}
	if !direct && roomName != "" {
		title += " in " + roomName
	}
	Safe(ctx, w, recipients, Event{
		Kind: KindChat, Title: title, Body: text, Actor: actor,
		URL:  link("/app/chat", "workspace", w, "room", roomID),
		Tag:  "chat-" + roomID,
		View: &View{Path: "/app/chat", Params: map[string]string{"room": roomID}, Loose: map[string]string{"workspace": w}},
	})
}

// ExternalChat notifies the readers of an external address (its owner, or
// every member for a shared one) of a new chat request or of a message in an
// accepted external conversation. The remote text is never previewed: a
// request is unreviewed content from another server.
func ExternalChat(ctx context.Context, tx pgx.Tx, w, conversationID string, owner *string, remote string, request bool) error {
	queries := q.New(tx)
	var recipients []string
	var err error
	if owner != nil {
		recipients = []string{*owner}
	} else if recipients, err = queries.ListPushWorkspaceMembers(ctx, w); err != nil {
		return err
	}
	e := Event{Kind: KindChat, Title: remote, Body: "New external message",
		URL:  link("/app/chat", "workspace", w, "external", conversationID),
		Tag:  "external-" + conversationID,
		View: &View{Path: "/app/chat", Params: map[string]string{"external": conversationID}, Loose: map[string]string{"workspace": w}},
	}
	if request {
		e.Kind, e.Title, e.Body = KindChatRequest, "Chat request from "+remote, "Open Chat to accept or decline."
	}
	_, err = Notify(ctx, tx, w, recipients, e)
	return err
}

// CalendarReminder notifies a member of their own event reminder, next to
// the reminder email. A reminder is never suppressed by an open tab.
func CalendarReminder(ctx context.Context, tx pgx.Tx, w, subject, calendarID, eventID, instance, title string, start time.Time, zone string) (int, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}
	return Notify(ctx, tx, w, []string{subject}, Event{
		Kind: KindCalendar, Title: "Reminder: " + title, Body: "Starts " + start.In(loc).Format("Mon Jan 2, 15:04") + " (" + loc.String() + ")",
		URL: link("/app/calendar", "workspace", w, "calendar", calendarID, "event", eventID, "instance", instance),
		Tag: "reminder-" + eventID + "-" + instance, TTL: 30 * time.Minute,
	})
}

// MeetingStarting notifies invited members (attendees whose address is a
// member's account) that a meeting they were just invited to starts now.
func MeetingStarting(ctx context.Context, tx pgx.Tx, w, actor, meetingID, title string, emails []string) error {
	lower := make([]string, 0, len(emails))
	for _, e := range emails {
		lower = append(lower, strings.ToLower(strings.TrimSpace(e)))
	}
	queries := q.New(tx)
	recipients, err := queries.PushAccountsByEmail(ctx, q.PushAccountsByEmailParams{Scope: w, Emails: lower})
	if err != nil || len(recipients) == 0 {
		return err
	}
	_, err = Notify(ctx, tx, w, recipients, Event{
		Kind: KindMeet, Title: "Meeting starting: " + title, Body: "You are invited. Tap to join.", Actor: actor,
		URL: link("/app/meet", "workspace", w, "meeting", meetingID),
		Tag: "meet-" + meetingID, TTL: 15 * time.Minute,
		View: &View{Path: "/app/meet", Params: map[string]string{"meeting": meetingID}, Loose: map[string]string{"workspace": w}},
	})
	return err
}
