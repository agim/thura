package chat

import (
	"context"
	"encoding/json"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"net/url"
	q "thura/db/queries/gen"
	provider "thura/internal/providers/matrix"
)

const ReconcileJob = "thura.chat.membership"

// Revocation denies all Thura API access immediately. This worker also removes
// Matrix virtual membership; no Matrix credentials were issued to that member.
// Each row stands alone: a Matrix refusal for one (the bot lacks kick power, the
// room is gone) is logged and the next row still runs, since the sweep always
// restarts from the first row a minute later. Matrix is called before the
// workspace lock, which is held only for the membership re-check and the delete.
func Reconcile(ctx context.Context, _ json.RawMessage) error {
	queries := q.New(db.From(ctx))
	rows, err := queries.ListRevokedChatActors(ctx, "")
	if err != nil || len(rows) == 0 {
		return err
	}
	cfg, err := provider.Load()
	if err != nil {
		return err
	}
	for _, r := range rows {
		member, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: r.WorkspaceID, Subject: r.Subject})
		if err != nil {
			return err
		}
		if member {
			continue
		}
		if err = remove(ctx, cfg, r.MatrixRoomID, cfg.User(r.Subject)); err != nil {
			lidza.Log(ctx).Warn("chat membership not revoked on Matrix", "error", err, "workspace", r.WorkspaceID, "room", r.MatrixRoomID, "participant", r.ID)
			continue
		}
		if err = forget(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

// remove kicks user from the room unless Matrix already shows them out of it;
// a kick refused because they left or were banned meanwhile counts the same.
func remove(ctx context.Context, cfg provider.Config, room, user string) error {
	out, err := membership(ctx, cfg, room, user)
	if err != nil || out {
		return err
	}
	kickErr := cfg.Call(ctx, cfg.Bot(), "POST", "/_matrix/client/v3/rooms/"+url.PathEscape(room)+"/kick", map[string]string{"user_id": user}, nil)
	if kickErr == nil {
		return nil
	}
	if out, err = membership(ctx, cfg, room, user); err == nil && out {
		return nil
	}
	return kickErr
}
func membership(ctx context.Context, cfg provider.Config, room, user string) (bool, error) {
	var state struct {
		Membership string `json:"membership"`
	}
	err := cfg.Call(ctx, cfg.Bot(), "GET", "/_matrix/client/v3/rooms/"+url.PathEscape(room)+"/state/m.room.member/"+url.PathEscape(user), nil, &state)
	if e, ok := err.(*provider.Error); ok && e.Status == 404 {
		return true, nil
	}
	return state.Membership == "leave" || state.Membership == "ban", err
}

// forget deletes the participant row under the workspace lock, after checking
// that the member was not restored while Matrix was being called.
func forget(ctx context.Context, r q.ListRevokedChatActorsRow) error {
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := q.New(tx)
	if _, err = qtx.LockWorkspace(ctx, r.WorkspaceID); err != nil {
		return err
	}
	member, err := qtx.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: r.WorkspaceID, Subject: r.Subject})
	if err != nil || member {
		return err
	}
	if err = qtx.DeleteChatParticipant(ctx, r.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
