package main

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"thura/internal/workspace"
	"thura/schema"
)

func TestContactsWorkspaceIsolation(t *testing.T) {
	srv := lidzatest.Start(t, app())
	ctx := srv.Context()
	email := fmt.Sprintf("owner-%d@example.com", time.Now().UnixNano())
	password := "maple lantern violet lake 7842"
	user, err := auth.From(ctx).CreateUser(ctx, email, "Owner", password)
	if err != nil {
		t.Fatal(err)
	}
	otherEmail := fmt.Sprintf("other-%d@example.com", time.Now().UnixNano())
	if _, err := auth.From(ctx).CreateUser(ctx, otherEmail, "Other", password); err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Owner workspace", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	other, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Other workspace", OwnerEmail: otherEmail})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.From(ctx).Exec(ctx, "DELETE FROM contact WHERE workspace_id IN ($1, $2)", w.ID, other.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope IN ($1, $2)", w.ID, other.ID)
		db.From(ctx).Exec(ctx, "DELETE FROM workspace WHERE id IN ($1, $2)", w.ID, other.ID)
		auth.From(ctx).DeleteUser(ctx, user.Subject)
		if p, err := auth.From(ctx).ProfileByEmail(ctx, otherEmail); err == nil {
			auth.From(ctx).DeleteUser(ctx, p.Subject)
		}
	})
	base := "/api/v1/workspaces/" + w.ID + "/contacts"
	status := func(method, path string, body, out any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, body, out)
		if res.StatusCode != want {
			t.Fatalf("%s %s: got %d want %d", method, path, res.StatusCode, want)
		}
	}
	status("GET", base, nil, nil, http.StatusUnauthorized)
	status("POST", "/api/v1/auth/register", auth.Registration{Email: "uninvited@example.com", Password: password}, nil, http.StatusNotFound)
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: password}, nil, http.StatusOK)
	var list schema.WorkspaceList
	status("GET", "/api/v1/workspaces", nil, &list, http.StatusOK)
	if len(list.Items) != 1 || list.Items[0].ID != w.ID {
		t.Fatalf("workspace list leaks: %+v", list)
	}
	company, phone := "Studio", "+1 410 555 1234"
	in := schema.ContactInput{Name: "  Alice  ", Email: "ALICE@example.com", Company: &company, Phone: &phone, Favorite: true}
	var c schema.Contact
	status("POST", base, in, &c, http.StatusCreated)
	if c.Name != "Alice" || c.Email != "alice@example.com" || c.WorkspaceID != w.ID || !c.Favorite {
		t.Fatalf("contact: %+v", c)
	}
	var contacts schema.ContactList
	status("GET", base, nil, &contacts, http.StatusOK)
	if len(contacts.Items) != 1 || contacts.Items[0].ID != c.ID {
		t.Fatalf("contacts: %+v", contacts)
	}
	in.Name = "Alice Updated"
	status("PATCH", base+"/"+c.ID, in, &c, http.StatusOK)
	if c.Name != in.Name {
		t.Fatalf("update: %+v", c)
	}
	forbidden := "/api/v1/workspaces/" + other.ID + "/contacts"
	status("GET", forbidden, nil, nil, http.StatusNotFound)
	status("POST", forbidden, in, nil, http.StatusNotFound)
	status("PATCH", forbidden+"/"+c.ID, in, nil, http.StatusNotFound)
	status("DELETE", forbidden+"/"+c.ID, nil, nil, http.StatusNotFound)
	status("GET", "/api/v1/workspaces/not-a-uuid/contacts", nil, nil, http.StatusNotFound)
	invalid := in
	invalid.Name = "   "
	status("POST", base, invalid, nil, http.StatusUnprocessableEntity)
	invalid = in
	invalid.Email = "invalid"
	status("POST", base, invalid, nil, http.StatusUnprocessableEntity)
	// Even a member of both workspaces cannot move a contact by changing its URL.
	if _, err := db.From(ctx).Exec(ctx, "INSERT INTO auth_member(subject, scope, role) VALUES($1, $2, 'member')", user.Subject, other.ID); err != nil {
		t.Fatal(err)
	}
	status("PATCH", forbidden+"/"+c.ID, in, nil, http.StatusNotFound)
	status("DELETE", forbidden+"/"+c.ID, nil, nil, http.StatusNotFound)
	status("DELETE", base+"/"+c.ID, nil, nil, http.StatusNoContent)
	status("GET", base, nil, &contacts, http.StatusOK)
	if len(contacts.Items) != 0 {
		t.Fatalf("deleted contact still visible: %+v", contacts)
	}
	// Revocation applies to an existing cookie session on its next request.
	if _, err := db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE subject=$1 AND scope=$2", user.Subject, w.ID); err != nil {
		t.Fatal(err)
	}
	status("GET", base, nil, nil, http.StatusNotFound)
	status("POST", base, in, nil, http.StatusNotFound)
}
