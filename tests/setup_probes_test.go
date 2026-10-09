package tests

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"github.com/google/uuid"
	"thura/internal/setup"
)

// A loopback-only synthetic relay: no delivery to any real mailbox.
func setupSMTP(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	count := &atomic.Int32{}
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(10 * time.Second))
				rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
				reply := func(s string) { fmt.Fprintf(rw, "%s\r\n", s); rw.Flush() }
				reply("220 fixture SMTP")
				for {
					line, err := rw.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
						reply("250 fixture")
					case strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"), strings.HasPrefix(line, "RSET"):
						reply("250 OK")
					case strings.HasPrefix(line, "DATA"):
						reply("354 continue")
						if _, err := textproto.NewReader(rw.Reader).ReadDotBytes(); err != nil {
							return
						}
						count.Add(1)
						reply("250 accepted fixture")
					case strings.HasPrefix(line, "QUIT"):
						reply("221 bye")
						return
					default:
						reply("500 unsupported fixture command")
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { listener.Close(); workers.Wait() })
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port), count
}
func runSetupChecks(t *testing.T, s *lidzatest.Server) {
	t.Helper()
	revision, _, _ := setupRow(t, s)
	for _, kind := range []string{"mail", "storage"} {
		confirm := "check"
		if kind == "mail" {
			confirm = "send"
		}
		r := setupForm(t, s, "check-"+kind, url.Values{"revision": {strconv.Itoa(revision)}, "check_id": {uuid.NewString()}, "confirm": {confirm}})
		if strings.Contains(r.Header.Get("Location"), "error=") {
			t.Fatal("provider check: " + r.Header.Get("Location"))
		}
	}
	var count int
	if err := db.From(s.Context()).QueryRow(s.Context(), "SELECT count(*) FROM setup_probe WHERE state='succeeded'").Scan(&count); err != nil || count < 2 {
		t.Fatalf("provider checks did not succeed: %d %v", count, err)
	}
}
func TestSetupProviderChecksBindSettingsAndDoNotRepeat(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	port, count := setupSMTP(t)
	saveStep(t, s, "mail", map[string]string{"MAIL_FROM": "team@setup.example", "MAIL_SMTP_HOST": "127.0.0.1", "MAIL_SMTP_PORT": port, "MAIL_SMTP_SECURITY": "none", "MAIL_INBOUND_SECRET": "PUBLIC-inbound-test-secret-with-32-characters"})
	revision, _, _ := setupRow(t, s)
	if r := setupForm(t, s, "publish", url.Values{"revision": {strconv.Itoa(revision)}, "confirm": {"publish"}}); !strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("changed mail settings published without a matching check")
	}
	id := uuid.NewString()
	form := url.Values{"revision": {strconv.Itoa(revision)}, "check_id": {id}, "confirm": {"send"}}
	var wait sync.WaitGroup
	errorsFound := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := setup.Probe(probeContext(s), "mail", strconv.Itoa(revision), id)
			errorsFound <- err
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count.Load() != 1 {
		t.Fatal("concurrent replay sent another email")
	}

	for range 2 {
		if r := setupForm(t, s, "check-mail", form); strings.Contains(r.Header.Get("Location"), "error=") {
			t.Fatal("mail check: " + r.Header.Get("Location"))
		}
	}
	if count.Load() != 1 {
		t.Fatalf("replayed form sent %d emails", count.Load())
	}

	// Idempotency is durable even when completion was lost after acceptance.
	if _, err := db.From(s.Context()).Exec(s.Context(), "UPDATE setup_probe SET state='started' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	setupForm(t, s, "check-mail", form)
	if count.Load() != 1 {
		t.Fatal("uncertain attempt was resent")
	}
	// Other setup settings do not invalidate provider checks.
	if _, err := db.From(s.Context()).Exec(s.Context(), "UPDATE setup_probe SET state='succeeded' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	saveStep(t, s, "policy", map[string]string{"THURA_MAX_MEMBERS": "9"})
	revision, _, _ = setupRow(t, s)
	if r := setupForm(t, s, "publish", url.Values{"revision": {strconv.Itoa(revision)}, "confirm": {"publish"}}); strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("unrelated edit invalidated provider checks")
	}
	// Missing explicit side-effect confirmation is rejected.
	id = uuid.NewString()
	setupForm(t, s, "check-mail", url.Values{"revision": {strconv.Itoa(revision)}, "check_id": {id}})
	if count.Load() != 1 {
		t.Fatal("email sent without confirmation")
	}
	// A copied ID cannot be used for a different operation.
	setupForm(t, s, "check-storage", url.Values{"revision": {strconv.Itoa(revision)}, "check_id": {form.Get("check_id")}, "confirm": {"check"}})
	if count.Load() != 1 {
		t.Fatal("cross-kind request repeated send")
	}
	// Administrator CSRF protection also applies while the app is setup-locked.
	req, _ := http.NewRequest("POST", s.URL+"/admin/setup/check-mail", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site provider check: %d", res.StatusCode)
	}
}

func TestSetupStorageCheckCleansOnlyItsOwnObjectAndAuditFailurePreventsSend(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	saveStep(t, s, "storage", map[string]string{"STORAGE_DIR": dir})
	revision, _, _ := setupRow(t, s)
	id := uuid.NewString()
	r := setupForm(t, s, "check-storage", url.Values{"revision": {strconv.Itoa(revision)}, "check_id": {id}, "confirm": {"check"}})
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal(r.Header.Get("Location"))
	}
	var state string
	if err := db.From(s.Context()).QueryRow(s.Context(), "SELECT state FROM setup_probe WHERE id=$1", id).Scan(&state); err != nil || state != "succeeded" {
		t.Fatalf("storage state %s %v", state, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "_thura_setup_checks", id)); !os.IsNotExist(err) {
		t.Fatal("probe object not cleaned up")
	}
	if body, err := os.ReadFile(filepath.Join(dir, "keep.txt")); err != nil || string(body) != "keep" {
		t.Fatal("storage check touched existing content")
	}
	port, count := setupSMTP(t)
	saveStep(t, s, "mail", map[string]string{"MAIL_FROM": "team@setup.example", "MAIL_SMTP_HOST": "127.0.0.1", "MAIL_SMTP_PORT": port, "MAIL_SMTP_SECURITY": "none", "MAIL_INBOUND_SECRET": "PUBLIC-inbound-test-secret-with-32-characters"})
	revision, _, _ = setupRow(t, s)
	if _, err := db.From(s.Context()).Exec(s.Context(), "ALTER TABLE audit_event ADD CONSTRAINT probe_audit_fixture CHECK(action<>'server.setup.check.start') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	r = setupForm(t, s, "check-mail", url.Values{"revision": {strconv.Itoa(revision)}, "check_id": {uuid.NewString()}, "confirm": {"send"}})
	if !strings.Contains(r.Header.Get("Location"), "error=") || count.Load() != 0 {
		t.Fatal("failed intent audit still sent email")
	}
}

// Fixed vendor origins are rewritten only by this synthetic test transport.
type setupRoundTrip func(*http.Request) (*http.Response, error)

func (f setupRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func probeContext(s *lidzatest.Server) context.Context {
	return auth.WithUser(s.Context(), &auth.User{ID: setupSubject(s)})
}
func setupSubject(s *lidzatest.Server) string {
	var subject string
	db.From(s.Context()).QueryRow(s.Context(), "SELECT subject FROM server_setup").Scan(&subject)
	return subject
}
func TestSetupMailAPIUsesOfficialProviderAndRedactsProviderErrors(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	var calls atomic.Int32
	vendor := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v3/mail/send" || r.Header.Get("Authorization") != "Bearer PUBLIC-synthetic-sendgrid-key" {
			t.Error("official provider request missing authorization/path")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "owner@setup.example") {
			t.Error("check sent outside administrator inbox")
		}
		w.WriteHeader(401)
		fmt.Fprint(w, "PUBLIC-synthetic-sendgrid-key should never be rendered")
	}))
	defer vendor.Close()
	endpoint, _ := url.Parse(vendor.URL)
	services := lidza.ServicesFrom(s.Context())
	lidza.Provide[http.RoundTripper](services, setupRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.sendgrid.com" {
			return nil, fmt.Errorf("unexpected vendor origin")
		}
		clone := r.Clone(r.Context())
		copyURL := *r.URL
		copyURL.Scheme = endpoint.Scheme
		copyURL.Host = endpoint.Host
		clone.URL = &copyURL
		clone.Host = endpoint.Host
		return vendor.Client().Transport.RoundTrip(clone)
	}))
	saveStep(t, s, "mail", map[string]string{"MAIL_PROVIDER": "sendgrid", "MAIL_FROM": "team@setup.example", "MAIL_API_KEY": "PUBLIC-synthetic-sendgrid-key", "MAIL_INBOUND_SECRET": "PUBLIC-inbound-test-secret-with-32-characters"})
	revision, _, _ := setupRow(t, s)
	message, err := setup.Probe(probeContext(s), "mail", strconv.Itoa(revision), uuid.NewString())
	if err != nil || calls.Load() != 1 || !strings.Contains(message, "failed") || strings.Contains(message, "synthetic") {
		t.Fatalf("failed provider response not generic: %q %v calls=%d", message, err, calls.Load())
	}
	page, err := s.Client().Get(s.URL + "/admin/setup?step=review")
	if err != nil {
		t.Fatal(err)
	}
	defer page.Body.Close()
	body, _ := io.ReadAll(page.Body)
	if strings.Contains(string(body), "PUBLIC-synthetic-sendgrid-key") {
		t.Fatal("secret rendered in review")
	}
}

func TestSetupS3CheckUsesSignedOperationsAndReportsCleanupFailure(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	var mu sync.Mutex
	objects := map[string][]byte{}
	var refuseDelete atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=PUBLIC-s3-access/") || r.Header.Get("X-Amz-Content-Sha256") == "" {
			t.Error("S3 request was not signed")
		}
		mu.Lock()
		defer mu.Unlock()
		key := strings.TrimPrefix(r.URL.Path, "/private/")
		if r.URL.Query().Get("list-type") == "2" {
			fmt.Fprint(w, "<ListBucketResult>")
			for k, b := range objects {
				if strings.HasPrefix(k, r.URL.Query().Get("prefix")) {
					fmt.Fprintf(w, "<Contents><Key>%s</Key><Size>%d</Size></Contents>", k, len(b))
				}
			}
			fmt.Fprint(w, "</ListBucketResult>")
			return
		}
		if !strings.HasPrefix(key, "app/_thura_setup_checks/") {
			t.Error("check accessed an unrelated S3 object")
			w.WriteHeader(403)
			return
		}
		switch r.Method {
		case "PUT":
			body, _ := io.ReadAll(r.Body)
			objects[key] = body
			w.WriteHeader(200)
		case "GET", "HEAD":
			body, ok := objects[key]
			if !ok {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Header().Set("Content-Type", "text/plain")
			if r.Method == "GET" {
				w.Write(body)
			}
		case "DELETE":
			if refuseDelete.Load() {
				w.WriteHeader(403)
				fmt.Fprint(w, "PUBLIC-s3-secret must not appear in errors")
				return
			}
			delete(objects, key)
			w.WriteHeader(204)
		default:
			w.WriteHeader(405)
		}
	}))
	defer server.Close()
	lidza.Provide[http.RoundTripper](lidza.ServicesFrom(s.Context()), server.Client().Transport)
	saveStep(t, s, "storage", map[string]string{"STORAGE_PROVIDER": "s3", "STORAGE_ENDPOINT": server.URL, "STORAGE_BUCKET": "private", "STORAGE_PREFIX": "app", "STORAGE_REGION": "us-east-1", "STORAGE_ACCESS_KEY": "PUBLIC-s3-access", "STORAGE_SECRET_KEY": "PUBLIC-s3-secret"})
	revision, _, _ := setupRow(t, s)
	message, err := setup.Probe(probeContext(s), "storage", strconv.Itoa(revision), uuid.NewString())
	if err != nil || !strings.Contains(message, "passed") {
		t.Fatalf("S3 check: %s %v", message, err)
	}
	mu.Lock()
	remaining := len(objects)
	mu.Unlock()
	if remaining != 0 {
		t.Fatal("S3 check object left behind")
	}
	refuseDelete.Store(true)
	id := uuid.NewString()
	message, err = setup.Probe(probeContext(s), "storage", strconv.Itoa(revision), id)
	if err != nil || !strings.Contains(message, "could not confirm deletion") || strings.Contains(message, "PUBLIC-s3") {
		t.Fatalf("cleanup failure: %s %v", message, err)
	}
	var state string
	if err := db.From(s.Context()).QueryRow(s.Context(), "SELECT state FROM setup_probe WHERE id=$1", id).Scan(&state); err != nil || state != "cleanup_required" {
		t.Fatalf("cleanup state: %s %v", state, err)
	}
	if r := setupForm(t, s, "publish", url.Values{"revision": {strconv.Itoa(revision)}, "confirm": {"publish"}}); !strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("old S3 success hid failed cleanup")
	}
	// A fourth storage intent exceeds the shared fifteen-minute budget.
	if _, err := setup.Probe(probeContext(s), "storage", strconv.Itoa(revision), uuid.NewString()); err == nil {
		t.Fatal("provider check rate limit absent")
	}
}

func TestSetupProviderChecksRejectNonAdministratorAndFreezeStorageLocation(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	revision, _, _ := setupRow(t, s)
	if _, err := setup.Probe(s.Context(), "mail", strconv.Itoa(revision), uuid.NewString()); err == nil {
		t.Fatal("anonymous provider check allowed")
	}
	if err := setup.Publish(probeContext(s), strconv.Itoa(revision)); err != nil {
		t.Fatal(err)
	}
	saveStep(t, s, "storage", map[string]string{"STORAGE_DIR": t.TempDir()})
	revision, _, _ = setupRow(t, s)
	if _, err := setup.Probe(probeContext(s), "storage", strconv.Itoa(revision), uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if err := setup.Publish(probeContext(s), strconv.Itoa(revision)); err == nil || !strings.Contains(err.Error(), "offline migration") {
		t.Fatalf("empty published deployment changed location: %v", err)
	}
}

func TestSetupReviewShowsExplicitProviderChecksAndResumesResults(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	res, err := s.Client().Get(s.URL + "/admin/setup?step=review")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("review page: %d %v", res.StatusCode, err)
	}
	for _, text := range []string{"Send test email", "Check storage", "Type send", "Type check", "No provider checks recorded.", "name=\"check_id\""} {
		// Confirmation, intent IDs and controls must be in the real admin template.
		if !strings.Contains(string(body), text) {
			t.Fatalf("review missing %q", text)
		}
	}
	completeSetup(t, s)
	res, err = s.Client().Get(s.URL + "/admin/setup?step=review")
	if err != nil {
		t.Fatal(err)
	}
	body, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || !strings.Contains(string(body), "provider checks are complete") || !strings.Contains(string(body), "Mail provider accepted") || !strings.Contains(string(body), "Storage write, read") {
		t.Fatalf("saved check results did not resume: %v", err)
	}
	if strings.Contains(string(body), "PUBLIC-inbound-test-secret") {
		t.Fatal("review leaked saved inbound credential")
	}
}

func TestSetupProviderChecksExpireBeforePublication(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	if _, err := db.From(s.Context()).Exec(s.Context(), "UPDATE setup_probe SET created_at=$1", lidza.Now(s.Context()).Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	revision, _, _ := setupRow(t, s)
	if err := setup.Publish(probeContext(s), strconv.Itoa(revision)); err == nil || !strings.Contains(err.Error(), "24 hours") {
		t.Fatalf("expired checks allowed publication: %v", err)
	}
}
