package tests

import (
	"encoding/json"
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"thura/app"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func TestChatScopesPrivateRoomsAndIdempotentSends(t *testing.T) {
	token := "thura-matrix-stub-application-service-fixture-token-72498"
	var mu sync.Mutex
	created := 0
	sent := map[string]string{}
	matrix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("wrong service credential")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.Contains(r.URL.Path, "/directory/room/"):
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]string{"errcode": "M_NOT_FOUND"})
		case strings.HasSuffix(r.URL.Path, "/createRoom"):
			created++
			json.NewEncoder(w).Encode(map[string]string{"room_id": fmt.Sprintf("!room%d:matrix.test", created)})
		case strings.Contains(r.URL.Path, "/state/m.room.member/"):
			json.NewEncoder(w).Encode(map[string]string{"membership": "join"})
		case strings.Contains(r.URL.Path, "/send/m.room.message/"):
			key := r.URL.Query().Get("user_id") + r.URL.Path
			if sent[key] == "" {
				sent[key] = fmt.Sprintf("$message%d", len(sent)+1)
			}
			json.NewEncoder(w).Encode(map[string]string{"event_id": sent[key]})
		case strings.HasSuffix(r.URL.Path, "/messages"):
			json.NewEncoder(w).Encode(map[string]any{"chunk": []any{map[string]any{"event_id": "$message1", "sender": "@remote:elsewhere.test", "type": "m.room.message", "origin_server_ts": 1760000000000, "content": map[string]string{"msgtype": "m.text", "body": "Remote message"}}}, "end": "older-cursor"})
		default:
			json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	defer matrix.Close()
	t.Setenv("MATRIX_SERVER_URL", matrix.URL)
	t.Setenv("MATRIX_SERVER_NAME", "matrix.test")
	t.Setenv("MATRIX_AS_TOKEN", token)
	srv := lidzatest.Start(t, app.New(nil))
	ctx := srv.Context()
	pool := db.From(ctx)
	suffix := time.Now().UnixNano()
	pw := "forest maple waterfall lantern 7143"
	ownerEmail := fmt.Sprintf("chat-owner-%d@example.com", suffix)
	otherEmail := fmt.Sprintf("chat-other-%d@example.com", suffix)
	thirdEmail := fmt.Sprintf("chat-third-%d@example.com", suffix)
	owner, err := auth.From(ctx).CreateUser(ctx, ownerEmail, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	other, err := auth.From(ctx).CreateUser(ctx, otherEmail, "Other", pw)
	if err != nil {
		t.Fatal(err)
	}
	third, err := auth.From(ctx).CreateUser(ctx, thirdEmail, "Third", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Chat test", OwnerEmail: ownerEmail})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{other.Subject, third.Subject} {
		if _, err = pool.Exec(ctx, "INSERT INTO auth_member(subject,scope,role) VALUES($1,$2,'member')", s, w.ID); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM chat_participant WHERE room_id IN (SELECT id FROM chat_room WHERE workspace_id=$1)", "DELETE FROM chat_room WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, err = pool.Exec(ctx, sql, w.ID); err != nil {
				t.Error(err)
			}
		}
		for _, s := range []string{owner.Subject, other.Subject, third.Subject} {
			auth.From(ctx).DeleteUser(ctx, s)
		}
	})
	status := func(method, path string, in, out any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, in, out)
		if res.StatusCode != want {
			t.Fatalf("%s got %d want %d", path, res.StatusCode, want)
		}
	}
	login := func(email string) {
		t.Helper()
		status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	}
	base := "/api/v1/workspaces/" + w.ID + "/chat"
	status("GET", base, nil, nil, 401)
	login(ownerEmail)
	var channel, direct, again schema.ChatRoom
	input := schema.ChatRoomInput{Name: "General", RequestID: uuid.NewString()}
	status("POST", base, input, &channel, 200)
	status("POST", base, input, &again, 200)
	if again.ID != channel.ID || created != 1 {
		t.Fatal("room retry duplicated room")
	}
	input = schema.ChatRoomInput{Name: "Private", RequestID: uuid.NewString(), Participant: &other.Subject}
	status("POST", base, input, &direct, 200)
	path := base + "/" + channel.ID
	var timeline schema.ChatTimeline
	status("GET", path+"/messages", nil, &timeline, 200)
	if len(timeline.Items) != 1 || timeline.Items[0].Body != "Remote message" {
		t.Fatal("timeline missing")
	}
	var first, repeat schema.ChatEventResult
	send := schema.ChatSendInput{TransactionID: uuid.NewString(), Body: "Hello"}
	status("POST", path+"/messages", send, &first, 200)
	status("POST", path+"/messages", send, &repeat, 200)
	if first.EventID != repeat.EventID || len(sent) != 1 {
		t.Fatal("send retry duplicated event")
	}
	status("GET", "/api/v1/workspaces/"+uuid.NewString()+"/chat/"+channel.ID+"/messages", nil, nil, 404)
	status("POST", base+"/"+direct.ID+"/invitations", schema.ChatRemoteInput{UserID: "@guest:remote.test"}, nil, 422)
	login(thirdEmail)
	status("GET", base+"/"+direct.ID+"/messages", nil, nil, 404)
	status("POST", path+"/invitations", schema.ChatRemoteInput{UserID: "@guest:remote.test"}, nil, 403)
	login(otherEmail)
	status("GET", base+"/"+direct.ID+"/messages", nil, &timeline, 200)
	if _, err = pool.Exec(ctx, "DELETE FROM auth_member WHERE subject=$1 AND scope=$2", other.Subject, w.ID); err != nil {
		t.Fatal(err)
	}
	status("GET", base+"/"+direct.ID+"/messages", nil, nil, 404)
	status("POST", path+"/messages", schema.ChatSendInput{TransactionID: uuid.NewString(), Body: "Revoked"}, nil, 404)
}
