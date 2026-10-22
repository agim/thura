package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"thura/app"
	q "thura/db/queries/gen"
	"thura/internal/calendar"
	"thura/internal/chat"
	"thura/internal/mailbox"
	provider "thura/internal/providers/matrix"
	"thura/internal/smipmail"
	"thura/internal/workspace"
	"thura/schema"
)

// One row that cannot progress must never stall a sweep for every other
// workspace: the SMIP transmission sweep, the calendar reminders and the chat
// membership reconcile each run every minute from the head of their queue.

func TestSmipMailSweepHoldsUnavailableIdentitiesAndServesOtherTeams(t *testing.T) {
	a, b := newMailNode(t, "sweep-alpha.example"), newMailNode(t, "sweep-beta.example")
	target, source := mailTLS(t, b, nil), mailTLS(t, a, nil)
	mailTrust(t, a, map[string]string{b.config.Domain: target.URL}, target)
	mailTrust(t, b, map[string]string{a.config.Domain: source.URL}, source)
	first := a.queue(t, b.box.Address)
	a.deliver(t, first)
	if got := a.detail(t, first); got.Item.Status != "sent" {
		t.Fatalf("first delivery failed: %+v", got)
	}
	// A secure intent whose peer is down is held for a minute.
	target.Close()
	held := a.queue(t, b.box.Address)
	a.deliver(t, held)
	if got := a.detail(t, held); got.Item.Status != "delivery_pending" || got.Deliveries[0].Transport != "smip" {
		t.Fatalf("outage did not hold the secure route: %+v", got)
	}
	heldRow, _ := a.deliveries(t, held)
	// Fifty older transmissions of another team whose domain has no identity on
	// this server: Dispatch cannot resolve it and used to leave them untouched.
	ctx := a.server.Context()
	pool := db.From(ctx)
	queries := q.New(pool)
	orphan, err := auth.From(ctx).CreateUser(ctx, "owner@orphan.example", "Orphan owner", "mail meadow river lantern 87344")
	if err != nil {
		t.Fatal(err)
	}
	other, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Orphan team", OwnerEmail: orphan.Email})
	if err != nil {
		t.Fatal(err)
	}
	box, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: other.ID, Name: "Inbox", Address: "team@orphan.example"})
	if err != nil {
		t.Fatal(err)
	}
	stale := lidza.Now(ctx).Add(-time.Hour)
	orphans := map[string]bool{}
	for i := 0; i < 50; i++ {
		item, err := queries.CreateDraft(ctx, q.CreateDraftParams{MailboxID: box.ID, AuthorID: orphan.Subject, FromAddress: box.Address, ToAddress: b.box.Address, Subject: fmt.Sprintf("Orphan %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		row, err := queries.AddMailTransmission(ctx, q.AddMailTransmissionParams{Origin: "orphan.example", WorkspaceID: other.ID, MailboxID: box.ID, ItemID: item.ID, AuthorID: orphan.Subject, Payload: "{}", NextAttempt: stale})
		if err != nil {
			t.Fatal(err)
		}
		if err = queries.AddMailDelivery(ctx, q.AddMailDeliveryParams{TransmissionID: row.ID, Recipient: b.box.Address, Domain: b.config.Domain, NextAttempt: stale}); err != nil {
			t.Fatal(err)
		}
		orphans[row.ID] = true
	}
	sweep := func() map[string]bool {
		t.Helper()
		if _, err := pool.Exec(ctx, "DELETE FROM job WHERE kind=$1", smipmail.DispatchJob); err != nil {
			t.Fatal(err)
		}
		if err := smipmail.Reconcile(ctx, nil); err != nil {
			t.Fatal(err)
		}
		rows, err := pool.Query(ctx, "SELECT unique_key FROM job WHERE kind=$1 AND state='pending'", smipmail.DispatchJob)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		selected := map[string]bool{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			selected[id] = true
		}
		return selected
	}
	a.server.Clock.Advance(61 * time.Second)
	selected := sweep()
	if len(selected) != 50 || selected[heldRow.ID] {
		t.Fatalf("first sweep must take the fifty oldest rows, got %d (held %v)", len(selected), selected[heldRow.ID])
	}
	for id := range selected {
		if err := smipmail.Dispatch(ctx, smipmail.Job(id)); err != nil {
			t.Fatal(err)
		}
	}
	var heldBack int
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM mail_transmission WHERE workspace_id=$1 AND state='planning' AND reason='identity_unavailable' AND next_attempt>$2", other.ID, lidza.Now(ctx)).Scan(&heldBack); err != nil || heldBack != 50 {
		t.Fatalf("unavailable identities not held back: %d %v", heldBack, err)
	}
	// The next minute reaches the other team instead of the same fifty rows.
	selected = sweep()
	if len(selected) != 1 || !selected[heldRow.ID] {
		t.Fatalf("held rows crowded out the due delivery: %v", selected)
	}
	if err := smipmail.Dispatch(ctx, smipmail.Job(heldRow.ID)); err != nil {
		t.Fatal(err)
	}
	if got := a.detail(t, held); got.Item.Status != "delivery_pending" || got.Deliveries[0].Transport != "smip" {
		t.Fatalf("retry changed the secure route: %+v", got)
	}
	if selected = sweep(); len(selected) != 0 {
		t.Fatalf("nothing is due within the holds: %v", selected)
	}
	// Holds expire: nothing is dropped from the sweep for good, and the row
	// due first comes first.
	a.server.Clock.Advance(5 * time.Minute)
	selected = sweep()
	if len(selected) != 50 || !selected[heldRow.ID] {
		t.Fatalf("expired holds not retried in due order: %d (held %v)", len(selected), selected[heldRow.ID])
	}
	for id := range selected {
		if !orphans[id] && id != heldRow.ID {
			t.Fatal("unknown transmission selected", id)
		}
	}
}

func TestCalendarReminderFailureDoesNotStopOtherSubscriptions(t *testing.T) {
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	pool := db.From(ctx)
	email := fmt.Sprintf("cal-reminder-%d@example.com", time.Now().UnixNano())
	pw := "forest maple waterfall lantern 7143"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Reminder isolation", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		events := "SELECT e.id FROM calendar_event e JOIN calendar c ON c.id=e.calendar_id WHERE c.workspace_id=$1"
		for _, sql := range []string{"DELETE FROM calendar_reminder WHERE event_id IN (" + events + ")", "DELETE FROM reminder_notice WHERE event_id IN (" + events + ")", "DELETE FROM event_attendee WHERE event_id IN (" + events + ")", "DELETE FROM calendar_event WHERE id IN (" + events + ")", "DELETE FROM calendar WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, err = pool.Exec(ctx, sql, w.ID); err != nil {
				t.Error(err)
			}
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	status := func(method, path string, in, out any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, in, out)
		if res.StatusCode != want {
			t.Fatalf("%s: got %d want %d", path, res.StatusCode, want)
		}
	}
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	base := "/api/v1/workspaces/" + w.ID + "/calendars"
	var cal schema.Calendar
	status("POST", base, schema.CalendarInput{Name: "Team", Color: "#15756b"}, &cal, 200)
	start := time.Now().Add(30 * time.Minute).UTC().Truncate(time.Second)
	end := start.Add(time.Hour)
	minutes := 30
	events := base + "/" + cal.ID + "/events"
	var broken, fine schema.EventDetail
	for _, e := range []struct {
		title string
		out   *schema.EventDetail
	}{{"Broken reminder", &broken}, {"Working reminder", &fine}} {
		status("POST", events, schema.EventInput{Title: e.title, StartsAt: &start, EndsAt: &end, TimeZone: "America/New_York"}, e.out, 200)
		status("PUT", events+"/"+e.out.Event.ID+"/reminder", schema.ReminderInput{MinutesBefore: &minutes}, nil, 200)
	}
	// The first subscription of the sweep cannot be computed any more; the
	// one after it must still be sent in the same run.
	if _, err = pool.Exec(ctx, "UPDATE calendar_event SET time_zone='Nowhere/Invalid' WHERE id=$1", broken.Event.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "UPDATE calendar_reminder SET id='00000000-0000-0000-0000-000000000001' WHERE event_id=$1", broken.Event.ID); err != nil {
		t.Fatal(err)
	}
	now := start.Add(-30 * time.Minute).Add(time.Second)
	if err = calendar.RemindAt(ctx, now); err != nil {
		t.Fatal("a broken subscription must not fail the sweep:", err)
	}
	var sent int
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM mail_message WHERE recipient=$1 AND subject='Reminder: Working reminder'", email).Scan(&sent); err != nil || sent != 1 {
		t.Fatal("reminder after the broken subscription was lost", sent, err)
	}
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM reminder_notice WHERE event_id=$1", broken.Event.ID).Scan(&sent); err != nil || sent != 0 {
		t.Fatal("broken subscription recorded a notice without mail", sent, err)
	}
	if err = calendar.RemindAt(ctx, now); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM mail_message WHERE recipient=$1 AND subject LIKE 'Reminder:%'", email).Scan(&sent); err != nil || sent != 1 {
		t.Fatal("reminder duplicated or lost on the next run", sent, err)
	}
}

func TestChatReconcileSkipsRefusedKicksAndReleasesWorkspaceLock(t *testing.T) {
	token := "thura-matrix-stub-application-service-fixture-token-72498"
	cfg := provider.Config{ServerName: "matrix.test"}
	refused := cfg.User("revoked-one")
	slow := cfg.User("revoked-two")
	var mu sync.Mutex
	kicked := map[string]int{}
	stalled := make(chan struct{}, 1)
	matrix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/state/m.room.member/"):
			if strings.HasSuffix(r.URL.Path, "/"+slow) {
				select {
				case stalled <- struct{}{}:
				default:
				}
				time.Sleep(1500 * time.Millisecond)
			}
			json.NewEncoder(w).Encode(map[string]string{"membership": "join"})
		case strings.HasSuffix(r.URL.Path, "/kick"):
			var in struct {
				User string `json:"user_id"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			mu.Lock()
			kicked[in.User]++
			mu.Unlock()
			if in.User == refused {
				w.WriteHeader(403)
				json.NewEncoder(w).Encode(map[string]string{"errcode": "M_FORBIDDEN"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{})
		default:
			json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer matrix.Close()
	t.Setenv("MATRIX_SERVER_URL", matrix.URL)
	t.Setenv("MATRIX_SERVER_NAME", "matrix.test")
	t.Setenv("MATRIX_AS_TOKEN", token)
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	pool := db.From(ctx)
	ownerEmail := fmt.Sprintf("chat-reconcile-%d@example.com", time.Now().UnixNano())
	owner, err := auth.From(ctx).CreateUser(ctx, ownerEmail, "Owner", "forest maple waterfall lantern 7143")
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Chat reconcile", OwnerEmail: ownerEmail})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM chat_participant WHERE room_id IN (SELECT id FROM chat_room WHERE workspace_id=$1)", "DELETE FROM chat_room WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, err = pool.Exec(ctx, sql, w.ID); err != nil {
				t.Error(err)
			}
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	var room string
	if err = pool.QueryRow(ctx, "INSERT INTO chat_room(workspace_id,matrix_room_id,request_id,name,created_by) VALUES($1,$2,$3,'General',$4) RETURNING id", w.ID, "!reconcile-"+w.ID+":matrix.test", uuid.NewString(), owner.Subject).Scan(&room); err != nil {
		t.Fatal(err)
	}
	// Three revoked participants; the refused kick comes first in id order.
	for i, subject := range []string{"revoked-one", "revoked-two", "revoked-three"} {
		if _, err = pool.Exec(ctx, "INSERT INTO chat_participant(id,room_id,subject) VALUES($1,$2,$3)", fmt.Sprintf("00000000-0000-0000-0000-00000000000%d", i+1), room, subject); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() { done <- chat.Reconcile(ctx, nil) }()
	// While Matrix is slow for one row, the workspace stays lockable.
	select {
	case <-stalled:
	case <-time.After(10 * time.Second):
		t.Fatal("Matrix membership check never reached")
	}
	began := time.Now()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "SELECT id FROM workspace WHERE id=$1 FOR UPDATE", w.ID); err != nil {
		t.Fatal(err)
	}
	waited := time.Since(began)
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if waited > 700*time.Millisecond {
		t.Fatalf("workspace lock held across the Matrix call: %s", waited)
	}
	if err = <-done; err != nil {
		t.Fatal("one refused kick failed the sweep:", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if kicked[refused] != 1 || kicked[slow] != 1 || kicked[cfg.User("revoked-three")] != 1 {
		t.Fatalf("kicks: %v", kicked)
	}
	var left []string
	rows, err := pool.Query(ctx, "SELECT subject FROM chat_participant WHERE room_id=$1 ORDER BY id", room)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		left = append(left, s)
	}
	if len(left) != 1 || left[0] != "revoked-one" {
		t.Fatalf("refused row must wait for the next run while the others go: %v", left)
	}
}
