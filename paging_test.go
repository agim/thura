package main

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"thura/internal/mailbox"
	"thura/internal/workspace"
	"thura/schema"
)

func TestDirectoryAndMailPagingSearchIsolation(t *testing.T) {
	t.Setenv("JOBS_WORKERS", "0")
	srv := lidzatest.Start(t, app())
	ctx := srv.Context()
	password := "lilac copper meadow lantern 7829"
	var spaces []schema.Workspace
	var users []string
	var boxes []schema.Mailbox
	for n := 0; n < 2; n++ {
		email := fmt.Sprintf("paging-%d-%d@example.com", time.Now().UnixNano(), n)
		user, err := auth.From(ctx).CreateUser(ctx, email, "Paging owner", password)
		if err != nil {
			t.Fatal(err)
		}
		users = append(users, user.Subject)
		space, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Paging fixture", OwnerEmail: email})
		if err != nil {
			t.Fatal(err)
		}
		spaces = append(spaces, space)
		box, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: space.ID, Name: "Paging mailbox", Address: email})
		if err != nil {
			t.Fatal(err)
		}
		boxes = append(boxes, box)
		if n == 0 && srv.JSON(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: password}, nil).StatusCode != 200 {
			t.Fatal("login failed")
		}
	}
	t.Cleanup(func() {
		for _, space := range spaces {
			db.From(ctx).Exec(ctx, "DELETE FROM mail_item WHERE mailbox_id IN (SELECT id FROM mailbox WHERE workspace_id=$1)", space.ID)
			db.From(ctx).Exec(ctx, "DELETE FROM mailbox WHERE workspace_id=$1", space.ID)
			db.From(ctx).Exec(ctx, "DELETE FROM contact WHERE workspace_id=$1", space.ID)
			db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE scope=$1", space.ID)
			db.From(ctx).Exec(ctx, "DELETE FROM workspace WHERE id=$1", space.ID)
		}
		for _, user := range users {
			auth.From(ctx).DeleteUser(ctx, user)
		}
	})
	_, err := db.From(ctx).Exec(ctx, `INSERT INTO contact(workspace_id,name,email,company)
SELECT $1,'Entry ' || lpad((n/3)::text,3,'0'),'page-' || n || '@example.com',CASE WHEN n=603 THEN '100% literal' ELSE '' END FROM generate_series(1,603) n`, spaces[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.From(ctx).Exec(ctx, `INSERT INTO mail_item(mailbox_id,author_id,folder,subject,text_body,status,updated_at)
SELECT $1,'','inbox',CASE WHEN n=251 THEN 'Older unique mail' ELSE 'Ordinary mail' END,'Body','received',now() - (n/3 * interval '1 second') FROM generate_series(1,251) n`, boxes[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.From(ctx).Exec(ctx, "INSERT INTO contact(workspace_id,name,email,company) VALUES($1,'Foreign private name','foreign@example.com','100% literal')", spaces[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	contacts := "/api/v1/workspaces/" + spaces[0].ID + "/contacts"
	mail := "/api/v1/workspaces/" + spaces[0].ID + "/mailboxes/" + boxes[0].ID + "/messages"
	get := func(path string, out any, want int) {
		t.Helper()
		if got := srv.JSON(t, "GET", path, nil, out).StatusCode; got != want {
			t.Fatalf("GET %s = %d, want %d", path, got, want)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; ; page++ {
		if page > 10 {
			t.Fatal("contact cursor failed to advance")
		}
		var result schema.ContactList
		get(contacts+"?limit=100&cursor="+url.QueryEscape(cursor), &result, 200)
		if len(result.Items) > 100 {
			t.Fatal("contact page exceeds limit")
		}
		for _, item := range result.Items {
			if seen[item.ID] || item.WorkspaceID != spaces[0].ID {
				t.Fatal("duplicate or foreign contact")
			}
			seen[item.ID] = true
		}
		cursor = result.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 603 {
		t.Fatalf("only %d of 603 contacts accessible", len(seen))
	}
	seen = map[string]bool{}
	cursor = ""
	for page := 0; ; page++ {
		if page > 10 {
			t.Fatal("mail cursor failed to advance")
		}
		var result schema.MailItemList
		get(mail+"?limit=37&cursor="+url.QueryEscape(cursor), &result, 200)
		if len(result.Items) > 37 {
			t.Fatal("mail page exceeds limit")
		}
		for _, item := range result.Items {
			if seen[item.ID] || item.MailboxID != boxes[0].ID {
				t.Fatal("duplicate or foreign mail")
			}
			seen[item.ID] = true
		}
		cursor = result.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 251 {
		t.Fatalf("only %d of 251 messages accessible", len(seen))
	}
	var matches schema.ContactList
	get(contacts+"?search="+url.QueryEscape("100% LITERAL"), &matches, 200)
	if len(matches.Items) != 1 || matches.Items[0].Company != "100% literal" {
		t.Fatal("literal/case-insensitive search leaks or misses")
	}
	get(contacts+"?search="+url.QueryEscape("%' OR 1=1 --"), &matches, 200)
	if len(matches.Items) != 0 {
		t.Fatal("query text became SQL")
	}
	var older schema.MailItemList
	get(mail+"?search=Older+unique+mail", &older, 200)
	if len(older.Items) != 1 || older.Items[0].Subject != "Older unique mail" {
		t.Fatal("mail search misses older rows")
	}
	for _, path := range []string{contacts, mail} {
		for _, query := range []string{"limit=0", "limit=201", "limit=bad", "cursor=invalid", "search=" + strings.Repeat("x", 201)} {
			get(path+"?"+query, nil, 422)
		}
	}
	get("/api/v1/workspaces/"+spaces[1].ID+"/contacts?cursor=invalid", nil, 404)
	get("/api/v1/workspaces/"+spaces[0].ID+"/mailboxes/"+boxes[1].ID+"/messages?search=Body", nil, 404)
	_, err = db.From(ctx).Exec(ctx, "DELETE FROM auth_member WHERE subject=$1 AND scope=$2", users[0], spaces[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	get(contacts+"?limit=100", nil, 404)
	get(mail+"?search=Older", nil, 404)
}
