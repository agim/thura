package tests

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"thura/internal/federation"
	"thura/internal/smip"
	"thura/schema"
)

func TestSmipAppCancelAuthorizationIdempotencyAndAuditRollback(t *testing.T) {
	a := newSmipApp(t, "cancel-sender.example", "cancel-receiver.example")
	b := newSmipApp(t, "cancel-receiver.example", "cancel-sender.example")
	binding := a.binding(t, b.config.Domain, uuid.NewString())
	smipSendingGateway(t, a, b, smipTLS(t, b))
	in := schema.SmipSendInput{TransactionID: uuid.NewString(), BindingID: binding.ID, Body: smipText("withdraw this queued copy")}
	var queued, cancelled schema.SmipOutbound
	a.request(t, "POST", a.base()+"/outbox", in, &queued, 200)
	path := a.base() + "/outbox/" + queued.ID + "/cancel"
	a.login(t, a.member)
	a.request(t, "POST", path, nil, nil, 403)
	a.login(t, a.owner)
	a.request(t, "POST", a.base()+"/outbox/not-a-uuid/cancel", nil, nil, 404)
	a.request(t, "POST", a.base()+"/outbox/"+uuid.NewString()+"/cancel", nil, nil, 404)
	b.login(t, b.owner)
	b.request(t, "POST", b.base()+"/outbox/"+queued.ID+"/cancel", nil, nil, 404)

	ctx := a.server.Context()
	pool := db.From(ctx)
	constraint := fmt.Sprintf("smip_cancel_audit_%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, fmt.Sprintf("ALTER TABLE audit_event ADD CONSTRAINT %s CHECK(action<>'smip.message.cancel' OR scope<>'%s')", constraint, a.workspace.ID)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "ALTER TABLE audit_event DROP CONSTRAINT IF EXISTS "+constraint); err != nil {
			t.Error(err)
		}
	})
	a.request(t, "POST", path, nil, nil, 500)
	var state string
	if err := pool.QueryRow(ctx, "SELECT state FROM smip_outbox WHERE id=$1", queued.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != smip.Pending {
		t.Fatal("audit failure committed cancellation")
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE audit_event DROP CONSTRAINT "+constraint); err != nil {
		t.Fatal(err)
	}
	a.request(t, "POST", path, nil, &cancelled, 200)
	if cancelled.State != federation.Cancelled || cancelled.Attempts != 0 || cancelled.Reason != "cancelled_unsent" {
		t.Fatalf("incorrect cancellation: %+v", cancelled)
	}
	var retry schema.SmipOutbound
	a.request(t, "POST", path, nil, &retry, 200)
	if retry != cancelled {
		t.Fatal("retry changed cancellation")
	}
	a.request(t, "POST", a.base()+"/outbox", in, &retry, 200)
	if retry.State != federation.Cancelled {
		t.Fatal("queue retry resurrected cancelled intent")
	}
	a.request(t, "POST", a.base()+"/outbox/"+queued.ID+"/resume", nil, nil, 409)
	smipDispatch(t, a, queued.ID)
	if err := federation.Reconcile(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var events int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_event WHERE scope=$1 AND action='smip.message.cancel'", a.workspace.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("cancel retries wrote %d audit events", events)
	}
	var attempts int
	if err := pool.QueryRow(ctx, "SELECT attempts FROM smip_outbox WHERE id=$1", queued.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 {
		t.Fatal("cancelled send reached dispatch")
	}
	// Already-paused, definitely unsent intents can also be withdrawn.
	in.TransactionID = uuid.NewString()
	a.request(t, "POST", a.base()+"/outbox", in, &queued, 200)
	a.request(t, "DELETE", a.base()+"/bindings/"+binding.ID, nil, nil, 204)
	smipDispatch(t, a, queued.ID)
	a.request(t, "POST", a.base()+"/outbox/"+queued.ID+"/cancel", nil, &cancelled, 200)
	if cancelled.State != federation.Cancelled || cancelled.Attempts != 0 {
		t.Fatal("blocked unsent intent could not be cancelled")
	}
}

func TestSmipAppCancelRefusesInFlightAndAttemptedPackets(t *testing.T) {
	a := newSmipApp(t, "flight-sender.example", "flight-receiver.example")
	b := newSmipApp(t, "flight-receiver.example", "flight-sender.example")
	binding := a.binding(t, b.config.Domain, uuid.NewString())
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusForbidden)
	}))
	target.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	target.StartTLS()
	t.Cleanup(target.Close)
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	smipSendingGateway(t, a, b, target)
	var queued schema.SmipOutbound
	a.request(t, "POST", a.base()+"/outbox", schema.SmipSendInput{TransactionID: uuid.NewString(), BindingID: binding.ID, Body: smipText("already attempted")}, &queued, 200)
	raw, err := json.Marshal(schema.SmipDispatchJob{ID: queued.ID})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- federation.Dispatch(a.server.Context(), raw) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("dispatch never reached peer")
	}
	path := a.base() + "/outbox/" + queued.ID + "/cancel"
	a.request(t, "POST", path, nil, nil, 409)
	once.Do(func() { close(release) })
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	a.request(t, "POST", path, nil, nil, 409)
	var out schema.SmipOutboxList
	a.request(t, "GET", a.base()+"/outbox", nil, &out, 200)
	if len(out.Items) != 1 || out.Items[0].State != smip.Blocked || out.Items[0].Attempts != 1 {
		t.Fatal("cancellation rewrote attempted packet")
	}
}

func TestSmipAppCancelRacesDispatchWithoutFalseRecall(t *testing.T) {
	a := newSmipApp(t, "race-sender.example", "race-receiver.example")
	b := newSmipApp(t, "race-receiver.example", "race-sender.example")
	binding := a.binding(t, b.config.Domain, uuid.NewString())
	var contacts atomic.Int32
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacts.Add(1); w.WriteHeader(http.StatusForbidden) }))
	target.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	target.StartTLS()
	t.Cleanup(target.Close)
	smipSendingGateway(t, a, b, target)
	var attempted int32
	for i := 0; i < 8; i++ {
		var queued schema.SmipOutbound
		a.request(t, "POST", a.base()+"/outbox", schema.SmipSendInput{TransactionID: uuid.NewString(), BindingID: binding.ID, Body: smipText("concurrent cancellation")}, &queued, 200)
		raw, err := json.Marshal(schema.SmipDispatchJob{ID: queued.ID})
		if err != nil {
			t.Fatal(err)
		}
		start, done := make(chan struct{}), make(chan error, 1)
		go func() { <-start; done <- federation.Dispatch(a.server.Context(), raw) }()
		close(start)
		response := a.server.JSON(t, "POST", a.base()+"/outbox/"+queued.ID+"/cancel", nil, nil)
		if err = <-done; err != nil {
			t.Fatal(err)
		}
		var state string
		var attempts int
		if err = db.From(a.server.Context()).QueryRow(a.server.Context(), "SELECT state,attempts FROM smip_outbox WHERE id=$1", queued.ID).Scan(&state, &attempts); err != nil {
			t.Fatal(err)
		}
		switch response.StatusCode {
		case 200:
			if state != federation.Cancelled || attempts != 0 {
				t.Fatal("successful cancel permitted HTTP")
			}
		case 409:
			if state != smip.Blocked || attempts != 1 {
				t.Fatal("dispatch winner lost attempt")
			}
			attempted++
		default:
			t.Fatalf("cancel race returned %d", response.StatusCode)
		}
	}
	if contacts.Load() != attempted {
		t.Fatal("HTTP contact count contradicts cancellation states")
	}
}
