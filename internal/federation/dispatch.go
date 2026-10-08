package federation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/smip"
	"thura/internal/workspace"
	"thura/schema"
)

const DispatchJob = "thura.smip.dispatch"
const ReconcileJob = "thura.smip.reconcile"
const MaxSendAttempts = 32

func saveOutbound(ctx context.Context, queries *q.Queries, r q.SmipOutbox) error {
	return queries.UpdateSmipOutbound(ctx, q.UpdateSmipOutboundParams{WorkspaceID: r.WorkspaceID, ID: r.ID, State: r.State, Reason: r.Reason, Attempts: r.Attempts, AttemptID: r.AttemptID, LastStatus: r.LastStatus, NextAttempt: r.NextAttempt, Receipt: r.Receipt, ReceiptKey: r.ReceiptKey, UpdatedAt: lidza.Now(ctx)})
}
func backoff(attempt int32) time.Duration {
	cap := int64(300)
	if attempt < 9 {
		cap = 1 << attempt
	}
	return time.Duration(cap/2+rand.Int64N(cap-cap/2+1)) * time.Second
}
func Dispatch(ctx context.Context, raw json.RawMessage) error {
	var in schema.SmipDispatchJob
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if !workspace.ValidID(in.ID) {
		return errors.New("invalid SMIP dispatch ID")
	}
	pool := db.From(ctx)
	initial, err := q.New(pool).FindSmipOutbound(ctx, in.ID)
	if err != nil {
		return err
	}
	if initial.Origin != From(ctx).domain {
		return nil
	}
	tx, queries, row, err := lockOutbound(ctx, initial.WorkspaceID, in.ID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if row.State != smip.Pending && row.State != smip.Uncertain {
		return nil
	}
	now := lidza.Now(ctx)
	if now.Before(row.NextAttempt) {
		return nil
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	g := From(ctx)
	p, packetErr := decodeOutbound(row)
	reason := ""
	member, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: row.WorkspaceID, Subject: row.Subject})
	if err != nil {
		return err
	}
	binding, err := queries.GetSmipBinding(ctx, q.GetSmipBindingParams{WorkspaceID: row.WorkspaceID, ID: row.BindingID})
	if err != nil {
		return err
	}
	if !member {
		reason = "membership_revoked"
	} else if !binding.Enabled {
		reason = "binding_disabled"
	} else if packetErr != nil {
		reason = "invalid_packet"
	} else if p.Envelope.From != g.domain || p.Envelope.To != binding.Peer || p.Envelope.Stream != binding.Stream || p.Envelope.Sender != binding.Recipient || p.Envelope.Recipient != binding.Sender {
		reason = "binding_changed"
	} else if key, ok := g.originKeys[p.Envelope.KeyID]; !ok || p.Verify(key) != nil {
		reason = "origin_key_unavailable"
	} else if g.origins[binding.Peer] == "" {
		reason = "peer_unconfigured"
	} else if row.Attempts >= MaxSendAttempts {
		reason = "retry_budget_exhausted"
	}
	if reason != "" {
		row.State = smip.Blocked
		row.Reason = reason
		if err = saveOutbound(ctx, queries, row); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if row.Attempts == 0 && p.Envelope.Expires <= now.Unix() {
		row.State = smip.Expired
		row.Reason = "expired_unsent"
		if err = saveOutbound(ctx, queries, row); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	client, err := g.client(binding.Peer)
	if err != nil {
		return err
	}
	defer client.Close()
	row.State = smip.Uncertain
	row.Reason = "attempt_in_progress"
	row.Attempts++
	row.AttemptID = uuid.NewString()
	row.LastStatus = 0
	// Keep a crash-recovered claim beyond the client's maximum HTTP lifetime.
	row.NextAttempt = now.Add(time.Minute)
	if err = saveOutbound(ctx, queries, row); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	receipt, sendErr := client.Send(ctx, p)
	resultTx, resultQueries, latest, err := lockOutbound(ctx, row.WorkspaceID, row.ID)
	if err != nil {
		return err
	}
	defer resultTx.Rollback(ctx)
	if latest.AttemptID != row.AttemptID || latest.State != smip.Uncertain {
		return errors.New("SMIP attempt was superseded")
	}
	latest.NextAttempt = lidza.Now(ctx).Add(backoff(row.Attempts))
	if sendErr == nil {
		peer, known := g.peers[p.Envelope.To][receipt.KeyID]
		if !known || receipt.Verify(p, peer) != nil {
			return errors.New("unverified SMIP receipt")
		}
		receiptJSON, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		keyJSON, err := json.Marshal(peer)
		if err != nil {
			return err
		}
		latest.State = smip.Accepted
		latest.Reason = "verified_receipt"
		latest.Receipt = string(receiptJSON)
		latest.ReceiptKey = string(keyJSON)
	} else {
		latest.Reason = "transport_uncertain"
		var status *smip.HTTPError
		if errors.As(sendErr, &status) {
			latest.LastStatus = int32(status.Status)
			latest.Reason = fmt.Sprintf("peer_http_%d", status.Status)
			if !status.Retryable {
				latest.State = smip.Blocked
			}
		}
	}
	if err = saveOutbound(ctx, resultQueries, latest); err != nil {
		return err
	}
	if err = resultTx.Commit(ctx); err != nil {
		return err
	}
	// Reconciliation schedules due uncertain records independently of the jobs
	// pack's attempt budget. Never create a new packet or another transport.
	return nil
}
func Reconcile(ctx context.Context, _ json.RawMessage) error {
	if From(ctx).domain == "" {
		return nil
	}
	rows, err := q.New(db.From(ctx)).DueSmipOutbound(ctx, q.DueSmipOutboundParams{Origin: From(ctx).domain, NextAttempt: lidza.Now(ctx)})
	if err != nil {
		return err
	}
	for _, id := range rows {
		if _, err = jobs.From(ctx).Enqueue(ctx, From(ctx).DispatchKind(), schema.SmipDispatchJob{ID: id}, jobs.Unique(id)); err != nil {
			return err
		}
	}
	return nil
}
func Resume(ctx context.Context, w, id string) (schema.SmipOutbound, error) {
	tx, queries, err := lockAccess(ctx, w, true)
	if err != nil {
		return schema.SmipOutbound{}, err
	}
	defer tx.Rollback(ctx)
	if !workspace.ValidID(id) {
		return schema.SmipOutbound{}, router.Errorf(404, "outbound packet not found")
	}
	row, err := queries.LockSmipOutbound(ctx, q.LockSmipOutboundParams{WorkspaceID: w, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return schema.SmipOutbound{}, router.Errorf(404, "outbound packet not found")
	}
	if err != nil {
		return schema.SmipOutbound{}, err
	}
	if row.State != smip.Blocked {
		return schema.SmipOutbound{}, router.Errorf(409, "only blocked packets can be resumed")
	}
	if row.Attempts >= MaxSendAttempts {
		return schema.SmipOutbound{}, router.Errorf(409, "SMIP retry budget is exhausted; reconcile with the peer")
	}
	row.State = smip.Pending
	if row.Attempts > 0 {
		row.State = smip.Uncertain
	}
	row.Reason = "operator_resumed"
	row.NextAttempt = lidza.Now(ctx)
	if err = saveOutbound(ctx, queries, row); err != nil {
		return schema.SmipOutbound{}, err
	}
	if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.message.resume", Scope: w, Resource: "smip-outbox/" + row.ID}); err != nil {
		return schema.SmipOutbound{}, err
	}
	if _, err = jobs.From(ctx).EnqueueTx(ctx, tx, From(ctx).DispatchKind(), schema.SmipDispatchJob{ID: row.ID}, jobs.Unique(row.ID)); err != nil {
		return schema.SmipOutbound{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return schema.SmipOutbound{}, err
	}
	return outboundView(row), nil
}

func lockOutbound(ctx context.Context, w, id string) (pgx.Tx, *q.Queries, q.SmipOutbox, error) {
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return nil, nil, q.SmipOutbox{}, err
	}
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return nil, nil, q.SmipOutbox{}, errors.Join(err, tx.Rollback(ctx))
	}
	row, err := queries.LockSmipOutbound(ctx, q.LockSmipOutboundParams{WorkspaceID: w, ID: id})
	if err != nil {
		return nil, nil, q.SmipOutbox{}, errors.Join(err, tx.Rollback(ctx))
	}
	return tx, queries, row, nil
}
