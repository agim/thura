//go:build matrixintegration

package tests

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"net/url"
	"testing"
	"thura/app"
	provider "thura/internal/providers/matrix"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

// Dedicated real-service suite. Start the documented two Synapse fixtures first.
func TestMatrixRealFederationAndBan(t *testing.T) {
	t.Setenv("MATRIX_SERVER_URL", "http://127.0.0.1:8108")
	t.Setenv("MATRIX_SERVER_NAME", "matrix-a.thura.test:8448")
	t.Setenv("MATRIX_AS_TOKEN", "thura-matrix-a-application-service-fixture-token-99184")
	srv := startConfigured(t, app.New(nil))
	ctx := srv.Context()
	cfgA, err := provider.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfgB := provider.Config{URL: "http://127.0.0.1:8109", ServerName: "matrix-b.thura.test:8448", Token: "thura-matrix-b-application-service-fixture-token-99184"}
	for _, cfg := range []provider.Config{cfgA, cfgB} {
		ready := false
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if cfg.Call(ctx, "", "GET", "/_matrix/client/versions", nil, nil) == nil {
				ready = true
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if !ready {
			t.Fatal("Matrix fixture did not become ready")
		}
	}
	email := fmt.Sprintf("matrix-real-%d@example.com", time.Now().UnixNano())
	pw := "forest maple waterfall lantern 7143"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Federation fixture", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Real federation", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	pool := db.From(ctx)
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM chat_participant WHERE room_id IN (SELECT id FROM chat_room WHERE workspace_id=$1)", "DELETE FROM chat_room WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, err = pool.Exec(ctx, sql, w.ID); err != nil {
				t.Error(err)
			}
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	call := func(method, path string, in, out any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, in, out)
		if res.StatusCode != want {
			t.Fatalf("%s got %d want %d", path, res.StatusCode, want)
		}
	}
	call("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	base := "/api/v1/workspaces/" + w.ID + "/chat"
	var room schema.ChatRoom
	call("POST", base, schema.ChatRoomInput{Name: "Federated fixture", RequestID: uuid.NewString()}, &room, 200)
	remote := cfgB.User("federation-fixture-" + uuid.NewString())
	if err = cfgB.EnsureUser(ctx, remote); err != nil {
		t.Fatal(err)
	}
	path := base + "/" + room.ID
	call("POST", path+"/invitations", schema.ChatRemoteInput{UserID: remote}, nil, 204)
	if err = cfgB.Call(ctx, remote, "POST", "/_matrix/client/v3/join/"+url.PathEscape(room.MatrixRoomID)+"?server_name="+url.QueryEscape(cfgA.ServerName), map[string]any{}, nil); err != nil {
		t.Fatal(err)
	}
	body := "Cross-domain message " + uuid.NewString()
	var message struct {
		ID string `json:"event_id"`
	}
	if err = cfgB.Call(ctx, remote, "PUT", "/_matrix/client/v3/rooms/"+url.PathEscape(room.MatrixRoomID)+"/send/m.room.message/"+uuid.NewString(), map[string]string{"msgtype": "m.text", "body": body}, &message); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		var timeline schema.ChatTimeline
		call("GET", path+"/messages", nil, &timeline, 200)
		for _, m := range timeline.Items {
			if m.ID == message.ID && m.Body == body && m.Sender == remote {
				found = true
			}
		}
		if found {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !found {
		t.Fatal("remote message did not federate")
	}

	payload := []byte("Synthetic cross-domain attachment " + uuid.NewString())
	media, xerr := cfgB.Upload(ctx, remote, "fixture.txt", payload, "application/octet-stream")
	if xerr != nil {
		t.Fatal(xerr)
	}
	var attachment struct {
		ID string `json:"event_id"`
	}
	if err = cfgB.Call(ctx, remote, "PUT", "/_matrix/client/v3/rooms/"+url.PathEscape(room.MatrixRoomID)+"/send/m.room.message/"+uuid.NewString(), map[string]any{"msgtype": "m.file", "body": "fixture.txt", "url": media, "info": map[string]any{"size": len(payload), "mimetype": "application/octet-stream"}}, &attachment); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(20 * time.Second)
	found = false
	for time.Now().Before(deadline) {
		var timeline schema.ChatTimeline
		call("GET", path+"/messages", nil, &timeline, 200)
		for _, m := range timeline.Items {
			if m.ID == attachment.ID && m.File != nil {
				found = true
			}
		}
		if found {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !found {
		t.Fatal("attachment event did not federate")
	}
	var download schema.FileContent
	call("GET", path+"/files?eventId="+url.QueryEscape(attachment.ID), nil, &download, 200)
	decoded, xerr := base64.StdEncoding.DecodeString(download.Data)
	if xerr != nil || !bytes.Equal(decoded, payload) || download.ContentType != "application/octet-stream" {
		t.Fatal("cross-domain attachment bytes differ")
	}
	var local, repeat schema.ChatEventResult
	send := schema.ChatSendInput{TransactionID: uuid.NewString(), Body: "Reply across domains"}
	call("POST", path+"/messages", send, &local, 200)
	call("POST", path+"/messages", send, &repeat, 200)
	if local.EventID != repeat.EventID {
		t.Fatal("real send retry duplicated event")
	}
	var back struct {
		Chunk []struct {
			ID string `json:"event_id"`
		} `json:"chunk"`
	}
	deadline = time.Now().Add(20 * time.Second)
	found = false
	for time.Now().Before(deadline) {
		if err = cfgB.Call(ctx, remote, "GET", "/_matrix/client/v3/rooms/"+url.PathEscape(room.MatrixRoomID)+"/messages?dir=b&limit=50", nil, &back); err != nil {
			t.Fatal(err)
		}
		for _, e := range back.Chunk {
			if e.ID == local.EventID {
				found = true
			}
		}
		if found {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !found {
		t.Fatal("local message did not reach second domain")
	}
	call("POST", path+"/bans", schema.ChatRemoteInput{UserID: remote}, nil, 204)
	deadline = time.Now().Add(20 * time.Second)
	denied := false
	for time.Now().Before(deadline) {
		err = cfgB.Call(ctx, remote, "PUT", "/_matrix/client/v3/rooms/"+url.PathEscape(room.MatrixRoomID)+"/send/m.room.message/"+uuid.NewString(), map[string]string{"msgtype": "m.text", "body": "Banned send"}, nil)
		if e, ok := err.(*provider.Error); ok && e.Status == 403 {
			denied = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !denied {
		t.Fatal("remote ban did not prevent sends")
	}
}
