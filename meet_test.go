package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/lidzatest"
	"github.com/google/uuid"
	lkauth "github.com/livekit/protocol/auth"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"thura/internal/meet"
	"thura/internal/workspace"
	"thura/schema"
	"time"
)

func TestMeetingScopedTokensWebhooksAndRevocation(t *testing.T) {
	key, secret := "thura-meet-test-key", "thura-meet-test-synthetic-secret-874991"
	var removed atomic.Int32
	sfu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifier, xerr := lkauth.ParseAPIToken(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if xerr != nil {
			t.Error(xerr)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, grants, xerr := verifier.Verify(secret)
		if xerr != nil || grants.Video == nil {
			t.Error("invalid SFU grant")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if (strings.HasSuffix(r.URL.Path, "CreateRoom") || strings.HasSuffix(r.URL.Path, "DeleteRoom")) && !grants.Video.RoomCreate {
			t.Error("missing create/delete grant")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if strings.HasSuffix(r.URL.Path, "RemoveParticipant") && !grants.Video.RoomAdmin {
			t.Error("missing room admin grant")
			w.WriteHeader(http.StatusForbidden)
			return
		}

		if strings.HasSuffix(r.URL.Path, "RemoveParticipant") {
			removed.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer sfu.Close()
	t.Setenv("LIVEKIT_SERVER_URL", sfu.URL)
	t.Setenv("LIVEKIT_PUBLIC_URL", "ws://localhost:7880")
	t.Setenv("LIVEKIT_API_KEY", key)
	t.Setenv("LIVEKIT_API_SECRET", secret)
	srv := lidzatest.Start(t, app())
	ctx := srv.Context()
	pool := db.From(ctx)
	suffix := time.Now().UnixNano()
	email := fmt.Sprintf("meet-owner-%d@example.com", suffix)
	guestEmail := fmt.Sprintf("meet-guest-%d@example.com", suffix)
	pw := "forest maple waterfall lantern 7143"
	owner, err := auth.From(ctx).CreateUser(ctx, email, "Owner", pw)
	if err != nil {
		t.Fatal(err)
	}
	guest, err := auth.From(ctx).CreateUser(ctx, guestEmail, "Guest", pw)
	if err != nil {
		t.Fatal(err)
	}
	w, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Meeting test", OwnerEmail: email})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO auth_member(subject,scope,role) VALUES($1,$2,'member')", guest.Subject, w.ID); err != nil {
		t.Fatal(err)
	}
	webhookIDs := []string{}
	t.Cleanup(func() {
		for _, sql := range []string{"DELETE FROM meeting_participant WHERE meeting_id IN (SELECT id FROM meeting WHERE workspace_id=$1)", "DELETE FROM meeting WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1", "DELETE FROM workspace WHERE id=$1"} {
			if _, err = pool.Exec(ctx, sql, w.ID); err != nil {
				t.Error(err)
			}
		}
		if _, err = pool.Exec(ctx, "DELETE FROM meeting_webhook WHERE event_id=ANY($1)", webhookIDs); err != nil {
			t.Error(err)
		}
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
		auth.From(ctx).DeleteUser(ctx, guest.Subject)
	})
	status := func(method, path string, in, out any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, in, out)
		if res.StatusCode != want {
			t.Fatalf("%s got %d want %d", path, res.StatusCode, want)
		}
	}
	login := func(email string) {
		status("POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	}
	base := "/api/v1/workspaces/" + w.ID + "/meet"
	status("GET", base, nil, nil, 401)
	login(email)
	var meeting, repeat schema.Meeting
	in := schema.MeetingInput{Name: "Planning", RequestID: uuid.NewString()}
	status("POST", base, in, &meeting, 200)
	status("POST", base, in, &repeat, 200)
	if meeting.ID != repeat.ID {
		t.Fatal("meeting retry duplicated metadata")
	}
	var access schema.MeetingAccess
	path := base + "/" + meeting.ID
	status("POST", path+"/join", nil, &access, 200)
	verifier, err := lkauth.ParseAPIToken(access.Token)
	if err != nil {
		t.Fatal(err)
	}
	_, claims, err := verifier.Verify(secret)
	if err != nil || claims.Video == nil || claims.Identity != owner.Subject || claims.Video.Room != meet.Room(meeting.ID) || !claims.Video.RoomJoin || claims.Video.RoomAdmin || claims.Video.RoomCreate {
		t.Fatal("meeting token has wrong scope")
	}
	if time.Until(access.ExpiresAt) > 2*time.Minute || time.Until(access.ExpiresAt) < time.Minute {
		t.Fatal("unexpected access-token lifetime")
	}
	status("POST", "/api/v1/workspaces/"+uuid.NewString()+"/meet/"+meeting.ID+"/join", nil, nil, 404)
	hook := func(eventID, kind, subject, sid string, tamper bool, want int) {
		t.Helper()
		webhookIDs = append(webhookIDs, eventID)
		body, _ := json.Marshal(map[string]any{"id": eventID, "event": kind, "room": map[string]string{"name": meet.Room(meeting.ID)}, "participant": map[string]string{"identity": subject, "sid": sid}})
		digest := sha256.Sum256(body)
		token, err := lkauth.NewAccessToken(key, secret).SetValidFor(time.Minute).SetSha256(base64.StdEncoding.EncodeToString(digest[:])).ToJWT()
		if err != nil {
			t.Fatal(err)
		}
		if tamper {
			body = append(body, ' ')
		}
		req, err := http.NewRequest("POST", srv.URL+"/api/v1/meet/webhook", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("webhook got %d want %d", res.StatusCode, want)
		}
	}
	eventID := uuid.NewString()
	hook(eventID, "participant_joined", owner.Subject, "session-a", true, 401)
	hook(eventID, "participant_joined", owner.Subject, "session-a", false, 204)
	hook(eventID, "participant_joined", owner.Subject, "session-a", false, 204)
	var joined bool
	if err = pool.QueryRow(ctx, "SELECT joined FROM meeting_participant WHERE meeting_id=$1 AND subject=$2", meeting.ID, owner.Subject).Scan(&joined); err != nil || !joined {
		t.Fatal("verified join was not stored")
	}
	hook(uuid.NewString(), "participant_left", owner.Subject, "older-session", false, 204)
	if err = pool.QueryRow(ctx, "SELECT joined FROM meeting_participant WHERE meeting_id=$1 AND subject=$2", meeting.ID, owner.Subject).Scan(&joined); err != nil || !joined {
		t.Fatal("old leave disconnected a newer session")
	}
	login(guestEmail)
	status("DELETE", path, nil, nil, 403)
	status("POST", path+"/join", nil, &access, 200)
	if _, err = pool.Exec(ctx, "DELETE FROM auth_member WHERE subject=$1 AND scope=$2", guest.Subject, w.ID); err != nil {
		t.Fatal(err)
	}
	status("POST", path+"/join", nil, nil, 404)
	hook(uuid.NewString(), "participant_joined", guest.Subject, "revoked-session", false, 204)
	if removed.Load() != 1 {
		t.Fatal("revoked participant was not removed")
	}
	login(email)
	status("DELETE", path, nil, &meeting, 200)
	status("POST", path+"/join", nil, nil, 410)
}
