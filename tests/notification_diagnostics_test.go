package tests

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"thura/internal/push"
	"thura/internal/workspace"
	"thura/schema"
)

// claimPushJobs runs each pending delivery job once as the queue would: it
// claims the job (counting the attempt), runs push.Send and settles it
// (done, pending again for a retry, or failed once the attempts are spent).
// It returns the number of handler errors.
func claimPushJobs(t *testing.T, n *mailNode) int {
	t.Helper()
	ctx := n.server.Context()
	pool := db.From(ctx)
	rows, err := pool.Query(ctx, "SELECT id FROM job WHERE kind=$1 AND state='pending' ORDER BY created_at, id", push.SendJob)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	failures := 0
	for _, id := range ids {
		var raw []byte
		var attempts, max int
		if err = pool.QueryRow(ctx, "UPDATE job SET state='running', attempts=attempts+1 WHERE id=$1 RETURNING payload, attempts, max_attempts", id).Scan(&raw, &attempts, &max); err != nil {
			t.Fatal(err)
		}
		state := "done"
		if err = push.Send(ctx, json.RawMessage(raw)); err != nil {
			failures++
			state = "pending"
			if attempts >= max {
				state = "failed"
			}
		}
		if _, err = pool.Exec(ctx, "UPDATE job SET state=$2 WHERE id=$1", id, state); err != nil {
			t.Fatal(err)
		}
	}
	return failures
}

func notificationSettings(t *testing.T, n *mailNode) schema.NotificationSettings {
	t.Helper()
	var out schema.NotificationSettings
	n.request(t, "GET", "/api/v1/workspaces/"+n.workspace.ID+"/notifications", nil, &out, 200)
	return out
}

func TestPushDeliveryStatusIsRecordedPerDevice(t *testing.T) {
	n := pushNode(t, "push-status.example")
	service := newPushService(t)
	base := "/api/v1/workspaces/" + n.workspace.ID + "/notifications"
	settings := n.subscribePush(t, service.server.URL+"/device", 200)
	device := settings.Devices[0]
	if device.LastAttemptAt != nil || device.LastUsedAt != nil || device.DeliveredCount != 0 || device.LastError != "" {
		t.Fatalf("fresh device: %+v", device)
	}

	// 2xx: delivered, counted, and the log entry says so.
	n.request(t, "POST", base+"/test", nil, nil, 200)
	if failures := claimPushJobs(t, n); failures != 0 {
		t.Fatalf("delivery failed %d times", failures)
	}
	settings = notificationSettings(t, n)
	device = settings.Devices[0]
	if device.LastAttemptAt == nil || device.LastUsedAt == nil || device.DeliveredCount != 1 || device.FailedCount != 0 || device.LastKind != "test" || device.LastError != "" {
		t.Fatalf("after delivery: %+v", device)
	}
	if len(settings.Recent) != 1 || settings.Recent[0].Status != "delivered" || settings.Recent[0].Kind != "test" {
		t.Fatalf("recent after delivery: %+v", settings.Recent)
	}

	// 5xx: every attempt records the error; the failure counts once, when
	// the queue gives up after the fifth attempt.
	service.status.Store(503)
	n.request(t, "POST", base+"/test", nil, nil, 200)
	for attempt := 1; attempt <= 5; attempt++ {
		if failures := claimPushJobs(t, n); failures != 1 {
			t.Fatalf("attempt %d: %d failures", attempt, failures)
		}
		device = notificationSettings(t, n).Devices[0]
		if !strings.Contains(device.LastError, "503 Service Unavailable") || device.LastErrorKind != "status" || device.LastErrorAt == nil {
			t.Fatalf("attempt %d error: %+v", attempt, device)
		}
		want := 0
		if attempt == 5 {
			want = 1
		}
		if device.FailedCount != want || device.DeliveredCount != 1 {
			t.Fatalf("attempt %d counts: delivered %d failed %d", attempt, device.DeliveredCount, device.FailedCount)
		}
	}
	if len(pushJobs(t, n)) != 0 {
		t.Fatal("a delivery is still queued after five attempts")
	}
	settings = notificationSettings(t, n)
	if r := settings.Recent[0]; r.Status != "failed" || !strings.HasPrefix(r.Error, "Gave up after 5 attempts.") || !strings.Contains(r.Error, "503") {
		t.Fatalf("recent after giving up: %+v", r)
	}

	// The per-device test answers in the request.
	service.status.Store(201)
	var result schema.PushDeviceTestResult
	n.request(t, "POST", base+"/devices/"+device.ID+"/test", nil, &result, 200)
	if !result.Delivered || result.Status != 201 || result.Service != "the local test push service" || result.Error != "" {
		t.Fatalf("device test: %+v", result)
	}
	if d := result.Settings.Devices[0]; d.DeliveredCount != 2 || d.LastUsedAt == nil || d.LastErrorAt == nil || d.LastUsedAt.Before(*d.LastErrorAt) {
		t.Fatalf("device after test: %+v", d)
	}
	service.status.Store(400)
	n.request(t, "POST", base+"/devices/"+device.ID+"/test", nil, &result, 200)
	if result.Delivered || result.Status != 400 || result.ErrorKind != "status" || !strings.Contains(result.Error, "400 Bad Request") {
		t.Fatalf("refused test: %+v", result)
	}

	// 410: the subscription is removed and why is kept for Settings.
	service.status.Store(410)
	n.request(t, "POST", base+"/test", nil, nil, 200)
	if failures := claimPushJobs(t, n); failures != 0 {
		t.Fatalf("410 retried: %d", failures)
	}
	settings = notificationSettings(t, n)
	if len(settings.Devices) != 0 {
		t.Fatalf("gone device kept: %+v", settings.Devices)
	}
	if len(settings.Removed) != 1 || settings.Removed[0].EndpointHash != device.EndpointHash || settings.Removed[0].Label != "Chrome on Linux" ||
		settings.Removed[0].Error != "The push service refused it (410 Gone): this device unsubscribed. Turn notifications on again here." {
		t.Fatalf("removed devices: %+v", settings.Removed)
	}
	if r := settings.Recent[0]; r.Status != "failed" || !strings.Contains(r.Error, "410 Gone") {
		t.Fatalf("recent after 410: %+v", r)
	}
	// Turning notifications on again clears the reason.
	service.status.Store(201)
	if settings = n.subscribePush(t, service.server.URL+"/device", 200); len(settings.Removed) != 0 || len(settings.Devices) != 1 {
		t.Fatalf("resubscribed: %+v", settings)
	}

	// Nobody else can test this member's device.
	other := n.addMember(t, "colleague@push-status.example")
	n.login(t, other.Email)
	n.request(t, "POST", base+"/devices/"+settings.Devices[0].ID+"/test", nil, nil, 404)
	if s := notificationSettings(t, n); len(s.Removed) != 0 || len(s.Recent) != 0 {
		t.Fatalf("colleague sees: %+v", s)
	}
}

func TestRecentNotificationsExplainEverySkip(t *testing.T) {
	n := pushNode(t, "push-recent.example")
	service := newPushService(t)
	ctx := n.server.Context()
	w := n.workspace.ID
	base := "/api/v1/workspaces/" + w + "/notifications"
	n.subscribePush(t, service.server.URL+"/owner", 200)
	reasons := func() map[string]schema.NotificationLogEntry {
		out := map[string]schema.NotificationLogEntry{}
		for _, r := range notificationSettings(t, n).Recent {
			if _, seen := out[r.Reason]; !seen {
				out[r.Reason] = r
			}
		}
		return out
	}

	// Not from a real person, with the default "Only mail from real people".
	item := receiveAbuse(t, n, "recent-unknown", "Stranger <stranger@outside.example>", "Hello there", "", 200)
	r, ok := reasons()["people_only"]
	if !ok || r.Status != "skipped" || r.Kind != "mail" || !strings.Contains(r.Link, "message="+item.ID) {
		t.Fatalf("people-only skip: %+v", reasons())
	}
	// Held as spam.
	settings := abuseSettings(t, n)
	settings.ApproveUnknown = true
	saveAbuseSettings(t, n, settings)
	held := receiveAbuse(t, n, "recent-held", "Unknown <unknown@elsewhere.example>", "Please read", "", 200)
	if r, ok = reasons()["spam"]; !ok || r.Status != "skipped" || !strings.Contains(r.Link, "message="+held.ID) {
		t.Fatalf("spam skip: %+v", reasons())
	}
	settings = abuseSettings(t, n)
	settings.ApproveUnknown = false
	saveAbuseSettings(t, n, settings)
	// The member's own action.
	if _, err := push.Notify(ctx, nil, w, []string{n.owner.Subject}, push.Event{Kind: push.KindChat, Title: "Mine", URL: "/app/chat?room=1", Actor: n.owner.Subject}); err != nil {
		t.Fatal(err)
	}
	if r, ok = reasons()["own"]; !ok || r.Kind != "chat" {
		t.Fatalf("own skip: %+v", reasons())
	}
	// A kind turned off.
	n.request(t, "PUT", base+"/preferences", schema.NotificationPreferences{Mail: true, MailPeopleOnly: true, Chat: false, ChatRequests: true, Calendar: true, Meet: true}, nil, 200)
	if _, err := push.Notify(ctx, nil, w, []string{n.owner.Subject}, push.Event{Kind: push.KindChat, Title: "Hi", URL: "/app/chat?room=1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok = reasons()["off"]; !ok {
		t.Fatalf("off skip: %+v", reasons())
	}
	// Delivered while a focused tab showed it: the service worker reports it.
	n.request(t, "PUT", base+"/preferences", schema.NotificationPreferences{Mail: true, MailPeopleOnly: true, Chat: true, ChatRequests: true, Calendar: true, Meet: true}, nil, 200)
	if _, err := push.Notify(ctx, nil, w, []string{n.owner.Subject}, push.Event{Kind: push.KindChat, Title: "Secret preview text", Body: "Do not store me", URL: "/app/chat?room=2"}); err != nil {
		t.Fatal(err)
	}
	jobs := pushJobs(t, n)
	if len(jobs) != 1 || jobs[0].Log == "" {
		t.Fatalf("queued: %+v", jobs)
	}
	var sent struct{ Workspace, Log string }
	if err := json.Unmarshal([]byte(jobs[0].Payload), &sent); err != nil || sent.Workspace != w || sent.Log != jobs[0].Log {
		t.Fatalf("payload names its log entry: %+v", sent)
	}
	if claimPushJobs(t, n) != 0 {
		t.Fatal("delivery failed")
	}
	if r = notificationSettings(t, n).Recent[0]; r.Status != "delivered" || r.Reason != "" {
		t.Fatalf("delivered: %+v", r)
	}
	n.request(t, "POST", base+"/recent/"+sent.Log+"/open", nil, nil, 200)
	if r = notificationSettings(t, n).Recent[0]; r.Reason != "open" || r.Status != "delivered" {
		t.Fatalf("open: %+v", r)
	}
	// The log keeps the kind and the link, never the title or preview.
	var leaked int
	if err := db.From(ctx).QueryRow(ctx, "SELECT count(*) FROM notification_log WHERE link LIKE '%Secret%' OR error LIKE '%Secret%' OR link LIKE '%Do not store%'").Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("log keeps payload text: %d", leaked)
	}

	// Bounded: at most 50 rows per member, Settings shows the last 10.
	for i := 0; i < 60; i++ {
		if _, err := push.Notify(ctx, nil, w, []string{n.owner.Subject}, push.Event{Kind: push.KindChat, Title: "Mine", URL: "/app/chat?room=1", Actor: n.owner.Subject}); err != nil {
			t.Fatal(err)
		}
	}
	var rows int
	if err := db.From(ctx).QueryRow(ctx, "SELECT count(*) FROM notification_log WHERE workspace_id=$1 AND subject=$2", w, n.owner.Subject).Scan(&rows); err != nil || rows != 50 {
		t.Fatalf("log rows: %d", rows)
	}
	if recent := notificationSettings(t, n).Recent; len(recent) != 10 {
		t.Fatalf("recent shows %d", len(recent))
	}

	// Another member sees none of it and cannot mark it; another workspace is a 404.
	other := n.addMember(t, "colleague@push-recent.example")
	n.login(t, other.Email)
	if s := notificationSettings(t, n); len(s.Recent) != 0 {
		t.Fatalf("colleague sees %d entries", len(s.Recent))
	}
	n.request(t, "POST", base+"/recent/"+sent.Log+"/open", nil, nil, 404)
	n.request(t, "POST", base+"/recent/not-an-id/open", nil, nil, 404)
	outsider, err := auth.From(ctx).CreateUser(ctx, "outsider@push-recent-elsewhere.example", "Outsider", pushPassword)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Elsewhere", OwnerEmail: outsider.Email})
	if err != nil {
		t.Fatal(err)
	}
	n.login(t, outsider.Email)
	n.request(t, "GET", base, nil, nil, 404)
	n.request(t, "POST", base+"/recent/"+sent.Log+"/open", nil, nil, 404)
	n.request(t, "POST", "/api/v1/workspaces/"+elsewhere.ID+"/notifications/recent/"+sent.Log+"/open", nil, nil, 404)
	var theirs schema.NotificationSettings
	n.request(t, "GET", "/api/v1/workspaces/"+elsewhere.ID+"/notifications", nil, &theirs, 200)
	if len(theirs.Recent) != 0 {
		t.Fatal("another workspace lists this workspace's notifications")
	}
}

func TestMailChoiceAllOrNone(t *testing.T) {
	n := pushNode(t, "push-choice.example")
	service := newPushService(t)
	base := "/api/v1/workspaces/" + n.workspace.ID + "/notifications"
	n.subscribePush(t, service.server.URL+"/owner", 200)
	// "All new Inbox mail": mail that is not from a real person notifies.
	n.request(t, "PUT", base+"/preferences", schema.NotificationPreferences{Mail: true, MailPeopleOnly: false, Chat: true, ChatRequests: true, Calendar: true, Meet: true}, nil, 200)
	receiveAbuse(t, n, "choice-all", "Newsletter <news@outside.example>", "Weekly news", "", 200)
	if jobs := pushJobs(t, n); len(jobs) != 1 || payloadOf(t, jobs[0]).Kind != "mail" {
		t.Fatalf("all mail: %d jobs", len(jobs))
	}
	if r := notificationSettings(t, n).Recent[0]; r.Status != "queued" || r.Kind != "mail" {
		t.Fatalf("all mail log: %+v", r)
	}
	clearPushJobs(t, n)
	// "No mail": nothing, not even from a saved contact.
	ctx := n.server.Context()
	if _, err := db.From(ctx).Exec(ctx, "INSERT INTO contact(workspace_id,name,email) VALUES($1,'Ada','ada@outside.example')", n.workspace.ID); err != nil {
		t.Fatal(err)
	}
	n.request(t, "PUT", base+"/preferences", schema.NotificationPreferences{Mail: false, MailPeopleOnly: true, Chat: true, ChatRequests: true, Calendar: true, Meet: true}, nil, 200)
	receiveAbuse(t, n, "choice-none-person", "Ada Lovelace <ada@outside.example>", "Lunch?", "", 200)
	receiveAbuse(t, n, "choice-none-news", "Newsletter <news@outside.example>", "More news", "", 200)
	if jobs := pushJobs(t, n); len(jobs) != 0 {
		t.Fatalf("no mail notified: %d", len(jobs))
	}
	if r := notificationSettings(t, n).Recent[0]; r.Status != "skipped" || r.Reason != "off" {
		t.Fatalf("no mail log: %+v", r)
	}
	// The stored shape stays mail + mailPeopleOnly.
	if p := notificationSettings(t, n).Preferences; p.Mail || !p.MailPeopleOnly {
		t.Fatalf("stored: %+v", p)
	}
}
