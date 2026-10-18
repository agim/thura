package meet

import (
	"context"
	"encoding/json"
	"github.com/agim/lidza/packs/db"
	q "thura/db/queries/gen"
	provider "thura/internal/providers/livekit"
)

const ReconcileJob = "thura.meet.membership"

func Reconcile(ctx context.Context, _ json.RawMessage) error {
	queries := q.New(db.From(ctx))
	rows, err := queries.ListRevokedMeetingParticipants(ctx)
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		cfg, err := provider.Load()
		if err != nil {
			return err
		}
		for _, p := range rows {
			allowed, xerr := queries.MeetingParticipantAllowed(ctx, q.MeetingParticipantAllowedParams{MeetingID: p.MeetingID, Subject: p.Subject})
			if xerr != nil {
				return xerr
			}
			if allowed {
				continue
			}

			if err = cfg.Call(ctx, "RemoveParticipant", Room(p.MeetingID), map[string]string{"room": Room(p.MeetingID), "identity": p.Subject}, nil); err != nil {
				return err
			}
			if err = queries.ClearMeetingParticipant(ctx, p.ID); err != nil {
				return err
			}
		}
	}
	return queries.ExpireMeetingWebhooks(ctx)
}
