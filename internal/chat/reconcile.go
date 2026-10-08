package chat

import (
	"context"
	"encoding/json"
	"github.com/agim/lidza/packs/db"
	"net/url"
	q "thura/db/queries/gen"
	provider "thura/internal/providers/matrix"
)

const ReconcileJob = "thura.chat.membership"

// Revocation denies all Thura API access immediately. This worker also removes
// Matrix virtual membership; no Matrix credentials were issued to that member.
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
		tx, err := db.From(ctx).Begin(ctx)
		if err != nil {
			return err
		}
		qtx := q.New(tx)
		if _, err = qtx.LockWorkspace(ctx, r.WorkspaceID); err == nil {
			var member bool
			member, err = qtx.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: r.WorkspaceID, Subject: r.Subject})
			if err == nil && !member {
				user := cfg.User(r.Subject)
				var state struct {
					Membership string `json:"membership"`
				}
				err = cfg.Call(ctx, cfg.Bot(), "GET", "/_matrix/client/v3/rooms/"+url.PathEscape(r.MatrixRoomID)+"/state/m.room.member/"+url.PathEscape(user), nil, &state)
				if e, ok := err.(*provider.Error); ok && e.Status == 404 {
					err = nil
					state.Membership = "leave"
				}
				if err == nil && state.Membership != "leave" && state.Membership != "ban" {
					err = cfg.Call(ctx, cfg.Bot(), "POST", "/_matrix/client/v3/rooms/"+url.PathEscape(r.MatrixRoomID)+"/kick", map[string]string{"user_id": user}, nil)
				}
				if err == nil {
					err = qtx.DeleteChatParticipant(ctx, r.ID)
				}
			}
		}
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			rollback := tx.Rollback(ctx)
			if rollback != nil {
				return rollback
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}
