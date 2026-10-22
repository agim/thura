package tests

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/mail"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/lidzatest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"thura/app"
	q "thura/db/queries/gen"
	"thura/internal/mailbox"
	"thura/internal/smip"
	"thura/internal/smipmail"
	"thura/internal/workspace"
	"thura/schema"
)

type mailNode struct {
	server    *lidzatest.Server
	config    smipmail.Config
	box       schema.Mailbox
	owner     auth.Profile
	workspace schema.Workspace
}

func newMailNode(t *testing.T, domain string, workers ...bool) *mailNode {
	t.Helper()
	if _, err := os.Stat("lidza.json"); err != nil {
		t.Chdir("..")
	}
	var cfg db.Config
	if err := env.Load(".", &cfg); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(cfg.URL)
	if err != nil {
		t.Fatal(err)
	}
	admin := *u
	admin.Path = "/postgres"
	conn, err := pgx.Connect(context.Background(), admin.String())
	if err != nil {
		t.Fatal(err)
	}
	database := "thura_smip_mail_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = conn.Exec(context.Background(), "CREATE DATABASE "+database); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(), "DROP DATABASE "+database); err != nil {
			t.Error(err)
		}
		conn.Close(context.Background())
	})
	u.Path = "/" + database
	t.Setenv("DATABASE_URL", u.String())
	t.Setenv("SMIP_MAIL_ENABLED", "false")
	t.Setenv("JOBS_WORKERS", "0")
	if len(workers) > 0 && workers[0] {
		t.Setenv("JOBS_WORKERS", "1")
	}
	t.Setenv("LIDZA_MASTER_KEY", strings.Repeat("ad", 32))
	t.Setenv("STORAGE_PROVIDER", "local")
	t.Setenv("STORAGE_DIR", t.TempDir())
	pool, err := pgxpool.New(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Migrate(context.Background(), pool, os.DirFS("db/migrations")); err != nil {
		t.Fatal(err)
	}
	pool.Close()
	server := startConfigured(t, app.New(nil))
	ctx := server.Context()
	owner, err := auth.From(ctx).CreateUser(ctx, "owner@"+domain, "Mail owner", "mail meadow river lantern 87344")
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "SMIP mail " + domain, OwnerEmail: owner.Email})
	if err != nil {
		t.Fatal(err)
	}
	box, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: w.ID, Name: "Inbox", Address: "team@" + domain})
	if err != nil {
		t.Fatal(err)
	}
	if r := server.JSON(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: owner.Email, Password: "mail meadow river lantern 87344"}, nil); r.StatusCode != 200 {
		t.Fatal("login", r.StatusCode)
	}
	now := time.Now().Unix()
	seed, _, _ := smipKeys(domain, now)
	return &mailNode{server: server, config: smipmail.Config{Domain: domain, Seed: seed, NotBefore: now - 86400, NotAfter: now + 30*86400}, box: box, owner: owner, workspace: w}
}
func (n *mailNode) base() string {
	return "/api/v1/workspaces/" + n.workspace.ID + "/mailboxes/" + n.box.ID + "/messages"
}
func (n *mailNode) request(t *testing.T, method, path string, in, out any, want int) {
	t.Helper()
	if r := n.server.JSON(t, method, path, in, out); r.StatusCode != want {
		t.Fatalf("%s %s = %d want %d", method, path, r.StatusCode, want)
	}
}
func (n *mailNode) queue(t *testing.T, to string, attachment ...[]byte) schema.MailItem {
	t.Helper()
	subject, text, html := "Secure mail receipt", "Hello from "+n.config.Domain, "<p>Hello from <b>"+n.config.Domain+"</b></p>"
	var draft schema.MailItem
	n.request(t, "POST", n.base(), schema.DraftInput{To: &to, Subject: &subject, Text: &text, HTML: &html}, &draft, 201)
	if len(attachment) > 0 {
		request, err := http.NewRequest("PUT", n.server.URL+n.base()+"/"+draft.ID+"/attachments", bytes.NewReader(attachment[0]))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/octet-stream")
		request.Header.Set("Content-Disposition", `attachment; filename="receipt.bin"`)
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		response, err := n.server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
			t.Fatal("attachment upload", response.StatusCode)
		}
	}
	n.request(t, "POST", n.base()+"/"+draft.ID+"/send", schema.SendMailInput{}, &draft, 200)
	n.server.Clock.Advance(11 * time.Second)
	return draft
}
func (n *mailNode) deliver(t *testing.T, item schema.MailItem) {
	t.Helper()
	if err := mailbox.Deliver(n.server.Context(), smipmail.Job(item.ID)); err != nil {
		t.Fatal(err)
	}
}
func (n *mailNode) detail(t *testing.T, item schema.MailItem) schema.MailDetail {
	t.Helper()
	var out schema.MailDetail
	n.request(t, "GET", n.base()+"/"+item.ID, nil, &out, 200)
	return out
}
func (n *mailNode) deliveries(t *testing.T, item schema.MailItem) (q.MailTransmission, []q.MailDelivery) {
	t.Helper()
	queries := q.New(db.From(n.server.Context()))
	row, err := queries.GetMailTransmission(n.server.Context(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := queries.ListMailDeliveries(n.server.Context(), row.ID)
	if err != nil {
		t.Fatal(err)
	}
	return row, rows
}
func mailTLS(t *testing.T, n *mailNode, wrap func(http.Handler) http.Handler) *httptest.Server {
	t.Helper()
	handler := n.server.Config.Handler
	if wrap != nil {
		handler = wrap(handler)
	}
	s := httptest.NewUnstartedServer(handler)
	s.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	s.Config.ReadHeaderTimeout = 5 * time.Second
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}
func mailTrust(t *testing.T, n *mailNode, origins map[string]string, certificate *httptest.Server) {
	t.Helper()
	cfg := n.config
	cfg.Origins = origins
	cfg.CAFile = filepath.Join(t.TempDir(), "fixture-ca.pem")
	if err := os.WriteFile(cfg.CAFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	gateway, err := smipmail.NewGateway(cfg, mailbox.ReceiveSMIP)
	if err != nil {
		t.Fatal(err)
	}
	lidza.Provide(n.server.Services, gateway)
	n.config = cfg
}

func TestSmipMailTwoIsolatedServersLostReceiptAndBidirectionalDelivery(t *testing.T) {
	a, b := newMailNode(t, "mail-alpha.example"), newMailNode(t, "mail-beta.example")
	var attempts atomic.Int32
	var wiresMu sync.Mutex
	var wires [][]byte
	receiver := mailTLS(t, b, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != smip.MailMessagePath {
				next.ServeHTTP(w, r)
				return
			}
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
			wiresMu.Lock()
			wires = append(wires, raw)
			wiresMu.Unlock()
			if attempts.Add(1) == 1 {
				capture := httptest.NewRecorder()
				next.ServeHTTP(capture, r)
				if capture.Code != 201 {
					t.Errorf("remote admission %d: %s", capture.Code, capture.Body.String())
				}
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				connection.Close()
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	sender := mailTLS(t, a, nil)
	mailTrust(t, a, map[string]string{b.config.Domain: receiver.URL}, receiver)
	mailTrust(t, b, map[string]string{a.config.Domain: sender.URL}, sender)
	binary := bytes.Repeat([]byte{0, 255, 7, 19}, 1024)
	draft := a.queue(t, b.box.Address, binary)
	a.deliver(t, draft)
	first := a.detail(t, draft)
	if first.Item.Status != "delivery_pending" || len(first.Deliveries) != 1 || first.Deliveries[0].Transport != "smip" || first.Deliveries[0].State != "uncertain" {
		t.Fatalf("lost receipt not held: %+v", first)
	}
	var inbox schema.MailItemList
	b.request(t, "GET", b.base()+"?folder=inbox", nil, &inbox, 200)
	if len(inbox.Items) != 1 || inbox.Items[0].TextBody != "Hello from "+a.config.Domain || inbox.Items[0].HTMLBody != "<p>Hello from <b>"+a.config.Domain+"</b></p>" {
		t.Fatalf("remote inbox lost content: %+v", inbox)
	}
	original := inbox.Items[0]
	remote := b.detail(t, original)
	if len(remote.Attachments) != 1 || remote.Attachments[0].Name != "receipt.bin" {
		t.Fatal("attachment missing")
	}
	var downloaded schema.FileContent
	b.request(t, "GET", b.base()+"/"+original.ID+"/attachments/"+remote.Attachments[0].ID, nil, &downloaded, 200)
	decoded, err := base64.StdEncoding.DecodeString(downloaded.Data)
	if err != nil || !bytes.Equal(decoded, binary) {
		t.Fatal("attachment bytes changed", err)
	}
	a.deliver(t, draft)
	if attempts.Load() != 1 {
		t.Fatal("retry lease ignored")
	}
	// A process restart loses no signed intent or sticky discovery state.
	mailTrust(t, a, map[string]string{b.config.Domain: receiver.URL}, receiver)
	a.server.Clock.Advance(61 * time.Second)
	b.server.Clock.Advance(61 * time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); a.deliver(t, draft) }()
	}
	wg.Wait()
	final := a.detail(t, draft)
	if final.Item.Status != "sent" || final.Deliveries[0].State != "accepted" || final.Deliveries[0].Attempts != 2 || attempts.Load() != 2 {
		t.Fatalf("receipt recovery failed: %+v", final)
	}
	wiresMu.Lock()
	if len(wires) != 2 || !bytes.Equal(wires[0], wires[1]) {
		t.Error("retry changed signed bytes")
	}
	wiresMu.Unlock()
	b.request(t, "GET", b.base()+"?folder=inbox", nil, &inbox, 200)
	if len(inbox.Items) != 1 || inbox.Items[0].ID != original.ID {
		t.Fatal("duplicate remote email")
	}
	_, rows := a.deliveries(t, draft)
	var receipt smip.Receipt
	if json.Unmarshal([]byte(rows[0].Receipt), &receipt) != nil || receipt.Version != smip.MailVersion {
		t.Fatal("missing durable signed receipt")
	}
	reverse := b.queue(t, a.box.Address)
	b.deliver(t, reverse)
	if got := b.detail(t, reverse); got.Item.Status != "sent" || got.Deliveries[0].Transport != "smip" {
		t.Fatalf("reverse delivery failed: %+v", got)
	}
	a.request(t, "GET", a.base()+"?folder=inbox", nil, &inbox, 200)
	if len(inbox.Items) != 1 || inbox.Items[0].FromAddress != b.box.Address {
		t.Fatal("reverse inbox missing")
	}
	t.Logf("Two separate databases, TLS 1.3 HTTP listeners: %s <-> %s; lost receipt recovered; one inbox copy per send", a.config.Domain, b.config.Domain)
}

func markAbsent(t *testing.T, n *mailNode, domain string) {
	t.Helper()
	ctx := n.server.Context()
	now := lidza.Now(ctx)
	if err := q.New(db.From(ctx)).WriteSmipMailDomain(ctx, q.WriteSmipMailDomainParams{Origin: n.config.Domain, Domain: domain, State: "absent", Reason: "no_smip_record", CheckedAt: now, ExpiresAt: now.Add(5 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
}
func TestSmipMailProviderSingleAttemptAndNoSecureDowngrade(t *testing.T) {
	a, b := newMailNode(t, "api-alpha.example"), newMailNode(t, "api-beta.example")
	var calls atomic.Int32
	var fail atomic.Bool
	var recipientsMu sync.Mutex
	var recipients []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		recipientsMu.Lock()
		recipients = append(recipients, r.Form["to"]...)
		recipientsMu.Unlock()
		if fail.Load() {
			http.Error(w, "outcome unknown", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"fixture-api-id","message":"Queued"}`)
	}))
	defer api.Close()
	t.Setenv("MAIL_PROVIDER", "mailgun")
	t.Setenv("MAIL_DOMAIN", a.config.Domain)
	t.Setenv("MAIL_API_KEY", "PUBLIC-fixture-key")
	t.Setenv("MAIL_BASE_URL", api.URL)
	var unavailable atomic.Bool
	target := mailTLS(t, b, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if unavailable.Load() {
				http.Error(w, "outage", http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	source := mailTLS(t, a, nil)
	mailTrust(t, a, map[string]string{b.config.Domain: target.URL}, target)
	mailTrust(t, b, map[string]string{a.config.Domain: source.URL}, source)
	markAbsent(t, a, "external.example")
	mixed := a.queue(t, b.box.Address+",person@external.example")
	a.deliver(t, mixed)
	if got := a.detail(t, mixed); got.Item.Status != "sent" || len(got.Deliveries) != 2 {
		t.Fatalf("mixed routes failed %+v", got)
	}
	recipientsMu.Lock()
	if len(recipients) != 1 || recipients[0] != "person@external.example" {
		t.Error("API received secure recipient", recipients)
	}
	recipientsMu.Unlock()
	a.deliver(t, mixed)
	if calls.Load() != 1 {
		t.Fatal("accepted API submission repeated")
	}
	fail.Store(true)
	failed := a.queue(t, "person@external.example")
	a.deliver(t, failed)
	a.server.Clock.Advance(61 * time.Second)
	a.deliver(t, failed)
	if got := a.detail(t, failed); got.Item.Status != "delivery_uncertain" || got.Deliveries[0].Attempts != 1 || calls.Load() != 2 {
		t.Fatalf("uncertain API retried: %+v calls=%d", got, calls.Load())
	}
	unavailable.Store(true)
	if _, err := smipmail.From(a.server.Context()).Resolve(a.server.Context(), b.config.Domain, true); err == nil {
		t.Fatal("outage verified")
	}
	held := a.queue(t, b.box.Address)
	a.deliver(t, held)
	if got := a.detail(t, held); got.Item.Status != "delivery_pending" || got.Deliveries[0].Transport != "pending" || calls.Load() != 2 {
		t.Fatalf("known domain downgraded %+v", got)
	}
	registry, err := q.New(db.From(a.server.Context())).GetSmipMailDomain(a.server.Context(), q.GetSmipMailDomainParams{Origin: a.config.Domain, Domain: b.config.Domain})
	if err != nil || !registry.EverVerified || registry.State != "error" {
		t.Fatal("sticky verification lost", err)
	}
}

func TestSmipMailRegisteredWorkersDeliverWithoutManualDispatch(t *testing.T) {
	a, b := newMailNode(t, "worker-mail-alpha.example", true), newMailNode(t, "worker-mail-beta.example", true)
	target, source := mailTLS(t, b, nil), mailTLS(t, a, nil)
	mailTrust(t, a, map[string]string{b.config.Domain: target.URL}, target)
	mailTrust(t, b, map[string]string{a.config.Domain: source.URL}, source)
	draft := a.queue(t, b.box.Address)
	deadline := time.After(20 * time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		row, err := q.New(db.From(a.server.Context())).GetMailItem(a.server.Context(), draft.ID)
		if err != nil {
			t.Fatal(err)
		}
		if row.Status == "sent" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("registered worker did not deliver, status=%s", row.Status)
		case <-tick.C:
		}
	}
	var inbox schema.MailItemList
	b.request(t, "GET", b.base()+"?folder=inbox", nil, &inbox, 200)
	if len(inbox.Items) != 1 || inbox.Items[0].TextBody != "Hello from "+a.config.Domain {
		t.Fatal("worker inbox wrong")
	}
	if got := a.detail(t, draft); len(got.Deliveries) != 1 || got.Deliveries[0].State != "accepted" {
		t.Fatal("worker receipt missing")
	}
}

func TestSmipMailRevokedSenderAndDisabledTransportNeverFallback(t *testing.T) {
	a, b := newMailNode(t, "revoke-mail-alpha.example"), newMailNode(t, "revoke-mail-beta.example")
	target, source := mailTLS(t, b, nil), mailTLS(t, a, nil)
	mailTrust(t, a, map[string]string{b.config.Domain: target.URL}, target)
	mailTrust(t, b, map[string]string{a.config.Domain: source.URL}, source)
	draft := a.queue(t, b.box.Address)
	ctx := a.server.Context()
	pool := db.From(ctx)
	if _, err := pool.Exec(ctx, "DELETE FROM auth_member WHERE scope=$1 AND subject=$2", a.workspace.ID, a.owner.Subject); err != nil {
		t.Fatal(err)
	}
	if err := mailbox.Deliver(ctx, smipmail.Job(draft.ID)); err == nil {
		t.Fatal("revoked author accepted")
	}
	// Preparation refuses revoked authorization before any delivery intent exists.
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM mail_transmission WHERE item_id=$1", draft.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("revoked sender prepared delivery", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO auth_member(subject,scope,role) VALUES($1,$2,'owner')", a.owner.Subject, a.workspace.ID); err != nil {
		t.Fatal(err)
	}
	a.deliver(t, draft)
	row, rows := a.deliveries(t, draft)
	if row.State != "accepted" || rows[0].Route != "smip" {
		t.Fatal("restored sender failed")
	}
	// Prepare an existing secure intent while its endpoint is unavailable.
	target.Close()
	held := a.queue(t, b.box.Address)
	a.deliver(t, held)
	if got := a.detail(t, held); got.Item.Status != "delivery_pending" || got.Deliveries[0].Transport != "smip" {
		t.Fatal("outage did not retain secure route")
	}
	// Existing signed intents can never enter legacy sending when SMIP is disabled.
	t.Setenv("SMIP_MAIL_ENABLED", "false")
	if err := smipmail.Configure(a.server.Services, mailbox.ReceiveSMIP); err != nil {
		t.Fatal(err)
	}
	a.deliver(t, held)
	if got := a.detail(t, held); got.Item.Status != "delivery_pending" || got.Deliveries[0].Transport != "smip" {
		t.Fatal("disabled gateway fell back")
	}
	a.deliver(t, draft)
	if got := a.detail(t, draft); got.Item.Status != "sent" || got.Deliveries[0].Attempts != 1 {
		t.Fatal("disabled gateway changed accepted message")
	}
	var inbox schema.MailItemList
	b.request(t, "GET", b.base()+"?folder=inbox", nil, &inbox, 200)
	if len(inbox.Items) != 1 {
		t.Fatal("disablement duplicated delivery")
	}
}

func TestSmipMailReceiverCommitFailureRollsBackAndRecovers(t *testing.T) {
	a, b := newMailNode(t, "atomic-mail-alpha.example"), newMailNode(t, "atomic-mail-beta.example")
	target, source := mailTLS(t, b, nil), mailTLS(t, a, nil)
	mailTrust(t, a, map[string]string{b.config.Domain: target.URL}, target)
	mailTrust(t, b, map[string]string{a.config.Domain: source.URL}, source)
	ctx := b.server.Context()
	pool := db.From(ctx)
	if _, err := pool.Exec(ctx, "ALTER TABLE smip_mail_receipt ADD CONSTRAINT fixture_receipt_failure CHECK (domain <> 'atomic-mail-beta.example')"); err != nil {
		t.Fatal(err)
	}
	draft := a.queue(t, b.box.Address)
	a.deliver(t, draft)
	for _, table := range []string{"mail_item", "smip_mail_receipt"} {
		var count int
		if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatal("failed receipt committed email", table, count, err)
		}
	}
	if got := a.detail(t, draft); got.Item.Status != "delivery_pending" || got.Deliveries[0].Transport != "smip" || got.Deliveries[0].Attempts != 1 {
		t.Fatal("failed receipt not held")
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE smip_mail_receipt DROP CONSTRAINT fixture_receipt_failure"); err != nil {
		t.Fatal(err)
	}
	a.server.Clock.Advance(61 * time.Second)
	b.server.Clock.Advance(61 * time.Second)
	a.deliver(t, draft)
	if got := a.detail(t, draft); got.Item.Status != "sent" || got.Deliveries[0].Attempts != 2 {
		t.Fatal("transaction failure did not recover")
	}
	// A forwarded header cannot turn the ordinary HTTP listener into a secure peer.
	request, err := http.NewRequest("POST", b.server.URL+smip.MailMessagePath, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-Proto", "https")
	response, err := b.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatal("forwarded TLS accepted", response.StatusCode)
	}
	// Reuse the immutable wire to prove tampering and changed recipient identity fail.
	row, deliveries := a.deliveries(t, draft)
	var snapshot struct {
		Payload smipmail.Payload `json:"payload"`
	}
	if err := json.Unmarshal([]byte(row.Payload), &snapshot); err != nil {
		t.Fatal(err)
	}
	var packet smip.Packet
	if err := json.Unmarshal([]byte(deliveries[0].Packet), &packet); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(snapshot.Payload)
	if err != nil {
		t.Fatal(err)
	}
	packet.Envelope.Payload = smip.Encode(payload)
	post := func(p smip.Packet, want int) {
		t.Helper()
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		response, err := target.Client().Post(target.URL+smip.MailMessagePath, "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != want {
			body, _ := io.ReadAll(response.Body)
			t.Fatalf("malformed admission %d want %d: %s", response.StatusCode, want, body)
		}
	}
	packet.Signature = strings.Repeat("A", 86)
	post(packet, http.StatusUnauthorized)
	_, private, _ := smipKeys(a.config.Domain, time.Now().Unix())
	packet.Envelope.ID = uuid.NewString()
	packet.Envelope.Recipient = "unknown@" + b.config.Domain
	packet, err = smip.Sign(packet.Envelope, private)
	if err != nil {
		t.Fatal(err)
	}
	post(packet, http.StatusForbidden)
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM mail_item").Scan(&count); err != nil || count != 1 {
		t.Fatal("invalid packet added email", err)
	}
}

func TestSmipMailBlindOnlyProviderRecipientsStayPrivateAndNeverRepeat(t *testing.T) {
	a, b := newMailNode(t, "blind-mail-alpha.example"), newMailNode(t, "blind-mail-beta.example")
	var mu sync.Mutex
	var sent []url.Values
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		mu.Lock()
		sent = append(sent, r.Form)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"blind-fixture-id","message":"Queued"}`)
	}))
	defer api.Close()
	t.Setenv("MAIL_PROVIDER", "mailgun")
	t.Setenv("MAIL_DOMAIN", a.config.Domain)
	t.Setenv("MAIL_API_KEY", "PUBLIC-fixture-key")
	t.Setenv("MAIL_BASE_URL", api.URL)
	target, source := mailTLS(t, b, nil), mailTLS(t, a, nil)
	mailTrust(t, a, map[string]string{b.config.Domain: target.URL}, target)
	mailTrust(t, b, map[string]string{a.config.Domain: source.URL}, source)
	markAbsent(t, a, "external.example")
	subject, text, bcc := "Blind delivery", "No blind-address disclosure", "one@external.example,two@external.example"
	var draft schema.MailItem
	a.request(t, "POST", a.base(), schema.DraftInput{To: &b.box.Address, Bcc: &bcc, Subject: &subject, Text: &text}, &draft, 201)
	a.request(t, "POST", a.base()+"/"+draft.ID+"/send", schema.SendMailInput{}, &draft, 200)
	a.server.Clock.Advance(11 * time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); a.deliver(t, draft) }()
	}
	wg.Wait()
	a.deliver(t, draft)
	if got := a.detail(t, draft); got.Item.Status != "sent" || len(got.Deliveries) != 3 {
		t.Fatalf("blind routing failed %+v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 2 {
		t.Fatal("wrong number of API submissions", len(sent))
	}
	seen := map[string]bool{}
	for _, form := range sent {
		to := form["to"]
		if len(to) != 1 || seen[to[0]] || len(form["cc"])+len(form["bcc"]) != 0 {
			t.Fatal("blind recipients exposed or repeated", form)
		}
		seen[to[0]] = true
	}
	if !seen["one@external.example"] || !seen["two@external.example"] {
		t.Fatal("blind recipient lost")
	}
	var inbox schema.MailItemList
	b.request(t, "GET", b.base()+"?folder=inbox", nil, &inbox, 200)
	if len(inbox.Items) != 1 {
		t.Fatal("secure recipient duplicated")
	}
	detail := b.detail(t, inbox.Items[0])
	raw, err := json.Marshal(detail)
	if err != nil || bytes.Contains(raw, []byte("external.example")) {
		t.Fatal("blind addresses leaked to secure recipient", err)
	}
}

func TestSmipMailProviderAttemptFencedByNewSecurePolicy(t *testing.T) {
	a := newMailNode(t, "policy-mail.example")
	var calls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"forbidden-submit"}`)
	}))
	defer api.Close()
	t.Setenv("MAIL_PROVIDER", "mailgun")
	t.Setenv("MAIL_DOMAIN", a.config.Domain)
	t.Setenv("MAIL_API_KEY", "PUBLIC-fixture-key")
	t.Setenv("MAIL_BASE_URL", api.URL)
	source := mailTLS(t, a, nil)
	mailTrust(t, a, map[string]string{}, source)
	markAbsent(t, a, "external.example")
	draft := a.queue(t, "recipient@external.example")
	ctx := a.server.Context()
	pool := db.From(ctx)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	box, err := queries.GetMailbox(ctx, a.box.ID)
	if err != nil {
		t.Fatal(err)
	}
	item, err := queries.LockMailItem(ctx, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := mailbox.Config(box)
	if err != nil {
		t.Fatal(err)
	}
	transmission, err := smipmail.Prepare(ctx, tx, box, item, mail.Message{From: box.Address, To: item.ToAddress, Subject: item.Subject, Text: item.TextBody}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a planned provider route, then another node learning secure support
	// before this worker's durable attempt marker. The route may not be rewritten.
	if _, err = tx.Exec(ctx, "UPDATE mail_delivery SET route='provider' WHERE transmission_id=$1", transmission.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE mail_transmission SET state='pending' WHERE id=$1", transmission.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	now := lidza.Now(ctx)
	if err = q.New(pool).WriteSmipMailDomain(ctx, q.WriteSmipMailDomainParams{Origin: a.config.Domain, Domain: "external.example", State: "error", EverVerified: true, Reason: "fixture verified then unavailable", CheckedAt: now, ExpiresAt: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	a.deliver(t, draft)
	got := a.detail(t, draft)
	if calls.Load() != 0 || got.Item.Status != "delivery_blocked" || got.Deliveries[0].Transport != "provider" || got.Deliveries[0].Attempts != 0 || got.Deliveries[0].Reason != "secure_policy_changed_before_provider_attempt" {
		t.Fatalf("new secure requirement allowed provider submission: %+v", got)
	}
	t.Setenv("ADMIN_USERS", a.owner.Subject)
	response, err := a.server.Client().Get(a.server.URL + "/admin/smip-mail")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || !bytes.Contains(raw, []byte("external.example")) || bytes.Contains(raw, []byte(a.config.Seed)) {
		t.Fatal("registry page missing or leaked identity", response.StatusCode, err)
	}
}

// fillRegistry inserts count never-verified failure rows, as an unauthenticated
// sender naming random domains leaves behind.
func fillRegistry(t *testing.T, n *mailNode, prefix string, expires time.Time) {
	t.Helper()
	ctx := n.server.Context()
	if _, err := db.From(ctx).Exec(ctx, "INSERT INTO smip_mail_domain(origin,domain,state,ever_verified,endpoint,document,reason,checked_at,expires_at) SELECT $1,$2||i||'.example','error',false,'','','verification_failed',$3,$4 FROM generate_series(1,$5::int) i", n.config.Domain, prefix, lidza.Now(ctx).Add(-time.Minute), expires, smipmail.RegistryCapacity); err != nil {
		t.Fatal(err)
	}
}
func TestSmipMailRegistryCapacityEvictsExpiredFailuresAndHoldsWithoutDowngrade(t *testing.T) {
	a, b := newMailNode(t, "cap-alpha.example"), newMailNode(t, "cap-beta.example")
	target := mailTLS(t, b, nil)
	source := mailTLS(t, a, nil)
	mailTrust(t, a, map[string]string{b.config.Domain: target.URL}, target)
	mailTrust(t, b, map[string]string{a.config.Domain: source.URL}, source)
	ctx := a.server.Context()
	queries := q.New(db.From(ctx))
	now := lidza.Now(ctx)
	fillRegistry(t, a, "attacker-", now.Add(-45*time.Second))
	count, err := queries.CountSmipMailDomains(ctx, q.CountSmipMailDomainsParams{Origin: a.config.Domain, ExpiresAt: now})
	if err != nil || count.Total != smipmail.RegistryCapacity || count.Live != 0 {
		t.Fatal("expired failures counted as capacity", err, count)
	}
	if _, err = smipmail.From(ctx).Resolve(ctx, b.config.Domain, false); err != nil {
		t.Fatal("expired failures locked a new peer out", err)
	}
	count, err = queries.CountSmipMailDomains(ctx, q.CountSmipMailDomainsParams{Origin: a.config.Domain, ExpiresAt: now})
	if err != nil || count.Total >= smipmail.RegistryCapacity || count.Live != 1 {
		t.Fatal("expired failures not evicted", err, count)
	}
	// Unexpired failures are genuine capacity: a new domain holds under its
	// own reason, with a long retry and never the provider.
	fillRegistry(t, a, "busy-", now.Add(time.Hour))
	if _, err = smipmail.From(ctx).Resolve(ctx, "fresh.example", false); !errors.Is(err, smipmail.ErrRegistryFull) {
		t.Fatal("full registry resolved a new domain", err)
	}
	held := a.queue(t, "person@fresh.example")
	a.deliver(t, held)
	_, rows := a.deliveries(t, held)
	if got := a.detail(t, held); got.Item.Status != "delivery_pending" || len(rows) != 1 || rows[0].Route != "pending" || rows[0].Reason != "registry_capacity" || rows[0].NextAttempt.Before(lidza.Now(ctx).Add(4*time.Minute)) {
		t.Fatalf("full registry hold: %+v %+v", got.Item, rows)
	}
	delivered := a.queue(t, b.box.Address)
	a.deliver(t, delivered)
	if got := a.detail(t, delivered); got.Item.Status != "sent" {
		t.Fatalf("verified peer blocked by a full registry: %+v", got)
	}
}
func TestSmipMailStalledBodiesNeverHoldAdmission(t *testing.T) {
	a, b := newMailNode(t, "stall-alpha.example"), newMailNode(t, "stall-beta.example")
	target := mailTLS(t, b, nil)
	source := mailTLS(t, a, nil)
	mailTrust(t, a, map[string]string{b.config.Domain: target.URL}, target)
	mailTrust(t, b, map[string]string{a.config.Domain: source.URL}, source)
	roots := x509.NewCertPool()
	roots.AddCert(target.Certificate())
	// Three connections send complete headers and one body byte, then idle.
	for i := 0; i < 3; i++ {
		conn, err := tls.Dial("tcp", target.Listener.Addr().String(), &tls.Config{RootCAs: roots, ServerName: "example.com"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		if _, err = conn.Write([]byte("POST " + smip.MailMessagePath + " HTTP/1.1\r\nHost: 127.0.0.1\r\nX-Thura-SMIP-Domain: " + b.config.Domain + "\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{")); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(50 * time.Millisecond)
	draft := a.queue(t, b.box.Address)
	a.deliver(t, draft)
	if got := a.detail(t, draft); got.Item.Status != "sent" || len(got.Deliveries) != 1 || got.Deliveries[0].Transport != "smip" {
		t.Fatalf("stalled bodies blocked delivery: %+v", got)
	}
}
