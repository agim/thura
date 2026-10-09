package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/credentials"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/lidzatest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"thura/app"
	"thura/internal/setup"
	"thura/internal/workspace"
	"thura/schema"
)

// Each fixture owns a new database: first-account races never delete or
// impersonate accounts in the ordinary integration or browser database.
func setupServer(t *testing.T) *lidzatest.Server {
	t.Helper()
	if _, e := os.Stat("lidza.json"); e != nil {
		t.Chdir("..")
	}
	t.Setenv("LIDZA_MODE", "test")
	var cfg db.Config
	if e := env.Load(".", &cfg); e != nil {
		t.Fatal(e)
	}
	u, e := url.Parse(cfg.URL)
	if e != nil {
		t.Fatal("test database URL is invalid")
	}
	database := "thura_setup_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	adminURL := *u
	adminURL.Path = "/postgres"
	ctx := context.Background()
	conn, e := pgx.Connect(ctx, adminURL.String())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(ctx, "CREATE DATABASE "+database); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := conn.Exec(ctx, "DROP DATABASE "+database); e != nil {
			t.Error(e)
		}
		conn.Close(ctx)
	})
	u.Path = "/" + database
	t.Setenv("DATABASE_URL", u.String())
	t.Setenv("JOBS_WORKERS", "0")
	// The fixture owns its provider configuration; CI's process overrides must
	// not mask the snapshot it is testing. Restore every inherited value.
	for _, step := range setup.Catalog() {
		for _, field := range step.Fields {
			if value, ok := os.LookupEnv(field.Name); ok {
				t.Setenv(field.Name, value)
				if e := os.Unsetenv(field.Name); e != nil {
					t.Fatal(e)
				}
			}
		}
	}
	t.Setenv("THURA_SETUP_TOKEN", "PUBLIC-test-bootstrap-token-with-32-characters")
	t.Setenv("LIDZA_MASTER_KEY", strings.Repeat("ad", 32))
	old := credentials.Overrides()
	t.Cleanup(func() { credentials.SetOverrides(old) })
	pool, e := pgxpool.New(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Migrate(ctx, pool, os.DirFS("db/migrations")); e != nil {
		t.Fatal(e)
	}
	pool.Close()
	return lidzatest.Start(t, app.New(nil))
}
func claimSetup(t *testing.T, s *lidzatest.Server) schema.SetupClaimInput {
	t.Helper()
	in := schema.SetupClaimInput{Token: "PUBLIC-test-bootstrap-token-with-32-characters", Email: "owner@setup.example", Name: "Server owner", Password: "setup maple private river 87344"}
	if r := s.JSON(t, "POST", "/api/v1/setup/claim", in, nil); r.StatusCode != 200 {
		t.Fatalf("claim: %d", r.StatusCode)
	}
	if r := s.JSON(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: in.Email, Password: in.Password}, nil); r.StatusCode != 200 {
		t.Fatalf("login: %d", r.StatusCode)
	}
	return in
}
func setupForm(t *testing.T, s *lidzatest.Server, action string, v url.Values) *http.Response {
	t.Helper()
	r, e := http.NewRequest("POST", s.URL+"/admin/setup/"+action, strings.NewReader(v.Encode()))
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	client := *s.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, e := client.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	return response
}
func setupRow(t *testing.T, s *lidzatest.Server) (int, string, string) {
	t.Helper()
	var n int
	var draft, published string
	if e := db.From(s.Context()).QueryRow(s.Context(), "SELECT revision,draft,published FROM server_setup").Scan(&n, &draft, &published); e != nil {
		t.Fatal(e)
	}
	return n, draft, published
}
func saveStep(t *testing.T, s *lidzatest.Server, step string, values map[string]string) {
	t.Helper()
	n, _, _ := setupRow(t, s)
	v := url.Values{"revision": {fmt.Sprint(n)}, "step": {step}}
	for _, f := range setup.Catalog() {
		if f.ID == step {
			for _, field := range f.Fields {
				v.Set(field.Name, field.Default)
			}
		}
	}
	for name, value := range values {
		v.Set(name, value)
	}
	r := setupForm(t, s, "save", v)
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("save failed: " + r.Header.Get("Location"))
	}
}
func completeSetup(t *testing.T, s *lidzatest.Server) {
	t.Helper()
	saveStep(t, s, "server", map[string]string{"APP_URL": "https://setup.example", "THURA_OPERATOR_EMAIL": "owner@setup.example", "THURA_BACKUP_PLAN": "Encrypted daily database and objects; separate master-key backup; quarterly restore."})
	saveStep(t, s, "mail", map[string]string{"MAIL_FROM": "team@setup.example", "MAIL_SMTP_HOST": "localhost", "MAIL_SMTP_PORT": "25", "MAIL_SMTP_SECURITY": "none", "MAIL_INBOUND_SECRET": "PUBLIC-inbound-test-secret-with-32-characters"})
	saveStep(t, s, "storage", nil)
	saveStep(t, s, "policy", nil)
	saveStep(t, s, "integrations", nil)
}
func TestSetupFirstAdminTokenAndConcurrentClaim(t *testing.T) {
	s := setupServer(t)
	var status schema.SetupStatus
	if r := s.JSON(t, "GET", "/api/v1/setup/status", nil, &status); r.StatusCode != 200 || !status.Open {
		t.Fatal("fresh server not open for token bootstrap")
	}
	in := schema.SetupClaimInput{Token: "wrong", Email: "owner@setup.example", Name: "Owner", Password: "setup maple private river 87344"}
	if r := s.JSON(t, "POST", "/api/v1/setup/claim", in, nil); r.StatusCode != 403 {
		t.Fatal("invalid token created administrator")
	}
	in.Token = "PUBLIC-test-bootstrap-token-with-32-characters"
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); r := s.JSON(t, "POST", "/api/v1/setup/claim", in, nil); codes <- r.StatusCode }()
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code == 409 {
			conflict++
		} else {
			t.Fatalf("claim race: %d", code)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("bootstrap race did not create exactly one account")
	}
	var count int
	if e := db.From(s.Context()).QueryRow(s.Context(), "SELECT COUNT(*) FROM auth_user").Scan(&count); e != nil || count != 1 {
		t.Fatal("bootstrap account count incorrect")
	}
	if r := s.JSON(t, "GET", "/api/v1/setup/status", nil, &status); r.StatusCode != 200 || status.Open || !status.Claimed {
		t.Fatal("bootstrap did not close")
	}
	if r := s.JSON(t, "POST", "/api/v1/auth/register", auth.Credentials{Email: "attacker@setup.example", Password: in.Password}, nil); r.StatusCode != 404 {
		t.Fatal("public registration opened")
	}
}
func TestSetupEncryptedDraftResumeRevisionAndSecretHandling(t *testing.T) {
	s := setupServer(t)
	in := claimSetup(t, s)
	secret := "PUBLIC-draft-api-key-that-must-never-render"
	saveStep(t, s, "mail", map[string]string{"MAIL_PROVIDER": "mailgun", "MAIL_API_KEY": secret, "MAIL_FROM": "team@setup.example", "MAIL_DOMAIN": "setup.example"})
	n, draft, _ := setupRow(t, s)
	if strings.Contains(draft, secret) || strings.Contains(draft, "mailgun") {
		t.Fatal("draft stored in plaintext")
	}
	response, e := s.Client().Get(s.URL + "/admin/setup?step=mail")
	if e != nil {
		t.Fatal(e)
	}
	body, e := io.ReadAll(response.Body)
	response.Body.Close()
	if e != nil {
		t.Fatal(e)
	}
	if response.StatusCode != 200 || strings.Contains(string(body), secret) || !strings.Contains(string(body), "Secret saved") {
		t.Fatal("resume leaked or lost saved credential")
	}
	r := setupForm(t, s, "save", url.Values{"revision": {fmt.Sprint(n - 1)}, "step": {"mail"}, "MAIL_API_KEY": {"stale-key"}})
	if !strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("stale revision overwrote draft")
	}
	saveStep(t, s, "mail", map[string]string{"MAIL_PROVIDER": "mailgun", "MAIL_FROM": "team@setup.example", "MAIL_DOMAIN": "setup.example"})
	_, draft, _ = setupRow(t, s)
	key, e := credentials.Key(".")
	if e != nil {
		t.Fatal(e)
	}
	plain, e := credentials.Decrypt(key, draft)
	if e != nil || !strings.Contains(string(plain), secret) {
		t.Fatal("blank field cleared saved secret")
	}
	member, e := auth.From(s.Context()).CreateUser(s.Context(), "member@setup.example", "Member", in.Password)
	if e != nil {
		t.Fatal(e)
	}
	if r := s.JSON(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: member.Email, Password: in.Password}, nil); r.StatusCode != 200 {
		t.Fatal("member login")
	}
	response, e = s.Client().Get(s.URL + "/admin/setup")
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("ordinary user gained server administrator access")
	}
	if r := s.JSON(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: in.Email, Password: in.Password}, nil); r.StatusCode != 200 {
		t.Fatal("owner login")
	}
	n, _, _ = setupRow(t, s)
	r = setupForm(t, s, "save", url.Values{"revision": {fmt.Sprint(n)}, "step": {"mail"}, "clear_MAIL_API_KEY": {"true"}})
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("secret clearing failed")
	}
	_, draft, _ = setupRow(t, s)
	plain, e = credentials.Decrypt(key, draft)
	if e != nil || strings.Contains(string(plain), secret) {
		t.Fatal("explicit clear retained secret")
	}
}
func TestSetupPublishAuditRollbackRestartAndPolicyActivation(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	n, _, _ := setupRow(t, s)
	ctx := s.Context()
	pool := db.From(ctx)
	if _, e := pool.Exec(ctx, "ALTER TABLE audit_event ADD CONSTRAINT setup_audit_fixture CHECK(action<>'server.setup.publish')"); e != nil {
		t.Fatal(e)
	}
	r := setupForm(t, s, "publish", url.Values{"revision": {fmt.Sprint(n)}, "confirm": {"publish"}})
	if !strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("failed audit published setup")
	}
	var workspaces int
	if e := pool.QueryRow(ctx, "SELECT COUNT(*) FROM workspace").Scan(&workspaces); e != nil || workspaces != 0 {
		t.Fatal("audit failure left partial workspace")
	}
	_, _, published := setupRow(t, s)
	if published != "" {
		t.Fatal("audit failure left published secrets")
	}
	if _, e := pool.Exec(ctx, "ALTER TABLE audit_event DROP CONSTRAINT setup_audit_fixture"); e != nil {
		t.Fatal(e)
	}
	r = setupForm(t, s, "publish", url.Values{"revision": {fmt.Sprint(n)}, "confirm": {"publish"}})
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("publish failed: " + r.Header.Get("Location"))
	}
	if _, _, p := setupRow(t, s); p == "" || strings.Contains(p, "PUBLIC-inbound") {
		t.Fatal("published configuration is absent or plaintext")
	}
	r = setupForm(t, s, "publish", url.Values{"revision": {fmt.Sprint(n)}, "confirm": {"publish"}})
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("publish retry failed")
	}
	if e := pool.QueryRow(ctx, "SELECT COUNT(*) FROM workspace").Scan(&workspaces); e != nil || workspaces != 1 {
		t.Fatal("publish retry duplicated workspace")
	}
	// A fresh app startup activates the committed snapshot, never the draft.
	restarted := lidzatest.Start(t, app.New(nil))
	if result := restarted.JSON(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: "owner@setup.example", Password: "setup maple private river 87344"}, nil); result.StatusCode != 200 {
		t.Fatal("administrator could not return after restart")
	}
	_, _, published = setupRow(t, s)
	var quota struct {
		Bytes int64 `env:"DRIVE_QUOTA_BYTES"`
	}
	if e := env.Load(".", &quota); e != nil || quota.Bytes != 1<<30 {
		t.Fatal("published quota did not activate")
	}
	if e := setup.Activate(ctx, s.Services); e != nil {
		t.Fatal(e)
	}
	var work schema.WorkspaceList
	if res := s.JSON(t, "GET", "/api/v1/workspaces", nil, &work); res.StatusCode != 200 || len(work.Items) != 1 {
		t.Fatal("initial owner workspace missing")
	}
	var role string
	if err := pool.QueryRow(ctx, "SELECT role FROM auth_member WHERE scope=$1", work.Items[0].ID).Scan(&role); err != nil || role != "owner" {
		t.Fatal("initial account lacks owner role")
	}
	// Pending invitations reserve capacity; the owner's seat cannot be bypassed.
	t.Setenv("THURA_MAX_MEMBERS", "1")
	if res := s.JSON(t, "POST", "/api/v1/workspaces/"+work.Items[0].ID+"/invitations", schema.InviteInput{Email: "guest@setup.example", Role: schema.WorkspaceRoleMember}, nil); res.StatusCode != 409 {
		t.Fatal("member quota not enforced")
	}
	r = setupForm(t, s, "restart", url.Values{"revision": {fmt.Sprint(n)}, "step": {"server"}, "confirm": {"restart"}})
	if strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("restart failed")
	}
	_, draft, after := setupRow(t, s)
	if after != published || draft == published {
		t.Fatal("restart changed active configuration")
	}
	if e := setup.Activate(ctx, s.Services); e != nil {
		t.Fatal(e)
	}
	if e := env.Load(".", &quota); e != nil || quota.Bytes != 1<<30 {
		t.Fatal("restart activated incomplete draft")
	}
	// Verify there is exactly one publication audit despite a lost-response retry.
	var audits int
	if e := pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_event WHERE action='server.setup.publish'").Scan(&audits); e != nil || audits != 1 {
		t.Fatal("publication audit count incorrect")
	}
}

func TestSetupEnforcedSharingAndInvitationRestrictions(t *testing.T) {
	s := setupServer(t)
	in := claimSetup(t, s)
	completeSetup(t, s)
	saveStep(t, s, "policy", map[string]string{"THURA_ALLOW_ANONYMOUS_SHARES": "true"})
	revision, _, _ := setupRow(t, s)
	if r := setupForm(t, s, "publish", url.Values{"revision": {strconv.Itoa(revision)}, "confirm": {"publish"}}); strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("policy fixture setup publication failed")
	}
	if e := setup.Activate(s.Context(), s.Services); e != nil {
		t.Fatal(e)
	}
	w, e := workspace.Create(s.Context(), schema.CreateWorkspaceInput{Name: "Restricted team", OwnerEmail: in.Email})
	if e != nil {
		t.Fatal(e)
	}
	f := &smipApp{server: s, workspace: w}
	file := smipUpload(t, f, []byte("private setup policy bytes"))
	expiry := lidza.Now(s.Context()).Add(24 * time.Hour)
	var share schema.ShareCreated
	path := "/api/v1/workspaces/" + w.ID + "/drive/files/" + file.ID + "/shares"
	f.request(t, "POST", path, schema.ShareInput{ExpiresAt: expiry}, &share, 200)
	t.Setenv("THURA_ALLOW_ANONYMOUS_SHARES", "false")
	f.request(t, "POST", path, schema.ShareInput{ExpiresAt: expiry}, nil, 403)
	f.request(t, "POST", "/api/v1/shares/open", schema.OpenShareInput{Token: share.Token}, nil, 403)
	recipient := in.Email
	f.request(t, "POST", path, schema.ShareInput{ExpiresAt: expiry, TargetEmail: &recipient}, &share, 200)
	f.request(t, "POST", "/api/v1/shares/open", schema.OpenShareInput{Token: share.Token}, nil, 200)
	t.Setenv("THURA_ALLOW_SHARES", "false")
	f.request(t, "POST", "/api/v1/shares/open", schema.OpenShareInput{Token: share.Token}, nil, 403)
	f.request(t, "POST", path, schema.ShareInput{ExpiresAt: expiry, TargetEmail: &recipient}, nil, 403)
	t.Setenv("THURA_MAX_MEMBERS", "2")
	base := "/api/v1/workspaces/" + w.ID + "/invitations"
	f.request(t, "POST", base, schema.InviteInput{Email: "one@setup.example", Role: schema.WorkspaceRoleMember}, nil, 201)
	f.request(t, "POST", base, schema.InviteInput{Email: "two@setup.example", Role: schema.WorkspaceRoleMember}, nil, 409)
	admin, e := auth.From(s.Context()).CreateUser(s.Context(), "admin@setup.example", "Workspace admin", in.Password)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.From(s.Context()).Exec(s.Context(), "INSERT INTO auth_member(subject,scope,role) VALUES($1,$2,'admin')", admin.Subject, w.ID); e != nil {
		t.Fatal(e)
	}
	t.Setenv("THURA_MAX_MEMBERS", "100")
	t.Setenv("THURA_ADMIN_INVITES", "false")
	f.request(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: admin.Email, Password: in.Password}, nil, 200)
	f.request(t, "POST", base, schema.InviteInput{Email: "three@setup.example", Role: schema.WorkspaceRoleMember}, nil, 403)
	// A workspace admin is not a server admin and cannot lift these policies.
	response, e := s.Client().Get(s.URL + "/admin/setup")
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("workspace admin gained server setup authority")
	}
}

func TestSetupInvitationAcceptanceRechecksReducedQuota(t *testing.T) {
	s := setupServer(t)
	in := claimSetup(t, s)
	completeSetup(t, s)
	revision, _, _ := setupRow(t, s)
	if r := setupForm(t, s, "publish", url.Values{"revision": {strconv.Itoa(revision)}, "confirm": {"publish"}}); strings.Contains(r.Header.Get("Location"), "error=") {
		t.Fatal("policy fixture setup publication failed")
	}
	if e := setup.Activate(s.Context(), s.Services); e != nil {
		t.Fatal(e)
	}
	w, e := workspace.Create(s.Context(), schema.CreateWorkspaceInput{Name: "Seat limit", OwnerEmail: in.Email})
	if e != nil {
		t.Fatal(e)
	}
	token := "PUBLIC-invitation-quota-fixture-token"
	digest := sha256.Sum256([]byte(token))
	var subject string
	if e = db.From(s.Context()).QueryRow(s.Context(), "SELECT subject FROM auth_user WHERE email=$1", in.Email).Scan(&subject); e != nil {
		t.Fatal(e)
	}
	if _, e = db.From(s.Context()).Exec(s.Context(), "INSERT INTO invitation(workspace_id,email,role,token_hash,invited_by,expires_at) VALUES($1,'quota-guest@setup.example','member',$2,$3,now()+interval '1 day')", w.ID, hex.EncodeToString(digest[:]), subject); e != nil {
		t.Fatal(e)
	}
	if r := s.JSON(t, "POST", "/api/v1/auth/logout", nil, nil); r.StatusCode != 204 {
		t.Fatal("logout failed")
	}
	name, password := "Guest", "setup guest oak private lantern 78433"
	input := schema.AcceptInviteInput{Token: token, Name: &name, Password: &password}
	t.Setenv("THURA_MAX_MEMBERS", "1")
	if r := s.JSON(t, "POST", "/api/v1/invitations/accept", input, nil); r.StatusCode != 409 {
		t.Fatal("reduced quota did not block acceptance")
	}
	var accounts int
	if e = db.From(s.Context()).QueryRow(s.Context(), "SELECT COUNT(*) FROM auth_user WHERE email='quota-guest@setup.example'").Scan(&accounts); e != nil || accounts != 0 {
		t.Fatal("refused acceptance left an account behind")
	}
	t.Setenv("THURA_MAX_MEMBERS", "2")
	if r := s.JSON(t, "POST", "/api/v1/invitations/accept", input, nil); r.StatusCode != 200 {
		t.Fatal("quota repair did not recover original invitation")
	}
	if e = db.From(s.Context()).QueryRow(s.Context(), "SELECT COUNT(*) FROM auth_member WHERE scope=$1", w.ID).Scan(&accounts); e != nil || accounts != 2 {
		t.Fatal("recovered acceptance did not retain exactly two members")
	}
}
