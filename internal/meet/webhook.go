package meet

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/webhook"
	"net/http"
	"strings"
	q "thura/db/queries/gen"
	provider "thura/internal/providers/livekit"
	"thura/internal/workspace"
)

func Webhook(w http.ResponseWriter, r *http.Request) {
	cfg, err := provider.Load()
	if err != nil {
		http.Error(w, "Meet service not configured", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	raw, err := webhook.Receive(r, auth.NewSimpleKeyProvider(cfg.Key, cfg.Secret))
	if err != nil {
		http.Error(w, "invalid meeting signature", http.StatusUnauthorized)
		return
	}
	var event struct {
		ID    string `json:"id"`
		Event string `json:"event"`
		Room  *struct {
			Name string `json:"name"`
		} `json:"room"`
		Participant *struct {
			Identity string `json:"identity"`
			Sid      string `json:"sid"`
		} `json:"participant"`
	}
	if json.Unmarshal(raw, &event) != nil || event.ID == "" || len(event.ID) > 300 || event.Room == nil {
		http.Error(w, "invalid meeting event", http.StatusBadRequest)
		return
	}
	if event.Event != "participant_joined" && event.Event != "participant_left" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if event.Participant == nil || event.Participant.Identity == "" || len(event.Participant.Identity) > 200 || event.Participant.Sid == "" {
		http.Error(w, "invalid participant", http.StatusBadRequest)
		return
	}
	id := strings.TrimPrefix(event.Room.Name, "thura_")
	if !strings.HasPrefix(event.Room.Name, "thura_") || !workspace.ValidID(id) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	digest := sha256.Sum256(raw)
	checksum := hex.EncodeToString(digest[:])
	err = applyEvent(r.Context(), cfg, event.ID, checksum, id, event.Event, event.Participant.Identity, event.Participant.Sid)
	if err != nil {
		http.Error(w, "meeting event failed", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func applyEvent(ctx context.Context, cfg provider.Config, eventID, checksum, id, kind, subject, sid string) error {
	queries := q.New(db.From(ctx))
	m, err := queries.FindMeeting(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries = q.New(tx)
	m, err = queries.LockMeeting(ctx, q.LockMeetingParams{WorkspaceID: m.WorkspaceID, ID: id})
	if err != nil {
		return err
	}
	existing, err := queries.FindMeetingWebhook(ctx, eventID)
	if err == nil {
		if existing.Checksum != checksum {
			return router.Errorf(409, "meeting event ID collision")
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if kind == "participant_joined" {
		member, xerr := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: m.WorkspaceID, Subject: subject})
		if xerr != nil {
			return xerr
		}
		_, xerr = queries.GetMeetingParticipant(ctx, q.GetMeetingParticipantParams{MeetingID: id, Subject: subject})
		if xerr != nil && !errors.Is(xerr, pgx.ErrNoRows) {
			return xerr
		}
		if !member || m.EndedAt != nil || errors.Is(xerr, pgx.ErrNoRows) {
			if err = cfg.Call(ctx, "RemoveParticipant", Room(id), map[string]string{"room": Room(id), "identity": subject}, nil); err != nil {
				return err
			}
		} else {
			err = queries.JoinMeetingSession(ctx, q.JoinMeetingSessionParams{MeetingID: id, Subject: subject, SessionID: sid})
		}
	} else {
		err = queries.LeaveMeetingSession(ctx, q.LeaveMeetingSessionParams{MeetingID: id, Subject: subject, SessionID: sid})
	}
	if err != nil {
		return err
	}
	if err = queries.AddMeetingWebhook(ctx, q.AddMeetingWebhookParams{EventID: eventID, Checksum: checksum}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
