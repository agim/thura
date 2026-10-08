//go:build unix

package smip

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func outbox(t *testing.T, root string) *FileOutbox {
	t.Helper()
	s, err := OpenOutbox(root, "a.example", trusted(signing(1)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func TestOutboxPersistsPacketBeforeNetworkAndAcceptedReceipt(t *testing.T) {
	root := t.TempDir()
	o := outbox(t, root)
	p := packet(t, "persist-first", "a.example", "b.example", "file", signing(1))
	queued, err := o.Enqueue(p, time.Unix(epoch+1, 0))
	if err != nil || queued.State != Pending {
		t.Fatal("packet not queued", err)
	}
	if _, err := OpenOutbox(root, "a.example", trusted(signing(1))); err == nil {
		t.Fatal("multiple outbox writers")
	}
	o.Close()
	o = outbox(t, root)
	var contacts atomic.Int32
	receive := receiver(t, "b.example", inbox(t), signing(2), map[string]map[string]Key{"a.example": trusted(signing(1))}, func() time.Time { return time.Unix(epoch+2, 0) })
	s := server(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		contacts.Add(1)
		r, found, err := o.Get(p.Envelope.ID)
		if err != nil || !found || r.State != Uncertain || r.Attempts != 1 || r.Packet != p {
			t.Error("network preceded durable unchanged attempt marker")
			http.Error(w, "invalid attempt", http.StatusServiceUnavailable)
			return
		}
		receive.ServeHTTP(w, req)
	}))
	c := client(t, s, "b.example", trusted(signing(2)))
	accepted, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(epoch+2, 0))
	if err != nil || accepted.State != Accepted {
		t.Fatal("acceptance not persisted", err)
	}
	// Returned receipt public keys cannot modify the client's pinned trust set.
	accepted.ReceiptKey.Public[0] ^= 255
	key := c.keys[accepted.Receipt.KeyID]
	if err := accepted.Receipt.Verify(p, key); err != nil {
		t.Fatal("outbox leaked a mutable client key")
	}
	o.Close()
	o = outbox(t, root)
	stored, found, err := o.Get(p.Envelope.ID)
	if err != nil || !found || stored.State != Accepted || stored.Packet != p || stored.Receipt == nil {
		t.Fatal("acceptance lost after restart", err)
	}
	if _, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(epoch+3, 0)); err != nil {
		t.Fatal(err)
	}
	if contacts.Load() != 1 {
		t.Fatal("accepted message sent again")
	}
	if _, err := o.Resume(p.Envelope.ID, time.Unix(epoch+3, 0)); !errors.Is(err, ErrTerminal) {
		t.Fatal("accepted message resumed")
	}
}
func TestOutboxLostAcknowledgementRestartAndExpiredReconciliation(t *testing.T) {
	root := t.TempDir()
	o := outbox(t, root)
	remote := inbox(t)
	r := receiver(t, "b.example", remote, signing(2), map[string]map[string]Key{"a.example": trusted(signing(1))}, func() time.Time { return time.Unix(epoch+2, 0) })
	var lose atomic.Bool
	lose.Store(true)
	s := server(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if lose.Swap(false) {
			capture := httptest.NewRecorder()
			r.ServeHTTP(capture, req)
			if capture.Code != http.StatusCreated {
				t.Error("first remote acceptance failed")
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
			return
		}
		r.ServeHTTP(w, req)
	}))
	c := client(t, s, "b.example", trusted(signing(2)))
	p := packet(t, "lost-outbox-ack", "a.example", "b.example", "chat", signing(1))
	if _, err := o.Enqueue(p, time.Unix(epoch+1, 0)); err != nil {
		t.Fatal(err)
	}
	uncertain, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(epoch+2, 0))
	if err == nil || uncertain.State != Uncertain || uncertain.Attempts != 1 || uncertain.Receipt != nil {
		t.Fatal("lost receipt claimed acceptance")
	}
	if _, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(epoch+2, 0)); !errors.Is(err, ErrNotDue) {
		t.Fatal("backoff ignored")
	}
	o.Close()
	o = outbox(t, root)
	accepted, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(p.Envelope.Expires+1, 0))
	if err != nil || accepted.State != Accepted || accepted.Attempts != 2 || accepted.Packet != p {
		t.Fatal("expired uncertain packet not reconciled unchanged", err)
	}
	paths, err := filepath.Glob(filepath.Join(remote.root, "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatal("duplicate remote delivery")
	}
}
func TestOutboxLocalReceiptCommitFailureRecovers(t *testing.T) {
	root := filepath.Join(t.TempDir(), "outbox")
	o := outbox(t, root)
	remote := inbox(t)
	r := receiver(t, "b.example", remote, signing(2), map[string]map[string]Key{"a.example": trusted(signing(1))}, func() time.Time { return time.Unix(epoch+2, 0) })
	var fail atomic.Bool
	fail.Store(true)
	s := server(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		capture := httptest.NewRecorder()
		r.ServeHTTP(capture, req)
		if fail.Swap(false) {
			if err := os.Rename(root, root+"-moved"); err != nil {
				t.Error(err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(capture.Code)
		if _, err := w.Write(capture.Body.Bytes()); err != nil {
			t.Error(err)
		}
	}))
	c := client(t, s, "b.example", trusted(signing(2)))
	p := packet(t, "local-commit-fail", "a.example", "b.example", "file", signing(1))
	if _, err := o.Enqueue(p, time.Unix(epoch+1, 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(epoch+2, 0)); err == nil {
		t.Fatal("failed local receipt commit reported durable success")
	}
	if err := os.Rename(root+"-moved", root); err != nil {
		t.Fatal(err)
	}
	before, found, err := o.Get(p.Envelope.ID)
	if err != nil || !found || before.State != Uncertain {
		t.Fatal("uncertain attempt lost", err)
	}
	after, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(before.NextAttempt, 0))
	if err != nil || after.State != Accepted {
		t.Fatal("local receipt commit not recovered", err)
	}
}
func TestOutboxPermanentErrorRemainsBlockedAndExplicitResumePreservesPacket(t *testing.T) {
	o := outbox(t, t.TempDir())
	remote := inbox(t)
	r := receiver(t, "b.example", remote, signing(2), map[string]map[string]Key{"a.example": trusted(signing(1))}, func() time.Time { return time.Unix(epoch+2, 0) })
	var denied atomic.Bool
	denied.Store(true)
	s := server(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if denied.Load() {
			http.Error(w, "policy denied", http.StatusForbidden)
			return
		}
		r.ServeHTTP(w, req)
	}))
	c := client(t, s, "b.example", trusted(signing(2)))
	p := packet(t, "blocked", "a.example", "b.example", "chat", signing(1))
	if _, err := o.Enqueue(p, time.Unix(epoch+1, 0)); err != nil {
		t.Fatal(err)
	}
	stopped, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(epoch+2, 0))
	if err == nil || stopped.State != Blocked || stopped.Attempts != 1 || stopped.LastStatus != http.StatusForbidden || stopped.NextAttempt != 0 {
		t.Fatal("permanent error not paused")
	}
	if _, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(epoch+3, 0)); !errors.Is(err, ErrNotDue) {
		t.Fatal("blocked message retried automatically")
	}
	denied.Store(false)
	resumed, err := o.Resume(p.Envelope.ID, time.Unix(epoch+3, 0))
	if err != nil || resumed.State != Uncertain || resumed.Packet != p {
		t.Fatal("resume changed uncertainty or packet", err)
	}
	accepted, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(epoch+3, 0))
	if err != nil || accepted.State != Accepted || accepted.Packet != p {
		t.Fatal("resumed packet not accepted", err)
	}
}
func TestOutboxUnsentExpiryCancellationAndDestinationChecks(t *testing.T) {
	o := outbox(t, t.TempDir())
	p := packet(t, "unsent-expiry", "a.example", "b.example", "chat", signing(1))
	if _, err := o.Enqueue(p, time.Unix(epoch+1, 0)); err != nil {
		t.Fatal(err)
	}
	var sends atomic.Int32
	s := server(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sends.Add(1)
		http.Error(w, "unexpected", http.StatusServiceUnavailable)
	}))
	c := client(t, s, "b.example", trusted(signing(2)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := o.Dispatch(ctx, p.Envelope.ID, c, time.Unix(epoch+2, 0)); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
	wrong := client(t, s, "c.example", trusted(signing(2)))
	if _, err := o.Dispatch(context.Background(), p.Envelope.ID, wrong, time.Unix(epoch+2, 0)); err == nil {
		t.Fatal("wrong configured peer accepted")
	}
	expired, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(p.Envelope.Expires, 0))
	if err != nil || expired.State != Expired || expired.Attempts != 0 {
		t.Fatal("unsent expired packet attempted", err)
	}
	if sends.Load() != 0 {
		t.Fatal("invalid/unsent expired packet transmitted")
	}
	if _, err := o.Resume(p.Envelope.ID, time.Unix(p.Envelope.Expires+1, 0)); !errors.Is(err, ErrTerminal) {
		t.Fatal("expired unsent packet resumed")
	}
}
func TestOutboxIntegrityQueuePagesAndKeyRevocation(t *testing.T) {
	o := outbox(t, t.TempDir())
	p := packet(t, "queue-a", "a.example", "b.example", "chat", signing(1))
	if _, err := o.Enqueue(p, time.Unix(epoch+1, 0)); err != nil {
		t.Fatal(err)
	}
	same, err := o.Enqueue(p, time.Unix(epoch+1, 0))
	if err != nil || same.Packet != p || same.Attempts != 0 {
		t.Fatal("enqueue not idempotent")
	}
	e := p.Envelope
	e.Payload = Encode([]byte("changed"))
	changed, err := Sign(e, signing(1).Private)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Enqueue(changed, time.Unix(epoch+1, 0)); !errors.Is(err, ErrConflict) {
		t.Fatal("same ID overwritten")
	}
	second := packet(t, "queue-b", "a.example", "b.example", "file", signing(1))
	if _, err := o.Enqueue(second, time.Unix(epoch+2, 0)); err != nil {
		t.Fatal(err)
	}
	due, err := o.Due(time.Unix(epoch+2, 0), 1)
	if err != nil || len(due) != 1 || due[0].ID != p.Envelope.ID {
		t.Fatal("due page order/limit wrong", err)
	}
	if _, err := o.Due(time.Unix(epoch+2, 0), 101); err == nil {
		t.Fatal("unbounded page allowed")
	}
	keys := trusted(signing(1))
	k := keys[p.Envelope.KeyID]
	k.Revoked = true
	keys[p.Envelope.KeyID] = k
	if err := o.SetKeys(keys); err != nil {
		t.Fatal(err)
	}
	var contacts atomic.Int32
	s := server(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		contacts.Add(1)
		http.Error(w, "unexpected", http.StatusServiceUnavailable)
	}))
	c := client(t, s, "b.example", trusted(signing(2)))
	blocked, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(epoch+3, 0))
	if err == nil || blocked.State != Blocked || blocked.Attempts != 0 || contacts.Load() != 0 {
		t.Fatal("revoked local origin key transmitted")
	}
	if _, found, err := o.Get(p.Envelope.ID); err != nil || !found {
		t.Fatal("historical revoked-key record unreadable", err)
	}
	path := filepath.Join(o.journal.root, recordName("a.example", p.Envelope.ID))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"state":"blocked"`), []byte(`"state":"accepted"`), 1)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := o.Get(p.Envelope.ID); err == nil {
		t.Fatal("forged accepted state without receipt accepted")
	}
}
func TestOutboxConcurrentEnqueueAndProcessExit(t *testing.T) {
	root := os.Getenv("SMIP_OUTBOX_EXIT_ROOT")
	if root != "" {
		o := outbox(t, root)
		p := packet(t, "process-exit", "a.example", "b.example", "file", signing(1))
		if _, err := o.Enqueue(p, time.Unix(epoch+1, 0)); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	}
	root = t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestOutboxConcurrentEnqueueAndProcessExit$")
	cmd.Env = append(os.Environ(), "SMIP_OUTBOX_EXIT_ROOT="+root)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("outbox exit fixture: %v %s", err, raw)
	}
	o := outbox(t, root)
	p := packet(t, "process-exit", "a.example", "b.example", "file", signing(1))
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := o.Enqueue(p, time.Unix(epoch+1, 0))
			if err != nil || r.Packet != p {
				t.Error("concurrent duplicate enqueue failed", err)
			}
		}()
	}
	wg.Wait()
	record, found, err := o.Get(p.Envelope.ID)
	if err != nil || !found || record.State != Pending || record.Packet != p {
		t.Fatal("queued payload did not survive process exit", err)
	}
	names, err := o.entries()
	if err != nil || len(names) != 1 {
		t.Fatal("duplicate durable queue records")
	}
	// A private state file is fully decodable without any private signing key.
	raw, err := os.ReadFile(filepath.Join(root, recordName("a.example", p.Envelope.ID)))
	if err != nil {
		t.Fatal(err)
	}
	var disk Outbound
	if err := json.Unmarshal(raw, &disk); err != nil || disk.Packet != p {
		t.Fatal("invalid journal representation")
	}
}

func TestOutboxRetryableErrorAndForgedReceiptStayUncertain(t *testing.T) {
	o := outbox(t, t.TempDir())
	p := packet(t, "retryable", "a.example", "b.example", "chat", signing(1))
	if _, err := o.Enqueue(p, time.Unix(epoch+1, 0)); err != nil {
		t.Fatal(err)
	}
	r := receiver(t, "b.example", inbox(t), signing(2), map[string]map[string]Key{"a.example": trusted(signing(1))}, func() time.Time { return time.Unix(epoch+2, 0) })
	var contacts atomic.Int32
	s := server(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch contacts.Add(1) {
		case 1:
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
		case 2:
			capture := httptest.NewRecorder()
			r.ServeHTTP(capture, req)
			var receipt Receipt
			if err := json.Unmarshal(capture.Body.Bytes(), &receipt); err != nil {
				t.Error(err)
				return
			}
			receipt.Digest = strings.Repeat("0", 64)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(capture.Code)
			body, err := json.Marshal(receipt)
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := w.Write(body); err != nil {
				t.Error(err)
			}
		default:
			r.ServeHTTP(w, req)
		}
	}))
	c := client(t, s, "b.example", trusted(signing(2)))
	first, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(epoch+2, 0))
	if err == nil || first.State != Uncertain || first.LastStatus != http.StatusServiceUnavailable || first.NextAttempt <= first.Updated || first.NextAttempt > first.Updated+2 {
		t.Fatal("retryable response did not preserve bounded uncertainty", err)
	}
	second, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(first.NextAttempt, 0))
	if err == nil || second.State != Uncertain || second.Attempts != 2 || second.Receipt != nil || second.Packet != p {
		t.Fatal("forged acknowledgement became acceptance", err)
	}
	third, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(second.NextAttempt, 0))
	if err != nil || third.State != Accepted || third.Attempts != 3 || third.Packet != p || contacts.Load() != 3 {
		t.Fatal("uncertain receipt did not reconcile unchanged", err)
	}
}

func TestOutboxUnavailableJournalPreventsNetwork(t *testing.T) {
	root := filepath.Join(t.TempDir(), "outbox")
	o := outbox(t, root)
	p := packet(t, "attempt-commit-fail", "a.example", "b.example", "chat", signing(1))
	if _, err := o.Enqueue(p, time.Unix(epoch+1, 0)); err != nil {
		t.Fatal(err)
	}
	var contacts atomic.Int32
	s := server(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		contacts.Add(1)
		http.Error(w, "unexpected", http.StatusServiceUnavailable)
	}))
	c := client(t, s, "b.example", trusted(signing(2)))
	// Replace the journal path with a regular file to make its durability
	// operations unavailable even when the tests run as root.
	name := filepath.Join(root, recordName("a.example", p.Envelope.ID))
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("unavailable journal"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Dispatch(context.Background(), p.Envelope.ID, c, time.Unix(epoch+2, 0)); err == nil {
		t.Fatal("failed durability barrier allowed dispatch")
	}
	if contacts.Load() != 0 {
		t.Fatal("network preceded durable attempt")
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root+"-moved", root); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var stored Outbound
	if err := json.Unmarshal(raw, &stored); err != nil || stored.State != Pending || stored.Attempts != 0 {
		t.Fatal("failed read barrier changed queued attempt", err)
	}
}
