package tests

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/credentials"
	"github.com/google/uuid"
	"thura/app"
	"thura/internal/calendar"
	"thura/internal/push"
	"thura/internal/workspace"
	"thura/schema"
)

// pushService is a local Web Push service: it records each delivery and
// answers with the status the test sets.
type pushService struct {
	server *httptest.Server
	status atomic.Int32
	mu     sync.Mutex
	got    []*http.Request
	bodies [][]byte
}

func newPushService(t *testing.T) *pushService {
	t.Helper()
	p := &pushService{}
	p.status.Store(201)
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.got = append(p.got, r)
		p.bodies = append(p.bodies, body)
		p.mu.Unlock()
		w.WriteHeader(int(p.status.Load()))
	}))
	t.Cleanup(p.server.Close)
	return p
}
func (p *pushService) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.got)
}

// browserKeys are a subscription's p256dh and auth keys, as a browser makes them.
func browserKeys(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 16)
	if _, err = rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(secret)
}

// pushNode is a mail node whose inbound relay delivers clean mail at once
// (no model, scanner or local filter), so arrival notifies in the request.
func pushNode(t *testing.T, domain string) *mailNode {
	t.Helper()
	t.Setenv("MAIL_AI_SCREENING", "false")
	t.Setenv("THURA_AI_ENABLED", "false")
	t.Setenv("MAIL_CLAMD_ADDRESS", "")
	t.Setenv("MAIL_RSPAMD_URL", "")
	t.Setenv("MAIL_INBOUND_SECRET", abuseRelaySecret)
	return newMailNode(t, domain)
}

const pushPassword = "signal harbour copper meadow 51827"

func (n *mailNode) login(t *testing.T, email string) {
	t.Helper()
	password := pushPassword
	if email == n.owner.Email {
		password = "mail meadow river lantern 87344" // newMailNode's owner
	}
	if r := n.server.JSON(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: password}, nil); r.StatusCode != 200 {
		t.Fatalf("login %s: %d", email, r.StatusCode)
	}
}

// addMember creates an account and adds it to the node's workspace.
func (n *mailNode) addMember(t *testing.T, email string) auth.Profile {
	t.Helper()
	ctx := n.server.Context()
	p, err := auth.From(ctx).CreateUser(ctx, email, strings.Split(email, "@")[0], pushPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.From(ctx).Exec(ctx, "INSERT INTO auth_member(subject,scope,role) VALUES($1,$2,'member')", p.Subject, n.workspace.ID); err != nil {
		t.Fatal(err)
	}
	return p
}

func (n *mailNode) subscribePush(t *testing.T, endpoint string, want int) schema.NotificationSettings {
	t.Helper()
	p256dh, secret := browserKeys(t)
	var out schema.NotificationSettings
	n.request(t, "POST", "/api/v1/workspaces/"+n.workspace.ID+"/notifications/devices", schema.PushSubscribeInput{Endpoint: endpoint, P256dh: p256dh, Auth: secret, Label: "Chrome on Linux"}, &out, want)
	return out
}

// pushJobs are the queued deliveries, oldest first.
func pushJobs(t *testing.T, n *mailNode) []schema.PushJob {
	t.Helper()
	ctx := n.server.Context()
	rows, err := db.From(ctx).Query(ctx, "SELECT payload FROM job WHERE kind=$1 AND state='pending' ORDER BY created_at, id", push.SendJob)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []schema.PushJob{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var job schema.PushJob
		if err = json.Unmarshal(raw, &job); err != nil {
			t.Fatal(err)
		}
		out = append(out, job)
	}
	return out
}
func clearPushJobs(t *testing.T, n *mailNode) {
	t.Helper()
	ctx := n.server.Context()
	if _, err := db.From(ctx).Exec(ctx, "DELETE FROM job WHERE kind=$1", push.SendJob); err != nil {
		t.Fatal(err)
	}
}

type sentPayload struct {
	Kind, Title, Body, URL, Tag string
	View                        *push.View
}

func payloadOf(t *testing.T, job schema.PushJob) sentPayload {
	t.Helper()
	var p sentPayload
	if err := json.Unmarshal([]byte(job.Payload), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPushSubscriptionsAreTheMembersOwn(t *testing.T) {
	n := pushNode(t, "push-crud.example")
	service := newPushService(t)
	base := "/api/v1/workspaces/" + n.workspace.ID + "/notifications"
	var settings schema.NotificationSettings
	n.request(t, "GET", base, nil, &settings, 200)
	if len(settings.Devices) != 0 || settings.PublicKey == "" {
		t.Fatalf("fresh settings: %+v", settings)
	}
	if p := settings.Preferences; !p.Mail || !p.MailPeopleOnly || !p.Chat || !p.ChatRequests || !p.Calendar || !p.Meet {
		t.Fatalf("default preferences: %+v", p)
	}
	// An endpoint that is not a browser push service is refused.
	for _, endpoint := range []string{"https://evil.example/push", "http://push.services.mozilla.com/x", "file:///etc/passwd", "https://fcm.googleapis.com:8443/x"} {
		n.subscribePush(t, endpoint, 422)
	}
	settings = n.subscribePush(t, service.server.URL+"/device/one", 200)
	if len(settings.Devices) != 1 || settings.Devices[0].Label != "Chrome on Linux" || settings.Devices[0].EndpointHash != push.EndpointHash(service.server.URL+"/device/one") {
		t.Fatalf("subscribed: %+v", settings.Devices)
	}
	// Subscribing the same browser again replaces its row.
	if settings = n.subscribePush(t, service.server.URL+"/device/one", 200); len(settings.Devices) != 1 {
		t.Fatalf("resubscribe added a row: %d", len(settings.Devices))
	}
	device := settings.Devices[0].ID
	var saved schema.NotificationSettings
	n.request(t, "PUT", base+"/preferences", schema.NotificationPreferences{Mail: true, MailPeopleOnly: false, Chat: false, ChatRequests: true, Calendar: true, Meet: false}, &saved, 200)
	if p := saved.Preferences; p.MailPeopleOnly || p.Chat || p.Meet || !p.Mail {
		t.Fatalf("saved preferences: %+v", p)
	}

	// Another member of the workspace sees none of it and cannot remove it.
	other := n.addMember(t, "colleague@push-crud.example")
	n.login(t, other.Email)
	n.request(t, "GET", base, nil, &settings, 200)
	if len(settings.Devices) != 0 || !settings.Preferences.Chat {
		t.Fatalf("colleague sees: %+v", settings)
	}
	n.request(t, "DELETE", base+"/devices/"+device, nil, nil, 404)
	n.request(t, "DELETE", base+"/devices/not-an-id", nil, nil, 404)

	// A member of another workspace gets 404 on this one.
	ctx := n.server.Context()
	outsider, err := auth.From(ctx).CreateUser(ctx, "outsider@push-elsewhere.example", "Outsider", pushPassword)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Elsewhere", OwnerEmail: outsider.Email})
	if err != nil {
		t.Fatal(err)
	}
	n.login(t, outsider.Email)
	n.request(t, "GET", base, nil, nil, 404)
	n.request(t, "DELETE", base+"/devices/"+device, nil, nil, 404)
	n.request(t, "PUT", base+"/preferences", schema.NotificationPreferences{}, nil, 404)
	n.request(t, "POST", base+"/test", nil, nil, 404)
	n.request(t, "GET", "/api/v1/workspaces/"+elsewhere.ID+"/notifications", nil, &settings, 200)
	if len(settings.Devices) != 0 {
		t.Fatal("another workspace lists this workspace's device")
	}
	n.request(t, "DELETE", "/api/v1/workspaces/"+elsewhere.ID+"/notifications/devices/"+device, nil, nil, 404)

	// The owner removes it; a second removal is a 404.
	n.login(t, n.owner.Email)
	n.request(t, "DELETE", base+"/devices/"+device, nil, &settings, 200)
	if len(settings.Devices) != 0 {
		t.Fatal("device not removed")
	}
	n.request(t, "DELETE", base+"/devices/"+device, nil, nil, 404)
	// A test needs a device.
	n.request(t, "POST", base+"/test", nil, nil, 409)
}

func TestVapidKeysAreGeneratedOnceSealedAndStable(t *testing.T) {
	n := pushNode(t, "push-vapid.example")
	ctx := n.server.Context()
	public, private, err := push.Keys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	again, privateAgain, err := push.Keys(ctx)
	if err != nil || again != public || privateAgain != private {
		t.Fatal("a second call made another key pair")
	}
	if raw, err := base64.RawURLEncoding.DecodeString(public); err != nil || len(raw) != 65 || raw[0] != 4 {
		t.Fatalf("public key is not an uncompressed P-256 point: %d bytes", len(raw))
	}
	var stored string
	if err = db.From(ctx).QueryRow(ctx, "SELECT private_key FROM push_vapid_key").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == private || strings.Contains(stored, private) {
		t.Fatal("the private key is stored in the clear")
	}
	key, err := hex.DecodeString(strings.Repeat("ad", 32))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := credentials.Decrypt(key, stored)
	if err != nil || string(plain) != private {
		t.Fatal("the stored key is not sealed with the master key")
	}
	// A restart (a new process on the same database) reads the same pair.
	restarted := startConfigured(t, app.New(nil))
	afterRestart, _, err := push.Keys(restarted.Context())
	if err != nil || afterRestart != public {
		t.Fatal("the key pair changed across a restart")
	}
	var rows int
	if err = db.From(ctx).QueryRow(ctx, "SELECT count(*) FROM push_vapid_key").Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("%d key rows", rows)
	}
}

func TestMailArrivalNotifiesWithRealPersonAndSpamRules(t *testing.T) {
	n := pushNode(t, "push-mail.example")
	service := newPushService(t)
	n.subscribePush(t, service.server.URL+"/owner", 200)
	base := "/api/v1/workspaces/" + n.workspace.ID + "/notifications"

	// Default "only from real people": an unknown sender does not notify.
	unknown := receiveAbuse(t, n, "push-unknown", "Stranger <stranger@outside.example>", "Hello there", "", 200)
	if d := n.detail(t, unknown); d.Item.Folder != schema.MailFolderInbox {
		t.Fatalf("fixture mail in %s", d.Item.Folder)
	}
	if jobs := pushJobs(t, n); len(jobs) != 0 {
		t.Fatalf("unknown sender notified: %d", len(jobs))
	}
	// A saved contact is a real person.
	ctx := n.server.Context()
	if _, err := db.From(ctx).Exec(ctx, "INSERT INTO contact(workspace_id,name,email) VALUES($1,'Ada','ada@outside.example')", n.workspace.ID); err != nil {
		t.Fatal(err)
	}
	person := receiveAbuse(t, n, "push-person", "Ada Lovelace <ada@outside.example>", "Lunch on Thursday?", "", 200)
	jobs := pushJobs(t, n)
	if len(jobs) != 1 {
		t.Fatalf("real person mail: %d jobs", len(jobs))
	}
	p := payloadOf(t, jobs[0])
	if p.Kind != "mail" || p.Title != "Ada Lovelace" || p.Body != "Protection test" || strings.Contains(job0(jobs), "Lunch on Thursday") {
		t.Fatalf("mail payload: %+v", p)
	}
	if p.URL != "/app/mail?folder=inbox&mailbox="+n.box.ID+"&message="+person.ID+"&workspace="+n.workspace.ID {
		t.Fatalf("mail link: %s", p.URL)
	}
	if p.View == nil || p.View.Path != "/app/mail" || p.View.Params["folder"] != "inbox" {
		t.Fatalf("mail view: %+v", p.View)
	}
	clearPushJobs(t, n)

	// Without "only from real people" any Inbox mail notifies.
	n.request(t, "PUT", base+"/preferences", schema.NotificationPreferences{Mail: true, MailPeopleOnly: false, Chat: true, ChatRequests: true, Calendar: true, Meet: true}, nil, 200)
	receiveAbuse(t, n, "push-anyone", "Newsletter <news@outside.example>", "Weekly news", "", 200)
	if jobs = pushJobs(t, n); len(jobs) != 1 {
		t.Fatalf("any sender: %d jobs", len(jobs))
	}
	clearPushJobs(t, n)

	// Mail off: nothing.
	n.request(t, "PUT", base+"/preferences", schema.NotificationPreferences{Mail: false, MailPeopleOnly: false, Chat: true, ChatRequests: true, Calendar: true, Meet: true}, nil, 200)
	receiveAbuse(t, n, "push-off", "Ada Lovelace <ada@outside.example>", "Again", "", 200)
	if jobs = pushJobs(t, n); len(jobs) != 0 {
		t.Fatalf("mail off notified: %d", len(jobs))
	}
	n.request(t, "PUT", base+"/preferences", schema.NotificationPreferences{Mail: true, MailPeopleOnly: false, Chat: true, ChatRequests: true, Calendar: true, Meet: true}, nil, 200)

	// Held mail (quarantine, Spam) never notifies.
	settings := abuseSettings(t, n)
	settings.ApproveUnknown = true
	saveAbuseSettings(t, n, settings)
	held := receiveAbuse(t, n, "push-held", "Unknown <unknown@elsewhere.example>", "Please read", "", 200)
	if d := n.detail(t, held); d.Item.Folder != schema.MailFolderSpam {
		t.Fatalf("held mail in %s", d.Item.Folder)
	}
	if jobs = pushJobs(t, n); len(jobs) != 0 {
		t.Fatalf("held mail notified: %d", len(jobs))
	}

}

func job0(jobs []schema.PushJob) string { return jobs[0].Payload }

// Mail held for screening notifies when the screening releases it to the
// Inbox, not when it arrives.
func TestScreenedMailNotifiesWhenReleasedToInbox(t *testing.T) {
	n := abuseNode(t, "push-screened.example")
	service := newPushService(t)
	n.subscribePush(t, service.server.URL+"/owner", 200)
	item := receivePerson(t, n, "push-screen-person", "Grace <grace@outside.example>", "", "Lunch on Thursday?")
	if d := n.detail(t, item); d.Item.Folder != schema.MailFolderSpam {
		t.Fatalf("pending mail in %s", d.Item.Folder)
	}
	if jobs := pushJobs(t, n); len(jobs) != 0 {
		t.Fatalf("pending mail notified: %d", len(jobs))
	}
	screenPerson(t, n, item, 3, "benign", "person")
	jobs := pushJobs(t, n)
	if len(jobs) != 1 {
		t.Fatalf("released mail: %d jobs", len(jobs))
	}
	if p := payloadOf(t, jobs[0]); p.Kind != "mail" || p.Title != "Grace" || !strings.Contains(p.URL, "message="+item.ID) {
		t.Fatalf("released payload: %+v", p)
	}
	clearPushJobs(t, n)
	// A phishing verdict keeps it in Spam: no notification.
	bad := receivePerson(t, n, "push-screen-phish", "Bank <alerts@bank-security.example>", "", "Verify your password now")
	screenPerson(t, n, bad, 95, "phishing", "automated")
	if d := n.detail(t, bad); d.Item.Folder != schema.MailFolderSpam {
		t.Fatalf("phishing mail in %s", d.Item.Folder)
	}
	if jobs = pushJobs(t, n); len(jobs) != 0 {
		t.Fatalf("spam notified: %d", len(jobs))
	}
}

func TestChatCalendarAndMeetNotificationsSkipTheActor(t *testing.T) {
	n := pushNode(t, "push-events.example")
	service := newPushService(t)
	ctx := n.server.Context()
	pool := db.From(ctx)
	n.subscribePush(t, service.server.URL+"/owner", 200)
	colleague := n.addMember(t, "colleague@push-events.example")
	n.login(t, colleague.Email)
	n.subscribePush(t, service.server.URL+"/colleague", 200)
	n.login(t, n.owner.Email)
	w := n.workspace.ID

	// Chat: the other participants, never the sender.
	var room string
	if err := pool.QueryRow(ctx, "INSERT INTO chat_room(workspace_id,matrix_room_id,request_id,name,created_by) VALUES($1,$2,$3,'Launch',$4) RETURNING id", w, "!"+uuid.NewString()+":example.com", uuid.NewString(), n.owner.Subject).Scan(&room); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{n.owner.Subject, colleague.Subject} {
		if _, err := pool.Exec(ctx, "INSERT INTO chat_participant(room_id,subject) VALUES($1,$2)", room, s); err != nil {
			t.Fatal(err)
		}
	}
	push.ChatMessage(ctx, w, room, "Launch", false, colleague.Subject, "Ship it today?")
	jobs := pushJobs(t, n)
	if len(jobs) != 1 {
		t.Fatalf("chat: %d jobs", len(jobs))
	}
	if p := payloadOf(t, jobs[0]); p.Kind != "chat" || p.Title != "colleague in Launch" || p.Body != "Ship it today?" || p.URL != "/app/chat?room="+room+"&workspace="+w || p.View.Params["room"] != room {
		t.Fatalf("chat payload: %+v", p)
	}
	ownerDevice := jobs[0].SubscriptionID
	clearPushJobs(t, n)

	// External chat request: a shared address notifies every member.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conversation := uuid.NewString()
	if err = push.ExternalChat(ctx, tx, w, conversation, nil, "remote@partner.example", true); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if jobs = pushJobs(t, n); len(jobs) != 2 {
		t.Fatalf("chat request: %d jobs", len(jobs))
	}
	if p := payloadOf(t, jobs[0]); p.Kind != "chat_request" || p.Title != "Chat request from remote@partner.example" || p.URL != "/app/chat?external="+conversation+"&workspace="+w {
		t.Fatalf("chat request payload: %+v", p)
	}
	clearPushJobs(t, n)
	// The colleague turned chat requests off.
	n.login(t, colleague.Email)
	n.request(t, "PUT", "/api/v1/workspaces/"+w+"/notifications/preferences", schema.NotificationPreferences{Mail: true, MailPeopleOnly: true, Chat: true, ChatRequests: false, Calendar: true, Meet: true}, nil, 200)
	n.login(t, n.owner.Email)
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = push.ExternalChat(ctx, tx, w, conversation, nil, "remote@partner.example", true); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if jobs = pushJobs(t, n); len(jobs) != 1 || jobs[0].SubscriptionID != ownerDevice {
		t.Fatalf("chat requests off: %d jobs", len(jobs))
	}
	clearPushJobs(t, n)

	// Meet: invited members by address, never the organizer.
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	meeting := uuid.NewString()
	if err = push.MeetingStarting(ctx, tx, w, n.owner.Subject, meeting, "Standup", []string{"COLLEAGUE@push-events.example", n.owner.Email, "guest@outside.example"}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if jobs = pushJobs(t, n); len(jobs) != 1 || jobs[0].SubscriptionID == ownerDevice {
		t.Fatalf("meeting: %d jobs", len(jobs))
	}
	if p := payloadOf(t, jobs[0]); p.Kind != "meet" || p.URL != "/app/meet?meeting="+meeting+"&workspace="+w {
		t.Fatalf("meeting payload: %+v", p)
	}
	clearPushJobs(t, n)

	// Own actions: Notify drops the actor.
	if queued, err := push.Notify(ctx, nil, w, []string{n.owner.Subject}, push.Event{Kind: push.KindChat, Title: "Mine", URL: "/app/chat", Actor: n.owner.Subject}); err != nil || queued != 0 {
		t.Fatalf("own action queued %d (%v)", queued, err)
	}

	// Calendar: the reminder job pushes alongside the email.
	var cal schema.Calendar
	n.request(t, "POST", "/api/v1/workspaces/"+w+"/calendars", schema.CalendarInput{Name: "Team", Color: "#15756b"}, &cal, 200)
	start := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	end := start.Add(time.Hour)
	var event schema.EventDetail
	events := "/api/v1/workspaces/" + w + "/calendars/" + cal.ID + "/events"
	n.request(t, "POST", events, schema.EventInput{Title: "Planning", StartsAt: &start, EndsAt: &end, TimeZone: "UTC"}, &event, 200)
	minutes := 15
	n.request(t, "PUT", events+"/"+event.Event.ID+"/reminder", schema.ReminderInput{MinutesBefore: &minutes}, nil, 200)
	if err = calendar.RemindAt(ctx, start.Add(-15*time.Minute).Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	jobs = pushJobs(t, n)
	if len(jobs) != 1 || jobs[0].SubscriptionID != ownerDevice {
		t.Fatalf("reminder: %d jobs", len(jobs))
	}
	if p := payloadOf(t, jobs[0]); p.Kind != "calendar" || p.Title != "Reminder: Planning" || !strings.HasPrefix(p.URL, "/app/calendar?") || !strings.Contains(p.URL, "event="+event.Event.ID) {
		t.Fatalf("reminder payload: %+v", p)
	}
	var notices int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM reminder_notice WHERE event_id=$1", event.Event.ID).Scan(&notices); err != nil || notices != 1 {
		t.Fatalf("reminder notice: %d", notices)
	}
	// The sweep runs every minute; a second pass sends nothing new.
	if err = calendar.RemindAt(ctx, start.Add(-14*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if jobs = pushJobs(t, n); len(jobs) != 1 {
		t.Fatalf("reminder repeated: %d", len(jobs))
	}
}

func TestPushDeliveryEncryptsAndRemovesGoneSubscriptions(t *testing.T) {
	n := pushNode(t, "push-send.example")
	service := newPushService(t)
	ctx := n.server.Context()
	n.subscribePush(t, service.server.URL+"/device", 200)
	var result schema.PushTestResult
	n.request(t, "POST", "/api/v1/workspaces/"+n.workspace.ID+"/notifications/test", nil, &result, 200)
	if result.Queued != 1 {
		t.Fatalf("test queued %d", result.Queued)
	}
	run := func(want error) {
		t.Helper()
		for _, job := range pushJobs(t, n) {
			raw, err := json.Marshal(job)
			if err != nil {
				t.Fatal(err)
			}
			err = push.Send(ctx, raw)
			if (err != nil) != (want != nil) {
				t.Fatalf("send: %v", err)
			}
		}
	}
	run(nil)
	if service.count() != 1 {
		t.Fatalf("push service got %d requests", service.count())
	}
	service.mu.Lock()
	r, body := service.got[0], service.bodies[0]
	service.mu.Unlock()
	public, _, err := push.Keys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r.Header.Get("Content-Encoding") != "aes128gcm" || !strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") || !strings.HasSuffix(r.Header.Get("Authorization"), "k="+public) || r.Header.Get("TTL") == "" {
		t.Fatalf("push headers: %v", r.Header)
	}
	if strings.Contains(string(body), "Thura notifications are on") || len(body) > 4096 {
		t.Fatal("payload is not encrypted or exceeds a push record")
	}
	var used *time.Time
	if err = db.From(ctx).QueryRow(ctx, "SELECT last_used_at FROM push_subscription").Scan(&used); err != nil || used == nil {
		t.Fatal("delivery did not record last use")
	}
	// A failing push service is retried by the queue.
	service.status.Store(503)
	run(context.DeadlineExceeded)
	// A subscription the push service no longer knows is removed.
	service.status.Store(410)
	run(nil)
	var left int
	if err = db.From(ctx).QueryRow(ctx, "SELECT count(*) FROM push_subscription").Scan(&left); err != nil || left != 0 {
		t.Fatalf("gone subscription kept: %d", left)
	}
	var settings schema.NotificationSettings
	n.request(t, "GET", "/api/v1/workspaces/"+n.workspace.ID+"/notifications", nil, &settings, 200)
	if len(settings.Devices) != 0 {
		t.Fatal("gone device still listed")
	}
	// A job for a removed subscription is a no-op.
	run(nil)
}
