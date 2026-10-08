//go:build unix

package smip

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
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

const epoch int64 = 1800000000

func signing(seed byte) SigningKey {
	return SigningKey{Private: ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, 32)), NotBefore: epoch - 1000, NotAfter: epoch + int64(30*24*time.Hour/time.Second)}
}
func trusted(key SigningKey) map[string]Key {
	pub := key.Private.Public().(ed25519.PublicKey)
	return map[string]Key{KeyID(pub): {Public: pub, NotBefore: key.NotBefore, NotAfter: key.NotAfter}}
}
func packet(t *testing.T, id, from, to, kind string, key SigningKey) Packet {
	t.Helper()
	e := Envelope{Version: Version, ID: id, From: from, To: to, Sender: "alice@" + from, Recipient: "bob@" + to, Stream: "conversation-1", Kind: kind, Created: epoch, Expires: epoch + 3600, Payload: Encode([]byte("hello from another server"))}
	if kind == "file" {
		e.Name = Encode([]byte("résumé.bin"))
		e.Payload = Encode([]byte{0, 255, 7, 3, 0, 9})
	}
	p, err := Sign(e, key.Private)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func inbox(t *testing.T) *FileInbox {
	t.Helper()
	s, err := OpenInbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func receiver(t *testing.T, domain string, s Inbox, key SigningKey, peers map[string]map[string]Key, now func() time.Time) *Receiver {
	t.Helper()
	r, err := NewReceiver(domain, s, key, peers, func(e Envelope) bool { return e.Recipient == "bob@"+domain && e.Stream == "conversation-1" }, now)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func localSend(r http.Handler, p Packet) *httptest.ResponseRecorder {
	b, _ := json.Marshal(p)
	req := httptest.NewRequest("POST", MessagePath, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func server(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(h)
	s.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	s.Config.ReadHeaderTimeout = 5 * time.Second
	s.Config.ReadTimeout = 30 * time.Second
	s.Config.WriteTimeout = 30 * time.Second
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}
func client(t *testing.T, s *httptest.Server, domain string, keys map[string]Key) *Client {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(s.Certificate())
	c, err := NewClient(s.URL, domain, keys, &tls.Config{RootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}
func TestTwoServersChatAndFiles(t *testing.T) {
	a, b := signing(1), signing(2)
	ia, ib := inbox(t), inbox(t)
	now := func() time.Time { return time.Unix(epoch+1, 0) }
	sa := server(t, receiver(t, "a.example", ia, a, map[string]map[string]Key{"b.example": trusted(b)}, now))
	sb := server(t, receiver(t, "b.example", ib, b, map[string]map[string]Key{"a.example": trusted(a)}, now))
	ca, cb := client(t, sa, "a.example", trusted(a)), client(t, sb, "b.example", trusted(b))
	for _, kind := range []string{"chat", "file"} {
		for _, direction := range []struct {
			from, to string
			k        SigningKey
			c        *Client
			s        *FileInbox
		}{{"a.example", "b.example", a, cb, ib}, {"b.example", "a.example", b, ca, ia}} {
			p := packet(t, kind, direction.from, direction.to, kind, direction.k)
			receipt, err := direction.c.Send(context.Background(), p)
			if err != nil {
				t.Fatal(err)
			}
			key := trusted(direction.k)[p.Envelope.KeyID]
			if err = p.Verify(key); err != nil {
				t.Fatal(err)
			}
			record, found, err := direction.s.Get(direction.from, p.Envelope.ID)
			if err != nil || !found {
				t.Fatalf("missing durable record: %v", err)
			}
			body, err := record.Packet.Envelope.Bytes()
			want, _ := p.Envelope.Bytes()
			if err != nil || !bytes.Equal(body, want) {
				t.Fatal("file/chat bytes changed")
			}
			if receipt.Digest != record.Receipt.Digest || receipt.Signature != record.Receipt.Signature {
				t.Fatal("receipt not durable")
			}
		}
	}
}
func TestLostReceiptOfflineSenderAndReceiverRestart(t *testing.T) {
	a, b := signing(1), signing(2)
	root := t.TempDir()
	store, err := OpenInbox(root)
	if err != nil {
		t.Fatal(err)
	}
	r := receiver(t, "b.example", store, b, map[string]map[string]Key{"a.example": trusted(a)}, func() time.Time { return time.Unix(epoch+1, 0) })
	var lose atomic.Bool
	lose.Store(true)
	s := server(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if lose.Swap(false) {
			capture := httptest.NewRecorder()
			r.ServeHTTP(capture, req)
			if capture.Code != http.StatusCreated {
				t.Errorf("first acceptance: %d", capture.Code)
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
	c := client(t, s, "b.example", trusted(b))
	p := packet(t, "lost-ack", "a.example", "b.example", "chat", a)
	if _, err = c.Send(context.Background(), p); err == nil {
		t.Fatal("lost acknowledgement must be uncertain")
	}
	// There is intentionally no origin callback/server. The signed packet is
	// retained unchanged by the caller and the receiver can restart offline.
	store.Close()
	restored, err := OpenInbox(root)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	r = receiver(t, "b.example", restored, b, map[string]map[string]Key{"a.example": trusted(a)}, func() time.Time { return time.Unix(epoch+5000, 0) })
	receipt, err := c.Send(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	record, found, err := restored.Get("a.example", "lost-ack")
	if err != nil || !found || receipt.Accepted != epoch+1 || receipt.Signature != record.Receipt.Signature {
		t.Fatal("retry did not return original committed receipt")
	}
	paths, _ := filepath.Glob(filepath.Join(root, "*.json"))
	if len(paths) != 1 {
		t.Fatal("duplicate inbox record")
	}
}
func TestEnvelopeRejectionAndConflict(t *testing.T) {
	a, b := signing(1), signing(2)
	store := inbox(t)
	r := receiver(t, "b.example", store, b, map[string]map[string]Key{"a.example": trusted(a)}, func() time.Time { return time.Unix(epoch+1, 0) })
	original := packet(t, "one", "a.example", "b.example", "chat", a)
	if w := localSend(r, original); w.Code != http.StatusCreated {
		t.Fatalf("initial: %d %s", w.Code, w.Body.String())
	}
	if w := localSend(r, original); w.Code != http.StatusOK {
		t.Fatal("duplicate was not idempotent")
	}
	changed := original.Envelope
	changed.Payload = Encode([]byte("different"))
	conflict, err := Sign(changed, a.Private)
	if err != nil {
		t.Fatal(err)
	}
	if w := localSend(r, conflict); w.Code != http.StatusConflict {
		t.Fatal("changed content reused ID")
	}
	cases := []struct {
		name   string
		change func(*Envelope)
		status int
	}{
		{"wrong receiver", func(e *Envelope) { e.To = "c.example"; e.Recipient = "bob@c.example" }, http.StatusForbidden},
		{"unknown recipient", func(e *Envelope) { e.Recipient = "outsider@b.example" }, http.StatusForbidden},
		{"unauthorized stream", func(e *Envelope) { e.Stream = "secret-room" }, http.StatusForbidden},
		{"expired", func(e *Envelope) { e.Created = epoch - 100; e.Expires = epoch }, http.StatusUnprocessableEntity},
		{"future", func(e *Envelope) { e.Created = epoch + 1000; e.Expires = epoch + 2000 }, http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := original.Envelope
			e.ID = strings.ReplaceAll(tc.name, " ", "-")
			tc.change(&e)
			p, err := Sign(e, a.Private)
			if err != nil {
				t.Fatal(err)
			}
			w := localSend(r, p)
			if w.Code != tc.status {
				t.Fatalf("got %d want %d", w.Code, tc.status)
			}
			_, found, err := store.Get(e.From, e.ID)
			if err != nil || found {
				t.Fatal("rejected packet stored")
			}
		})
	}
	tampered := original
	tampered.Envelope.Payload = Encode([]byte("tampered"))
	if w := localSend(r, tampered); w.Code != http.StatusUnauthorized {
		t.Fatal("tampering accepted")
	}
	keys := trusted(a)
	key := keys[original.Envelope.KeyID]
	key.Revoked = true
	keys[original.Envelope.KeyID] = key
	if err := r.SetPeer("a.example", keys); err != nil {
		t.Fatal(err)
	}
	if w := localSend(r, original); w.Code != http.StatusUnauthorized {
		t.Fatal("revoked key accepted even on retry")
	}
}
func TestRotationAndHistoricalReceipts(t *testing.T) {
	a, b, newA, newB := signing(1), signing(2), signing(3), signing(4)
	s := inbox(t)
	r := receiver(t, "b.example", s, b, map[string]map[string]Key{"a.example": trusted(a)}, func() time.Time { return time.Unix(epoch+1, 0) })
	old := packet(t, "before-rotation", "a.example", "b.example", "chat", a)
	first := localSend(r, old)
	if first.Code != http.StatusCreated {
		t.Fatal(first.Code)
	}
	both := trusted(a)
	for id, key := range trusted(newA) {
		both[id] = key
	}
	if err := r.SetPeer("a.example", both); err != nil {
		t.Fatal(err)
	}
	if err := r.SetSigningKey(newB); err != nil {
		t.Fatal(err)
	}
	newer := packet(t, "after-rotation", "a.example", "b.example", "chat", newA)
	result := localSend(r, newer)
	if result.Code != http.StatusCreated {
		t.Fatal(result.Code)
	}
	var receipt Receipt
	if err := json.Unmarshal(result.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if err := receipt.Verify(newer, trusted(newB)[receipt.KeyID]); err != nil {
		t.Fatal(err)
	}
	retry := localSend(r, old)
	if !bytes.Equal(first.Body.Bytes(), retry.Body.Bytes()) {
		t.Fatal("rotation changed original receipt")
	}
	if err := receipt.Verify(old, trusted(newB)[receipt.KeyID]); err == nil {
		t.Fatal("receipt substituted between messages")
	}
	untrusted := trusted(newB)[receipt.KeyID]
	untrusted.Revoked = true
	if err := receipt.Verify(newer, untrusted); err == nil {
		t.Fatal("revoked receipt key accepted")
	}
}
func TestCanonicalLimitsAndTransportPolicy(t *testing.T) {
	a, b := signing(1), signing(2)
	r := receiver(t, "b.example", inbox(t), b, map[string]map[string]Key{"a.example": trusted(a)}, func() time.Time { return time.Unix(epoch+1, 0) })
	p := packet(t, "canonical", "a.example", "b.example", "chat", a)
	raw, _ := json.Marshal(p)
	for _, bad := range [][]byte{append(raw, []byte("\n")...), bytes.Replace(raw, []byte(`"signature":`), []byte(`"extra":1,"signature":`), 1), bytes.Replace(raw, []byte(`"id":"canonical"`), []byte(`"id":"ignored","id":"canonical"`), 1)} {
		req := httptest.NewRequest("POST", MessagePath, bytes.NewReader(bad))
		req.Header.Set("Content-Type", "application/json")
		req.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatal("noncanonical packet accepted")
		}
	}
	req := httptest.NewRequest("POST", MessagePath, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatal("plaintext accepted")
	}
	req.TLS = &tls.ConnectionState{Version: tls.VersionTLS12}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatal("old TLS accepted")
	}
	req.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	req.ContentLength = MaxWire + 1
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatal("oversize body accepted")
	}
	e := p.Envelope
	e.Payload = Encode(bytes.Repeat([]byte{'x'}, MaxChat+1))
	if _, err := Sign(e, a.Private); err == nil {
		t.Fatal("oversize chat accepted")
	}
	e = p.Envelope
	e.Payload = Encode([]byte{255})
	if _, err := Sign(e, a.Private); err == nil {
		t.Fatal("invalid UTF-8 chat accepted")
	}
	e = p.Envelope
	e.Expires = e.Created + int64(MaxLifetime/time.Second) + 1
	if _, err := Sign(e, a.Private); err == nil {
		t.Fatal("unbounded lifetime accepted")
	}
	for _, endpoint := range []string{"http://a.example", "https://a.example/path", "https://user:pass@a.example", "https://a.example?redirect=1", "https://a.example/#fragment"} {
		if _, err := NewClient(endpoint, "b.example", trusted(b), nil); err == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
	if _, err := NewClient("https://b.example", "b.example", trusted(b), &tls.Config{InsecureSkipVerify: true}); err == nil {
		t.Fatal("disabled TLS verification accepted")
	}
	// A redirect is not followed even to another allowed HTTPS endpoint.
	redirected := server(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "https://other.example"+MessagePath, http.StatusTemporaryRedirect)
	}))
	c := client(t, redirected, "b.example", trusted(b))
	if _, err := c.Send(context.Background(), p); err == nil {
		t.Fatal("redirect accepted")
	}
}

type failingInbox struct{}

func (failingInbox) Get(string, string) (Record, bool, error) {
	return Record{}, false, errors.New("unavailable disk")
}
func (failingInbox) Put(Record) (Record, bool, error) {
	return Record{}, false, errors.New("unavailable disk")
}
func TestUnavailableStorageNeverAcknowledged(t *testing.T) {
	a, b := signing(1), signing(2)
	r := receiver(t, "b.example", failingInbox{}, b, map[string]map[string]Key{"a.example": trusted(a)}, func() time.Time { return time.Unix(epoch+1, 0) })
	if w := localSend(r, packet(t, "storage-fault", "a.example", "b.example", "chat", a)); w.Code != http.StatusServiceUnavailable {
		t.Fatal("storage error acknowledged")
	}
}
func TestInboxExclusiveLockAtomicConcurrencyAndCorruption(t *testing.T) {
	root := t.TempDir()
	s, err := OpenInbox(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if second, err := OpenInbox(root); err == nil {
		second.Close()
		t.Fatal("multiple inbox writers allowed")
	}
	p := packet(t, "parallel", "a.example", "b.example", "file", signing(1))
	r := Record{p, receiptFor(p, signing(2).Private, epoch+1)}
	var count atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stored, created, err := s.Put(r)
			if err != nil {
				t.Error(err)
				return
			}
			if stored.Receipt.Signature != r.Receipt.Signature {
				t.Error("receipt changed")
			}
			if created {
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatal("non-atomic idempotency")
	}
	path := filepath.Join(root, recordName("a.example", "parallel"))
	if err := os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("a.example", "parallel"); err == nil {
		t.Fatal("corrupt record accepted")
	}
	private := filepath.Join(t.TempDir(), "public")
	if err := os.Mkdir(private, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(private, 0755); err != nil {
		t.Fatal(err)
	}
	if opened, err := OpenInbox(private); err == nil {
		opened.Close()
		t.Fatal("public inbox directory accepted")
	}
}
func TestInboxSurvivesProcessExit(t *testing.T) {
	if root := os.Getenv("SMIP_CRASH_FIXTURE_ROOT"); root != "" {
		s, err := OpenInbox(root)
		if err != nil {
			t.Fatal(err)
		}
		p := packet(t, "crash", "a.example", "b.example", "file", signing(1))
		if _, _, err := s.Put(Record{p, receiptFor(p, signing(2).Private, epoch+1)}); err != nil {
			t.Fatal(err)
		}
		os.Exit(0) // No Close: kernel releases the lock after process death.
	}
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestInboxSurvivesProcessExit$")
	cmd.Env = append(os.Environ(), "SMIP_CRASH_FIXTURE_ROOT="+root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("crash fixture failed: %v %s", err, output)
	}
	s, err := OpenInbox(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, found, err := s.Get("a.example", "crash")
	if err != nil || !found {
		t.Fatal("durable receipt lost after process exit")
	}
	want := packet(t, "crash", "a.example", "b.example", "file", signing(1))
	if r.Packet.Digest() != want.Digest() {
		t.Fatal("payload changed after process exit")
	}
}

func TestPublicInteroperabilityVector(t *testing.T) {
	var vector struct {
		Packet               Packet  `json:"packet"`
		Receipt              Receipt `json:"receipt"`
		SenderPublic         string  `json:"senderPublic"`
		ReceiverPublic       string  `json:"receiverPublic"`
		EnvelopeSigningBytes string  `json:"envelopeSigningBytes"`
		ReceiptSigningBytes  string  `json:"receiptSigningBytes"`
	}
	raw, err := os.ReadFile("testdata/file-v0.1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	p := packet(t, "interop-file", "a.example", "b.example", "file", signing(1))
	if p != vector.Packet {
		t.Fatal("published packet vector differs")
	}
	if !bytes.Equal(p.Envelope.signingBytes(), mustDecode(t, vector.EnvelopeSigningBytes)) || !bytes.Equal(vector.Receipt.signingBytes(), mustDecode(t, vector.ReceiptSigningBytes)) {
		t.Fatal("canonical signing bytes differ")
	}
	if vector.SenderPublic != Encode(signing(1).Private.Public().(ed25519.PublicKey)) || vector.ReceiverPublic != Encode(signing(2).Private.Public().(ed25519.PublicKey)) {
		t.Fatal("published key differs")
	}
	if err := vector.Receipt.Verify(p, trusted(signing(2))[vector.Receipt.KeyID]); err != nil {
		t.Fatal(err)
	}
}
func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := decode(s, MaxWire)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestMaximumFileOverTLS(t *testing.T) {
	a, b := signing(1), signing(2)
	store := inbox(t)
	r := receiver(t, "b.example", store, b, map[string]map[string]Key{"a.example": trusted(a)}, func() time.Time { return time.Unix(epoch+1, 0) })
	s := server(t, r)
	c := client(t, s, "b.example", trusted(b))
	e := packet(t, "maximum-file", "a.example", "b.example", "file", a).Envelope
	payload := bytes.Repeat([]byte{0, 255, 4, 19}, MaxPayload/4)
	e.Payload = Encode(payload)
	p, err := Sign(e, a.Private)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Send(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	record, found, err := store.Get(e.From, e.ID)
	if err != nil || !found {
		t.Fatal("maximum file not durable")
	}
	body, err := record.Packet.Envelope.Bytes()
	if err != nil || !bytes.Equal(body, payload) {
		t.Fatal("maximum file bytes changed")
	}
	e.ID = "oversize-file"
	e.Payload = Encode(append(payload, 1))
	if _, err := Sign(e, a.Private); err == nil {
		t.Fatal("oversize file accepted")
	}
}
func TestClientRequiresAuthenticReceiptAndClassifiesRetries(t *testing.T) {
	a, b := signing(1), signing(2)
	p := packet(t, "receipt-test", "a.example", "b.example", "chat", a)
	for _, kind := range []string{"forged", "wrong-message", "unknown-key", "unavailable", "denied"} {
		t.Run(kind, func(t *testing.T) {
			s := server(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if kind == "unavailable" {
					http.Error(w, "busy", http.StatusServiceUnavailable)
					return
				}
				if kind == "denied" {
					http.Error(w, "denied", http.StatusForbidden)
					return
				}
				r := receiptFor(p, b.Private, epoch+1)
				switch kind {
				case "forged":
					r.Signature = Encode(make([]byte, 64))
				case "wrong-message":
					r.ID = "other-message"
				case "unknown-key":
					r = receiptFor(p, signing(3).Private, epoch+1)
				}
				writeReceipt(w, r, http.StatusCreated)
			}))
			c := client(t, s, "b.example", trusted(b))
			_, err := c.Send(context.Background(), p)
			if err == nil {
				t.Fatal("invalid acknowledgement accepted")
			}
			if kind == "unavailable" || kind == "denied" {
				var status *HTTPError
				if !errors.As(err, &status) || status.Retryable != (kind == "unavailable") {
					t.Fatal("wrong retry classification")
				}
			}
		})
	}
}
func TestAdmissionBackpressureAndSigningKeyExpiry(t *testing.T) {
	a, b := signing(1), signing(2)
	store := inbox(t)
	r := receiver(t, "b.example", store, b, map[string]map[string]Key{"a.example": trusted(a)}, func() time.Time { return time.Unix(epoch+1, 0) })
	p := packet(t, "backpressure", "a.example", "b.example", "chat", a)
	r.slots <- struct{}{}
	r.slots <- struct{}{}
	w := localSend(r, p)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "1" {
		t.Fatal("unbounded request concurrency")
	}
	<-r.slots
	<-r.slots
	b.NotAfter = epoch
	if err := r.SetSigningKey(b); err != nil {
		t.Fatal(err)
	}
	if w := localSend(r, p); w.Code != http.StatusServiceUnavailable {
		t.Fatal("expired signing key acknowledged new packet")
	}
	if _, found, err := store.Get(p.Envelope.From, p.Envelope.ID); err != nil || found {
		t.Fatal("unacknowledgeable message stored")
	}
}

func TestKeyRevocationFencesInFlightAdmission(t *testing.T) {
	a, b := signing(1), signing(2)
	entered, release := make(chan struct{}), make(chan struct{})
	r, err := NewReceiver("b.example", inbox(t), b, map[string]map[string]Key{"a.example": trusted(a)}, func(e Envelope) bool { close(entered); <-release; return true }, func() time.Time { return time.Unix(epoch+1, 0) })
	if err != nil {
		t.Fatal(err)
	}
	p := packet(t, "revocation-fence", "a.example", "b.example", "chat", a)
	accepted := make(chan int, 1)
	go func() { accepted <- localSend(r, p).Code }()
	<-entered
	keys := trusted(a)
	key := keys[p.Envelope.KeyID]
	key.Revoked = true
	keys[p.Envelope.KeyID] = key
	began, changed := make(chan struct{}), make(chan error, 1)
	go func() { close(began); changed <- r.SetPeer("a.example", keys) }()
	<-began
	select {
	case <-changed:
		t.Fatal("revocation returned while old admission could still commit")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if status := <-accepted; status != http.StatusCreated {
		t.Fatal("in-flight admission failed")
	}
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	if w := localSend(r, p); w.Code != http.StatusUnauthorized {
		t.Fatal("revoked key accepted after update completed")
	}
}

type failingCommitInbox struct{}

func (failingCommitInbox) Get(string, string) (Record, bool, error) { return Record{}, false, nil }
func (failingCommitInbox) Put(Record) (Record, bool, error) {
	return Record{}, false, errors.New("commit failed")
}
func TestCommitFailureNeverAcknowledged(t *testing.T) {
	a, b := signing(1), signing(2)
	r := receiver(t, "b.example", failingCommitInbox{}, b, map[string]map[string]Key{"a.example": trusted(a)}, func() time.Time { return time.Unix(epoch+1, 0) })
	if w := localSend(r, packet(t, "commit-fault", "a.example", "b.example", "chat", a)); w.Code != http.StatusServiceUnavailable {
		t.Fatal("failed commit acknowledged")
	}
}
func TestDomainIdentityRejectsAmbiguity(t *testing.T) {
	for _, domain := range []string{"127.0.0.1", "LOCAL.example", "singlelabel", "a..example", "-a.example", "a-.example", "a.example.", "x@example.org"} {
		if validDomain(domain) {
			t.Fatalf("ambiguous domain accepted: %s", domain)
		}
	}
	for _, domain := range []string{"a.example", "xn--bcher-kva.example", "sub.a.example"} {
		if !validDomain(domain) {
			t.Fatalf("canonical domain rejected: %s", domain)
		}
	}
}
