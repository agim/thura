package tests

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"thura/app"
	"thura/internal/workspace"
	"thura/schema"
)

func TestWorkspaceAuditAuthorizationPagingAndRollback(t *testing.T) {
	srv := lidzatest.Start(t, app.New(nil))
	ctx := srv.Context()
	pool := db.From(ctx)
	password := "audit meadow lantern waterfall 8319"
	stamp := time.Now().UnixNano()
	ownerEmail := fmt.Sprintf("audit-owner-%d@example.com", stamp)
	memberEmail := fmt.Sprintf("audit-member-%d@example.com", stamp)
	otherEmail := fmt.Sprintf("audit-other-%d@example.com", stamp)
	var profiles []auth.Profile
	for _, email := range []string{ownerEmail, memberEmail, otherEmail} {
		profile, err := auth.From(ctx).CreateUser(ctx, email, "Audit fixture", password)
		if err != nil {
			t.Fatal(err)
		}
		profiles = append(profiles, profile)
	}
	t.Cleanup(func() {
		for _, profile := range profiles {
			if err := auth.From(ctx).DeleteUser(ctx, profile.Subject); err != nil {
				t.Error(err)
			}
		}
	})
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Audit fixture", OwnerEmail: ownerEmail})
	if err != nil {
		t.Fatal(err)
	}
	other, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Other audit fixture", OwnerEmail: otherEmail})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, id := range []string{w.ID, other.ID} {
			for _, sql := range []string{"DELETE FROM audit_event WHERE scope=$1", "DELETE FROM invitation WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
				if _, err := pool.Exec(ctx, sql, id); err != nil {
					t.Error(err)
				}
			}
		}
	})
	if _, err := pool.Exec(ctx, "INSERT INTO auth_member(subject,scope,role) VALUES ($1,$2,'member')", profiles[1].Subject, w.ID); err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, input, output any, want int) {
		t.Helper()
		if res := srv.JSON(t, method, path, input, output); res.StatusCode != want {
			t.Fatalf("%s %s: got %d want %d", method, path, res.StatusCode, want)
		}
	}
	origin, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	sessions := map[string][]*http.Cookie{}
	login := func(email string) {
		t.Helper()
		if cookies := sessions[email]; len(cookies) > 0 {
			srv.Client().Jar.SetCookies(origin, cookies)
			return
		}
		request("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: password}, nil, 200)
		sessions[email] = srv.Client().Jar.Cookies(origin)
	}
	base := "/api/v1/workspaces/" + w.ID
	request("GET", base+"/audit", nil, nil, 401)
	login(memberEmail)
	request("GET", base+"/audit", nil, nil, 403)
	request("POST", base+"/invitations", schema.InviteInput{Email: "not-recorded@example.com", Role: schema.WorkspaceRoleOwner}, nil, 403)
	login(otherEmail)
	request("GET", base+"/audit", nil, nil, 404)
	login(ownerEmail)
	request("DELETE", base+"/members/"+profiles[0].Subject, nil, nil, 409)
	request("PATCH", base+"/members/"+profiles[1].Subject, schema.RoleInput{Role: schema.WorkspaceRoleAdmin}, nil, 204)
	var invitation schema.InviteView
	request("POST", base+"/invitations?actor=forged", schema.InviteInput{Email: "not-recorded@example.com", Role: schema.WorkspaceRoleMember}, &invitation, 201)
	request("DELETE", base+"/invitations/"+invitation.ID, nil, nil, 204)
	seen := map[string]bool{}
	cursor := ""
	var all []schema.WorkspaceAuditRecord
	for {
		var page schema.WorkspaceAuditList
		request("GET", base+"/audit?limit=2&scope="+other.ID+"&cursor="+cursor, nil, &page, 200)
		if len(page.Items) > 2 {
			t.Fatal("audit page exceeded limit")
		}
		for _, record := range page.Items {
			if seen[record.ID] {
				t.Fatal("duplicate audit record across cursor pages")
			}
			seen[record.ID] = true
			all = append(all, record)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(all) != 6 {
		t.Fatalf("expected all 6 scoped events, got %+v", all)
	}
	found := map[string]bool{}
	for _, record := range all {
		found[record.Action+":"+record.Outcome] = true
		if record.Action == "invitation.create" && record.Outcome == audit.OK && record.Actor != profiles[0].Subject {
			t.Fatal("audit actor was not the authenticated owner")
		}
		for _, detail := range record.Details {
			if strings.Contains(detail.Value, "not-recorded@example.com") || strings.Contains(detail.Name, "token") || strings.Contains(detail.Value, password) {
				t.Fatal("sensitive request content reached audit metadata")
			}
		}
	}
	for _, event := range []string{"workspace.create:ok", "invitation.create:denied", "member.remove:denied", "member.role:ok", "invitation.create:ok", "invitation.revoke:ok"} {
		if !found[event] {
			t.Errorf("missing audit outcome %s", event)
		}
	}
	var filtered schema.WorkspaceAuditList
	request("GET", base+"/audit?action=invitation.create&outcome=ok", nil, &filtered, 200)
	if len(filtered.Items) != 1 || filtered.Items[0].Resource != "invitation/"+invitation.ID {
		t.Fatal("audit action/outcome filters returned the wrong records")
	}
	request("GET", base+"/audit?limit=201", nil, nil, 422)
	request("GET", base+"/audit?cursor=broken", nil, nil, 422)
	badUUID := base64.RawURLEncoding.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano) + " not-a-uuid"))
	request("GET", base+"/audit?cursor="+badUUID, nil, nil, 422)
	request("GET", base+"/audit?outcome=unknown", nil, nil, 422)
	login(memberEmail)
	request("GET", base+"/audit", nil, nil, 200)
	login(ownerEmail)
	request("DELETE", base+"/members/"+profiles[1].Subject, nil, nil, 204)
	login(memberEmail)
	request("GET", base+"/audit", nil, nil, 404)
	login(ownerEmail)
	// An unavailable audit store must roll back both the invitation and mail job.
	function := fmt.Sprintf("thura_audit_fail_%d", stamp)
	if _, err := pool.Exec(ctx, "CREATE FUNCTION "+function+"() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic audit failure'; END $$"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DROP TRIGGER IF EXISTS "+function+" ON audit_event; DROP FUNCTION "+function+"()"); err != nil {
			t.Error(err)
		}
	})
	if _, err := pool.Exec(ctx, "CREATE TRIGGER "+function+" BEFORE INSERT ON audit_event FOR EACH ROW WHEN (NEW.scope='"+w.ID+"' AND NEW.action='invitation.create' AND NEW.outcome='ok') EXECUTE FUNCTION "+function+"()"); err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM job").Scan(&before); err != nil {
		t.Fatal(err)
	}
	request("POST", base+"/invitations", schema.InviteInput{Email: "rolled-back@example.com", Role: schema.WorkspaceRoleMember}, nil, 500)
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM invitation WHERE workspace_id=$1 AND email='rolled-back@example.com'", w.ID).Scan(&after); err != nil || after != 0 {
		t.Fatalf("failed audit left an invitation: %d %v", after, err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM job").Scan(&after); err != nil || after != before {
		t.Fatalf("failed audit queued a mail job: %d -> %d %v", before, after, err)
	}
	request("GET", base+"/audit?action=invitation.create&outcome=failed", nil, &filtered, 200)
	if len(filtered.Items) != 1 {
		t.Fatal("audit-store failure did not record the failed outcome")
	}
}
