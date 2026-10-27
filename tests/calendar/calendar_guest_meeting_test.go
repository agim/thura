package tests

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"thura/tests/testsupport"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	lkauth "github.com/livekit/protocol/auth"
	"thura/app"
	"thura/internal/meet"
	"thura/internal/workspace"
	"thura/schema"
)

func TestCalendarGuestMeetingInvitationAndRevocation(t *testing.T) {
	key, secret := "guest-test", "synthetic-guest-meeting-secret-4829147"
	var removed atomic.Int32
	sfu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "RemoveParticipant") {
			removed.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer sfu.Close()
	t.Setenv("JOBS_WORKERS", "0")
	t.Setenv("LIDZA_MASTER_KEY", strings.Repeat("bc", 32))
	t.Setenv("LIVEKIT_SERVER_URL", sfu.URL)
	t.Setenv("LIVEKIT_PUBLIC_URL", "ws://localhost:7880")
	t.Setenv("LIVEKIT_API_KEY", key)
	t.Setenv("LIVEKIT_API_SECRET", secret)
	srv := testsupport.StartConfigured(t, app.New(nil))
	ctx := srv.Context()
	pool := db.From(ctx)
	email := fmt.Sprintf("guest-meet-owner-%d@example.com", time.Now().UnixNano())
	pw := "maple river lantern forest 8484"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	workspaceValue, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Guest meetings", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	w := workspaceValue.ID
	t.Cleanup(func() {
		events := "SELECT e.id FROM calendar_event e JOIN calendar c ON c.id=e.calendar_id WHERE c.workspace_id=$1"
		for _, sql := range []string{"DELETE FROM meeting_participant WHERE meeting_id IN (SELECT id FROM meeting WHERE workspace_id=$1)", "DELETE FROM meeting_invite WHERE event_id IN (" + events + ")", "DELETE FROM calendar_reply_grant WHERE attendee_id IN (SELECT id FROM event_attendee WHERE event_id IN (" + events + "))", "DELETE FROM calendar_dispatch WHERE event_id IN (" + events + ")", "DELETE FROM event_attendee WHERE event_id IN (" + events + ")", "DELETE FROM calendar_event WHERE id IN (" + events + ")", "DELETE FROM calendar WHERE workspace_id=$1", "DELETE FROM meeting WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, e := pool.Exec(ctx, sql, w); e != nil {
				t.Error(e)
			}
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	request := func(method, path string, in, out any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, in, out)
		if res.StatusCode != want {
			t.Fatalf("%s %s = %d want %d", method, path, res.StatusCode, want)
		}
	}
	login := func() { request("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200) }
	login()
	base := "/api/v1/workspaces/" + w
	var cal schema.Calendar
	request("POST", base+"/calendars", schema.CalendarInput{Name: "Personal", Color: "#15756b", Personal: true}, &cal, 200)
	events := base + "/calendars/" + cal.ID + "/events"
	start := time.Now().UTC().Add(time.Hour)
	end := start.Add(time.Hour)
	enabled := true
	location := "Room 4"
	description := "Discuss plans"
	in := schema.EventInput{Title: "Planning with guests", Description: &description, Location: &location, StartsAt: &start, EndsAt: &end, TimeZone: "UTC", CreateMeeting: &enabled, Attendees: []string{"one@fixture.example", "two@fixture.example"}, SendInvitations: &enabled}
	var event schema.EventDetail
	failed := in
	failed.Attendees = nil
	request("POST", events, failed, nil, 422)
	var orphanCount int
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM meeting WHERE workspace_id=$1", w).Scan(&orphanCount); err != nil || orphanCount != 0 {
		t.Fatal("failed send left an orphan meeting")
	}
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM calendar_event WHERE calendar_id=$1", cal.ID).Scan(&orphanCount); err != nil || orphanCount != 0 {
		t.Fatal("failed send left an orphan event")
	}
	request("POST", events, in, &event, 200)
	if event.Dispatch == nil || event.Dispatch.Queued != 2 || !event.Dispatch.Captured {
		t.Fatal("save-and-send did not return actual capture result")
	}
	if event.Event.MeetingID == nil {
		t.Fatal("meeting was not created atomically")
	}
	link, err := url.Parse(event.Event.Location)
	if err != nil {
		t.Fatal(err)
	}
	fragment, _ := url.ParseQuery(link.Fragment)
	pin := fragment.Get("pin")
	code := link.Query().Get("room")
	if len(pin) != 6 || len(code) != 9 || link.Path != "/meet" {
		t.Fatal("invitation link is missing meeting ID or PIN")
	}
	for _, part := range []string{"Discuss plans", "Location: Room 4", "Meeting ID: " + code, "PIN: " + pin, "If the link does not work"} {
		if !strings.Contains(event.Event.Description, part) {
			t.Fatalf("missing %q in instructions", part)
		}
	}
	for _, recipient := range in.Attendees {
		var body string
		if err = pool.QueryRow(ctx, "SELECT text FROM mail_message WHERE recipient=$1 ORDER BY created_at DESC LIMIT 1", recipient).Scan(&body); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, event.Event.Location) || !strings.Contains(body, "PIN: "+pin) || strings.Contains(body, "Only current workspace members") {
			t.Fatal("guest invitation lacks usable instructions")
		}
	}
	var dispatch schema.CalendarDispatchResult
	request("POST", events+"/"+event.Event.ID+"/invitations", schema.CalendarInviteInput{Sequence: event.Event.Sequence}, &dispatch, 200)
	if dispatch.Queued != 0 {
		t.Fatal("same version sent twice")
	}
	var exported schema.FileContent
	request("GET", base+"/calendars/"+cal.ID+"/export", nil, &exported, 200)
	raw, err := base64.StdEncoding.DecodeString(exported.Data)
	if err != nil {
		t.Fatal(err)
	}
	ics := strings.ReplaceAll(string(raw), "\r\n ", "")
	if !strings.Contains(ics, "LOCATION:"+event.Event.Location) || !strings.Contains(ics, "URL:"+event.Event.Location) || !strings.Contains(ics, "PIN: "+pin) {
		t.Fatal("ICS lacks join link or fallback instructions")
	}
	in.Sequence = event.Event.Sequence
	in.Description = &event.Event.Description
	in.Location = &event.Event.Location
	in.CreateMeeting = nil
	in.SendInvitations = nil
	request("PUT", events+"/"+event.Event.ID, in, &event, 200)
	if strings.Count(event.Event.Description, "[Thura Meet]") != 1 {
		t.Fatal("editing duplicated instructions")
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM meeting WHERE workspace_id=$1", w).Scan(&count); err != nil || count != 1 {
		t.Fatal("editing created another meeting")
	}
	// Do not leave a calendar invitation pointing at a different room's old PIN.
	var replacement schema.Meeting
	request("POST", base+"/meet", schema.MeetingInput{Name: "Replacement", RequestID: uuid.NewString()}, &replacement, 200)
	request("PUT", events+"/"+event.Event.ID+"/meeting", schema.BindMeetingInput{MeetingID: replacement.ID, Sequence: event.Event.Sequence}, nil, 409)
	// A separate client has no workspace session or account.
	guestRequest := func(input schema.GuestMeetingInput, want int) schema.GuestMeetingAccess {
		t.Helper()
		body, _ := json.Marshal(input)
		res, e := http.Post(srv.URL+"/api/v1/meeting-guests/join", "application/json", bytes.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var access schema.GuestMeetingAccess
		if res.StatusCode != want {
			b, _ := io.ReadAll(res.Body)
			t.Fatalf("guest join=%d want %d: %s", res.StatusCode, want, b)
		}
		if want == 200 && json.NewDecoder(res.Body).Decode(&access) != nil {
			t.Fatal("invalid guest response")
		}
		return access
	}
	guest := schema.GuestMeetingInput{Code: code, Pin: pin, Name: "Invited visitor"}
	wrong := guest
	wrong.Pin = "abcdef"
	guestRequest(wrong, 404)
	wrong.Pin = "000000"
	if pin == wrong.Pin {
		wrong.Pin = "111111"
	}
	guestRequest(wrong, 404)
	// Failed PINs are counted on the invite itself, not only per client
	// address: the tenth locks it, even for the correct PIN, and the
	// lockout doubles with every further failure.
	inviteState := func() (int, *time.Time) {
		t.Helper()
		var failed int
		var locked *time.Time
		if err = pool.QueryRow(ctx, "SELECT failed_pins, locked_until FROM meeting_invite WHERE event_id=$1", event.Event.ID).Scan(&failed, &locked); err != nil {
			t.Fatal(err)
		}
		return failed, locked
	}
	// The non-numeric PIN is refused before the lookup; only the digit one counts.
	if pinFailures, lockedUntil := inviteState(); pinFailures != 1 || lockedUntil != nil {
		t.Fatalf("one wrong PIN: failed=%d locked=%v", pinFailures, lockedUntil)
	}
	if _, err = pool.Exec(ctx, "UPDATE meeting_invite SET failed_pins=9 WHERE event_id=$1", event.Event.ID); err != nil {
		t.Fatal(err)
	}
	guestRequest(wrong, 404)
	pinFailures, lockedUntil := inviteState()
	if pinFailures != 10 || lockedUntil == nil || lockedUntil.Before(time.Now().Add(14*time.Minute)) || lockedUntil.After(time.Now().Add(16*time.Minute)) {
		t.Fatalf("tenth wrong PIN did not lock the invite: failed=%d locked=%v", pinFailures, lockedUntil)
	}
	guestRequest(guest, 429)
	if _, err = pool.Exec(ctx, "UPDATE meeting_invite SET failed_pins=12, locked_until=now()-interval '1 second' WHERE event_id=$1", event.Event.ID); err != nil {
		t.Fatal(err)
	}
	guestRequest(wrong, 404)
	if pinFailures, lockedUntil = inviteState(); pinFailures != 13 || lockedUntil == nil || lockedUntil.Before(time.Now().Add(110*time.Minute)) {
		t.Fatalf("lockout did not grow: failed=%d locked=%v", pinFailures, lockedUntil)
	}
	if _, err = pool.Exec(ctx, "UPDATE meeting_invite SET locked_until=NULL WHERE event_id=$1", event.Event.ID); err != nil {
		t.Fatal(err)
	}
	access := guestRequest(guest, 200)
	if pinFailures, lockedUntil = inviteState(); pinFailures != 0 || lockedUntil != nil {
		t.Fatalf("correct PIN did not reset the counter: failed=%d locked=%v", pinFailures, lockedUntil)
	}
	verifier, err := lkauth.ParseAPIToken(access.Token)
	if err != nil {
		t.Fatal(err)
	}
	claims, grants, err := verifier.Verify(secret)
	if err != nil || grants.Video == nil || !grants.Video.RoomJoin || grants.Video.Room != meet.Room(*event.Event.MeetingID) || grants.Video.RoomAdmin || grants.Video.RoomCreate {
		t.Fatal("guest token exceeded room-only scope")
	}
	res, err := http.Get(srv.URL + base + "/meet")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal("guest gained workspace access")
	}
	hooks := []string{}
	hook := func(kind, sid string) {
		t.Helper()
		id := uuid.NewString()
		hooks = append(hooks, id)
		body, _ := json.Marshal(map[string]any{"id": id, "event": kind, "room": map[string]string{"name": meet.Room(*event.Event.MeetingID)}, "participant": map[string]string{"identity": claims.Subject, "sid": sid}})
		digest := sha256.Sum256(body)
		token, e := lkauth.NewAccessToken(key, secret).SetValidFor(time.Minute).SetSha256(base64.StdEncoding.EncodeToString(digest[:])).ToJWT()
		if e != nil {
			t.Fatal(e)
		}
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/meet/webhook", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		res.Body.Close()
		if res.StatusCode != 204 {
			t.Fatal("guest webhook rejected")
		}
	}
	t.Cleanup(func() { pool.Exec(ctx, "DELETE FROM meeting_webhook WHERE event_id=ANY($1)", hooks) })
	hook("participant_joined", "session-1")
	if removed.Load() != 0 {
		t.Fatal("valid guest was removed")
	}
	hook("participant_joined", "session-2")
	hook("participant_left", "session-1")
	var joined bool
	if err = pool.QueryRow(ctx, "SELECT joined FROM meeting_participant WHERE subject=$1", claims.Subject).Scan(&joined); err != nil || !joined {
		t.Fatal("stale leave cleared current guest session")
	}
	request("DELETE", events+"/"+event.Event.ID+fmt.Sprintf("?sequence=%d", event.Event.Sequence), nil, &event, 200)
	guestRequest(guest, 410)
	if err = meet.Reconcile(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if removed.Load() != 1 {
		t.Fatal("cancelled event did not revoke connected guest")
	}
	// End and expiry also deny fresh admission, even with correct credentials.
	if _, err = pool.Exec(ctx, "UPDATE calendar_event SET cancelled=false WHERE id=$1", event.Event.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE meeting SET ended_at=now() WHERE id=$1", *event.Event.MeetingID); err != nil {
		t.Fatal(err)
	}
	guestRequest(guest, 410)
	if _, err = pool.Exec(ctx, "UPDATE meeting SET ended_at=NULL WHERE id=$1", *event.Event.MeetingID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE meeting_invite SET expires_at=now()-interval '1 second' WHERE event_id=$1", event.Event.ID); err != nil {
		t.Fatal(err)
	}
	guestRequest(guest, 410)
}
