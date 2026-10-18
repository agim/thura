package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"thura/app"
	"thura/internal/drive"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func TestCorrespondenceSearchesLiveContent(t *testing.T) {
	var calls atomic.Int32
	var unavailable atomic.Bool
	matrix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/_matrix/client/v3/search" {
			t.Errorf("search mutated membership: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		if !strings.HasPrefix(r.URL.Query().Get("user_id"), "@thura_") || strings.HasPrefix(r.URL.Query().Get("user_id"), "@thura_bot:") {
			t.Error("wrong search identity")
		}
		var input struct {
			Categories struct {
				Events struct {
					Term   string `json:"search_term"`
					Filter struct {
						Rooms []string `json:"rooms"`
					} `json:"filter"`
				} `json:"room_events"`
			} `json:"search_categories"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || len(input.Categories.Events.Filter.Rooms) != 1 || input.Categories.Events.Filter.Rooms[0] != "!search:matrix.test" {
			t.Error("unscoped search")
		}
		if unavailable.Load() {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"errcode":"M_UNAVAILABLE"}`)
			return
		}
		next := "older"
		if r.URL.Query().Get("next_batch") == "older" {
			next = ""
		}
		event := func(room, body string) any {
			return map[string]any{"result": map[string]any{"room_id": room, "event_id": "$match", "type": "m.room.message", "sender": "@john:matrix.test", "origin_server_ts": 1760000000000, "content": map[string]string{"body": body}}}
		}
		json.NewEncoder(w).Encode(map[string]any{"search_categories": map[string]any{"room_events": map[string]any{"next_batch": next, "results": []any{event("!search:matrix.test", "The fiber renewal is approved."), event("!other:matrix.test", "private unrelated secret")}}}})
	}))
	defer matrix.Close()
	t.Setenv("MATRIX_SERVER_URL", matrix.URL)
	t.Setenv("MATRIX_SERVER_NAME", "matrix.test")
	t.Setenv("MATRIX_AS_TOKEN", "synthetic-content-search-token-at-least-32-characters")
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	pool := db.From(ctx)
	email := fmt.Sprintf("content-%d@example.com", time.Now().UnixNano())
	pw := "maple lantern violet lake 7842"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	space, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Content search", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	insert := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, sql+" RETURNING id", args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM contact WHERE workspace_id=$1", "DELETE FROM chat_participant WHERE room_id IN(SELECT id FROM chat_room WHERE workspace_id=$1)", "DELETE FROM chat_room WHERE workspace_id=$1", "DELETE FROM file_version WHERE file_id IN(SELECT id FROM drive_file WHERE workspace_id=$1)", "DELETE FROM drive_file WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			exec(sql, space.ID)
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	status := func(method, path string, in, out any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, in, out)
		if res.StatusCode != want {
			t.Fatalf("%s %s: %d want %d", method, path, res.StatusCode, want)
		}
	}
	status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	base := "/api/v1/workspaces/" + space.ID
	var person schema.Contact
	status("POST", base+"/contacts", schema.ContactInput{Name: "John", Email: "john@example.com"}, &person, 201)
	path := base + "/contacts/" + person.ID + "/loop"
	room := insert("INSERT INTO chat_room(workspace_id,matrix_room_id,request_id,name,direct,created_by) VALUES($1,'!search:matrix.test',gen_random_uuid(),'Project channel',true,$2)", space.ID, owner.Subject)
	exec("INSERT INTO chat_participant(room_id,subject) VALUES($1,$2)", room, owner.Subject)
	file := insert("INSERT INTO drive_file(workspace_id,name,content_type,size) VALUES($1,'agreement.txt','text/plain',32)", space.ID)
	put := func(number int, body string) {
		t.Helper()
		key := fmt.Sprintf("content-test/%s/%d", file, number)
		_, err := storage.From(ctx).Put(ctx, key, bytes.NewBufferString(body), storage.PutOptions{ContentType: "text/plain"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { storage.From(ctx).Delete(ctx, key) })
		exec("INSERT INTO file_version(file_id,number,object_key,checksum,size,created_by) VALUES($1,$2,$3,$4,$5,$6)", file, number, key, drive.Hash([]byte(body)), len(body), owner.Subject)
	}
	put(1, strings.Repeat("unrelated prefix ", 100)+"The fiber renewal is approved.")
	for _, item := range []struct{ source, id string }{{"chat", room}, {"document", file}} {
		status("PUT", path+"/links", schema.CorrespondenceLinkInput{Source: item.source, SourceID: item.id}, nil, 200)
	}
	var result schema.CorrespondenceTimeline
	status("GET", path+"?search=fiber", nil, &result, 200)
	if len(result.Items) != 2 || len(result.Notices) != 0 {
		t.Fatalf("content search: %+v", result)
	}
	for _, item := range result.Items {
		if !strings.Contains(item.Preview, "fiber renewal") || strings.Contains(item.Preview, "private unrelated") {
			t.Fatalf("wrong excerpt: %+v", item)
		}
	}
	var messages schema.ChatTimeline
	status("GET", base+"/chat/"+room+"/search?search=fiber", nil, &messages, 200)
	if len(messages.Items) != 1 || messages.Next == nil {
		t.Fatalf("room search: %+v", messages)
	}
	status("GET", base+"/chat/"+room+"/search?search=fiber&cursor="+url.QueryEscape(*messages.Next), nil, &messages, 200)
	if messages.Next != nil {
		t.Fatal("cursor was not forwarded")
	}
	put(2, "Replacement without the old term.")
	exec("UPDATE drive_file SET current_version=2 WHERE id=$1", file)
	status("GET", path+"?search=fiber&source=document", nil, &result, 200)
	if len(result.Items) != 0 {
		t.Fatal("old document version leaked into search")
	}
	unavailable.Store(true)
	status("GET", path+"?search=fiber", nil, &result, 200)
	if len(result.Notices) == 0 || len(result.Items) != 0 {
		t.Fatalf("provider outage silently reported no matches: %+v", result)
	}
	unavailable.Store(false)
	exec("DELETE FROM chat_participant WHERE room_id=$1", room)
	before := calls.Load()
	status("GET", path+"?search=fiber", nil, &result, 200)
	if calls.Load() != before || len(result.Items) != 0 {
		t.Fatal("inaccessible room searched")
	}
	status("GET", base+"/chat/"+room+"/search?search=fiber", nil, nil, 404)
	exec("UPDATE drive_file SET trashed=true WHERE id=$1", file)
	status("GET", path+"?search=Replacement", nil, &result, 200)
	if len(result.Items) != 0 {
		t.Fatal("trash searched")
	}
	exec("DELETE FROM auth_member WHERE scope=$1", space.ID)
	status("GET", path+"?search=fiber", nil, nil, 404)
}
