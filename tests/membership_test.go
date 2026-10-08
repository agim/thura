package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"thura/app"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/mail"
	"thura/internal/workspace"
	"thura/schema"
)

func TestInvitationLifecycleAndRoles(t *testing.T) {
	t.Setenv("AUTH_LOGIN_BURST", "100")
	t.Setenv("AUTH_SIGNIN_BURST", "100")
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	password := "bamboo waterfall blue lantern 8512"
	ownerEmail := fmt.Sprintf("inviter-%d@example.com", time.Now().UnixNano())
	owner, err := auth.From(ctx).CreateUser(ctx, ownerEmail, "Owner", password)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Invitation workspace", OwnerEmail: ownerEmail})
	if err != nil {
		t.Fatal(err)
	}
	createdEmails := []string{ownerEmail}
	t.Cleanup(func() {
		db.From(ctx).Exec(ctx, "DELETE FROM invitation WHERE workspace_id=$1", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM workspace WHERE id=$1", w.ID)
		for _, email := range createdEmails {
			if p, err := auth.From(ctx).ProfileByEmail(ctx, email); err == nil {
				auth.From(ctx).DeleteUser(ctx, p.Subject)
			}
		}
	})
	status := func(method, path string, body, out any, want int) {
		t.Helper()
		r := srv.JSON(t, method, path, body, out)
		if r.StatusCode != want {
			t.Fatalf("%s %s: got %d want %d", method, path, r.StatusCode, want)
		}
	}
	login := func(email string) {
		t.Helper()
		status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: password}, nil, 200)
	}
	logout := func() { t.Helper(); status("POST", "/api/v1/auth/logout", nil, nil, 204) }
	base := "/api/v1/workspaces/" + w.ID
	invite := func(email string, r schema.WorkspaceRole) (schema.InviteView, string) {
		t.Helper()
		var view schema.InviteView
		status("POST", base+"/invitations", schema.InviteInput{Email: email, Role: r}, &view, 201)
		msg, err := mail.From(ctx).WaitFor(ctx, email, "Invitation", 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		token := ""
		if msg.Text != nil {
			for _, word := range strings.Fields(*msg.Text) {
				if i := strings.Index(word, "/invite?token="); i >= 0 {
					token = word[i+len("/invite?token="):]
				}
			}
		}
		if token == "" {
			t.Fatal("invitation mail missing link")
		}
		return view, token
	}
	login(ownerEmail)
	// Last-owner removal and demotion must both fail.
	status("DELETE", base+"/members/"+owner.Subject, nil, nil, 409)
	status("PATCH", base+"/members/"+owner.Subject, schema.RoleInput{Role: schema.WorkspaceRoleAdmin}, nil, 409)
	email := fmt.Sprintf("invited-%d@example.com", time.Now().UnixNano())
	createdEmails = append(createdEmails, email)
	view, token := invite(email, schema.WorkspaceRoleMember)
	var hash string
	if err := db.From(ctx).QueryRow(ctx, "SELECT token_hash FROM invitation WHERE id=$1", view.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == token || len(hash) != 64 {
		t.Fatal("invitation did not store only a hash")
	}
	logout()
	name := "Invited Member"
	status("POST", "/api/v1/invitations/accept", schema.AcceptInviteInput{Token: token, Name: &name, Password: &password}, nil, 200)
	status("POST", "/api/v1/invitations/accept", schema.AcceptInviteInput{Token: token, Name: &name, Password: &password}, nil, 410)
	login(email)
	var members schema.MemberList
	status("GET", base+"/members", nil, &members, 200)
	if len(members.Items) != 2 {
		t.Fatalf("members=%d", len(members.Items))
	}
	// Members cannot invite, inspect invite links, or escalate their own role.
	status("POST", base+"/invitations", schema.InviteInput{Email: "nobody@example.com", Role: schema.WorkspaceRoleOwner}, nil, 403)
	status("GET", base+"/invitations", nil, nil, 403)
	p, err := auth.From(ctx).ProfileByEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	status("PATCH", base+"/members/"+p.Subject, schema.RoleInput{Role: schema.WorkspaceRoleOwner}, nil, 403)
	logout()
	login(ownerEmail)
	status("PATCH", base+"/members/"+p.Subject, schema.RoleInput{Role: schema.WorkspaceRoleAdmin}, nil, 204)
	logout()
	login(email)
	status("POST", base+"/invitations", schema.InviteInput{Email: "nobody@example.com", Role: schema.WorkspaceRoleAdmin}, nil, 403)
	status("DELETE", base+"/members/"+owner.Subject, nil, nil, 403)
	logout()
	login(ownerEmail)
	revoked, revokedToken := invite(fmt.Sprintf("revoked-%d@example.com", time.Now().UnixNano()), schema.WorkspaceRoleMember)
	status("DELETE", base+"/invitations/"+revoked.ID, nil, nil, 204)
	_, expiredToken := invite(fmt.Sprintf("expired-%d@example.com", time.Now().UnixNano()), schema.WorkspaceRoleMember)
	logout()
	status("POST", "/api/v1/invitations/accept", schema.AcceptInviteInput{Token: revokedToken, Name: &name, Password: &password}, nil, 410)
	srv.Clock.Advance(73 * time.Hour)
	status("POST", "/api/v1/invitations/accept", schema.AcceptInviteInput{Token: expiredToken, Name: &name, Password: &password}, nil, 410)
	srv.Clock.Resume()
	// Existing accounts must authenticate as the invited subject; a token
	// cannot be used to overwrite their password or another user's identity.
	login(ownerEmail)
	_, existingToken := invite(email, schema.WorkspaceRoleMember)
	logout()
	status("POST", "/api/v1/invitations/accept", schema.AcceptInviteInput{Token: existingToken, Name: &name, Password: &password}, nil, 409)
	login(email)
	status("POST", "/api/v1/invitations/accept", schema.AcceptInviteInput{Token: existingToken}, nil, 200)
	// Accepting a member invitation must not downgrade an existing administrator.
	status("GET", base+"/members", nil, &members, 200)
	found := false
	for _, m := range members.Items {
		if m.Subject == p.Subject {
			found = m.Role == schema.WorkspaceRoleAdmin
		}
	}
	if !found {
		t.Fatal("existing member role changed through invite")
	}
	logout()
	login(ownerEmail)
	status("DELETE", base+"/members/"+p.Subject, nil, nil, 204)
	logout()
	login(email)
	status("GET", base+"/contacts", nil, nil, http.StatusNotFound)
}

func TestConcurrentLastOwnerRemoval(t *testing.T) {
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	password := "silver mountain waterfall lantern 8294"
	ownerEmail := fmt.Sprintf("race-owner-%d@example.com", time.Now().UnixNano())
	first, err := auth.From(ctx).CreateUser(ctx, ownerEmail, "First", password)
	if err != nil {
		t.Fatal(err)
	}
	second, err := auth.From(ctx).CreateUser(ctx, fmt.Sprintf("race-second-%d@example.com", time.Now().UnixNano()), "Second", password)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Concurrent owners", OwnerEmail: ownerEmail})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.From(ctx).Exec(ctx, "INSERT INTO auth_member(subject,scope,role) VALUES($1,$2,'owner')", second.Subject, w.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM workspace WHERE id=$1", w.ID)
		auth.From(ctx).DeleteUser(ctx, first.Subject)
		auth.From(ctx).DeleteUser(ctx, second.Subject)
	})
	tokens := map[string]string{}
	for _, subject := range []string{first.Subject, second.Subject} {
		token, err := auth.From(ctx).Login(ctx, subject, nil)
		if err != nil {
			t.Fatal(err)
		}
		tokens[subject] = token.Access
	}
	statuses := make(chan int, 2)
	for _, subject := range []string{first.Subject, second.Subject} {
		go func(subject string) {
			req, err := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/workspaces/"+w.ID+"/members/"+subject, nil)
			if err != nil {
				statuses <- 0
				return
			}
			req.Header.Set("Authorization", "Bearer "+tokens[subject])
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				statuses <- 0
				return
			}
			res.Body.Close()
			statuses <- res.StatusCode
		}(subject)
	}
	got := []int{<-statuses, <-statuses}
	sort.Ints(got)
	if got[0] != 204 || got[1] != 409 {
		t.Fatalf("concurrent owner removals=%v", got)
	}
	var owners int
	if err = db.From(ctx).QueryRow(ctx, "SELECT count(*) FROM auth_member WHERE scope=$1 AND role='owner'", w.ID).Scan(&owners); err != nil || owners != 1 {
		t.Fatalf("owners=%d err=%v", owners, err)
	}
}

func TestConcurrentInviteAcceptance(t *testing.T) {
	t.Setenv("AUTH_LOGIN_BURST", "100")
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	password := "sunrise meadow bamboo lantern 3581"
	ownerEmail := fmt.Sprintf("replay-owner-%d@example.com", time.Now().UnixNano())
	owner, err := auth.From(ctx).CreateUser(ctx, ownerEmail, "Owner", password)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Replay", OwnerEmail: ownerEmail})
	if err != nil {
		t.Fatal(err)
	}
	email := fmt.Sprintf("replay-invited-%d@example.com", time.Now().UnixNano())
	t.Cleanup(func() {
		db.From(ctx).Exec(ctx, "DELETE FROM invitation WHERE workspace_id=$1", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", w.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM workspace WHERE id=$1", w.ID)
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
		if p, e := auth.From(ctx).ProfileByEmail(ctx, email); e == nil {
			auth.From(ctx).DeleteUser(ctx, p.Subject)
		}
	})
	res := srv.JSON(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: ownerEmail, Password: password}, nil)
	if res.StatusCode != 200 {
		t.Fatal("owner login failed")
	}
	res = srv.JSON(t, "POST", "/api/v1/workspaces/"+w.ID+"/invitations", schema.InviteInput{Email: email, Role: schema.WorkspaceRoleMember}, nil)
	if res.StatusCode != 201 {
		t.Fatal("invite failed")
	}
	msg, err := mail.From(ctx).WaitFor(ctx, email, "Invitation", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	token := ""
	if msg.Text != nil {
		for _, word := range strings.Fields(*msg.Text) {
			if i := strings.Index(word, "/invite?token="); i >= 0 {
				token = word[i+len("/invite?token="):]
			}
		}
	}
	if token == "" {
		t.Fatal("no invite token")
	}
	body, err := json.Marshal(map[string]string{"token": token, "name": "Replay Member", "password": password})
	if err != nil {
		t.Fatal(err)
	}
	statuses := make(chan int, 2)
	for range 2 {
		go func() {
			res, err := http.Post(srv.URL+"/api/v1/invitations/accept", "application/json", bytes.NewReader(body))
			if err != nil {
				statuses <- 0
				return
			}
			res.Body.Close()
			statuses <- res.StatusCode
		}()
	}
	got := []int{<-statuses, <-statuses}
	sort.Ints(got)
	if got[0] != 200 || got[1] != 410 {
		t.Fatalf("replayed invite=%v", got)
	}
}
