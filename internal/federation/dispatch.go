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
	"thura/internal/smipmail"
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
	g := From(ctx)
	// One trust read serves the endpoint choice and the receipt verification
	// of this attempt; a peer revoked before it blocks before any network.
	trust, err := g.Trust(ctx)
	if err != nil {
		return err
	}
	// A conversation's partner is discovered before the row lock is taken, so
	// no network fetch happens inside the workspace transaction; the outcome
	// is applied under the lock below.
	var discovered partner
	var discoveryErr error
	if initial.ConversationID != nil {
		peek, err := q.New(pool).GetSmipConversation(ctx, q.GetSmipConversationParams{WorkspaceID: initial.WorkspaceID, ID: *initial.ConversationID})
		if err != nil {
			return err
		}
		discovered, discoveryErr = g.resolvePartner(ctx, &trust, peek.RemoteDomain)
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
	p, packetErr := decodeOutbound(row)
	member, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: row.WorkspaceID, Subject: row.Subject})
	if err != nil {
		return err
	}
	var reason, peer string
	var target partner
	held := false
	if row.ConversationID != nil {
		reason, held, err = authorizeConversation(ctx, queries, row, p, packetErr, member, discovered, discoveryErr)
		target = discovered
	} else {
		reason, peer, err = authorizeBinding(ctx, queries, g, trust, row, p, packetErr, member)
		target = partner{Domain: peer, Keys: trust.Peers[peer], Origin: trust.Origins[peer], Pinned: true}
	}
	if err != nil {
		return err
	}
	if reason == "" && row.Attempts >= MaxSendAttempts {
		reason = "retry_budget_exhausted"
	}
	if held {
		// The partner could not be verified right now (or lost its
		// verification): wait and retry the same packet, never attempting an
		// unverified endpoint. No attempt is recorded, so a packet never
		// attempted still expires.
		if row.Attempts == 0 && p.Envelope.Expires <= now.Unix() {
			row.State = smip.Expired
			reason = "expired_unsent"
		}
		row.Reason = reason
		row.NextAttempt = now.Add(backoff(row.Attempts + 3))
		if err = saveOutbound(ctx, queries, row); err != nil {
			return err
		}
		return tx.Commit(ctx)
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
	client, err := g.partnerClient(ctx, trust, target)
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
		key, known := trust.Peers[p.Envelope.To][receipt.KeyID]
		if !known || receipt.Verify(p, key) != nil {
			return errors.New("unverified SMIP receipt")
		}
		receiptJSON, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		keyJSON, err := json.Marshal(key)
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

// authorizeBinding rechecks a legacy stream send under the workspace lock:
// the requester is still a member and an owner or admin, the binding is
// enabled and unchanged, the policy is not Off for it, the origin key still
// verifies and the pinned peer still has a sending endpoint.
func authorizeBinding(ctx context.Context, queries *q.Queries, g *Gateway, trust Trust, row q.SmipOutbox, p smip.Packet, packetErr error, member bool) (string, string, error) {
	// Queueing is for owners and admins, so a requester demoted to member
	// since queueing is blocked too.
	role := ""
	var err error
	if member {
		if role, err = queries.WorkspaceRole(ctx, q.WorkspaceRoleParams{Scope: row.WorkspaceID, Subject: row.Subject}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", "", err
		}
	}
	binding, err := queries.GetSmipBinding(ctx, q.GetSmipBindingParams{WorkspaceID: row.WorkspaceID, ID: deref(row.BindingID)})
	if err != nil {
		return "", "", err
	}
	policy, err := loadPolicy(ctx, queries, row.WorkspaceID)
	if err != nil {
		return "", "", err
	}
	switch {
	case !member:
		return "membership_revoked", binding.Peer, nil
	case role != "owner" && role != "admin":
		return "role_revoked", binding.Peer, nil
	case !binding.Enabled:
		return "binding_disabled", binding.Peer, nil
	case packetErr != nil:
		return "invalid_packet", binding.Peer, nil
	case p.Envelope.From != g.domain || p.Envelope.To != binding.Peer || p.Envelope.Stream != binding.Stream || p.Envelope.Sender != binding.Recipient || p.Envelope.Recipient != binding.Sender:
		return "binding_changed", binding.Peer, nil
	case !policy.allowsBinding(p.Envelope.Kind, binding.Peer):
		return "policy_refused", binding.Peer, nil
	}
	if key, ok := g.originKeys[p.Envelope.KeyID]; !ok || p.Verify(key) != nil {
		return "origin_key_unavailable", binding.Peer, nil
	}
	if trust.Origins[binding.Peer] == "" {
		return "peer_unconfigured", binding.Peer, nil
	}
	return "", binding.Peer, nil
}

// authorizeConversation rechecks an external conversation send under the
// workspace lock: the requester is still a member who may see the
// conversation, the recipient side has not declined it, the policy still
// allows the kind with that domain, the packet still matches the
// conversation and the partner verifies. held=true means the partner could
// not be verified right now and the packet waits.
func authorizeConversation(ctx context.Context, queries *q.Queries, row q.SmipOutbox, p smip.Packet, packetErr error, member bool, target partner, discoveryErr error) (string, bool, error) {
	g := From(ctx)
	conversation, err := queries.GetSmipConversation(ctx, q.GetSmipConversationParams{WorkspaceID: row.WorkspaceID, ID: deref(row.ConversationID)})
	if err != nil {
		return "", false, err
	}
	policy, err := loadPolicy(ctx, queries, row.WorkspaceID)
	if err != nil {
		return "", false, err
	}
	switch {
	case !member:
		return "membership_revoked", false, nil
	case !visible(conversation, row.Subject):
		return "membership_revoked", false, nil
	case conversation.State != ConversationAccepted:
		return "conversation_closed", false, nil
	case packetErr != nil:
		return "invalid_packet", false, nil
	case p.Envelope.From != g.domain || p.Envelope.To != conversation.RemoteDomain || p.Envelope.Sender != conversation.LocalAddress || p.Envelope.Recipient != conversation.RemoteAddress || p.Envelope.Stream != conversation.Stream:
		return "conversation_changed", false, nil
	case !policy.allows(p.Envelope.Kind, conversation.RemoteDomain):
		return "policy_refused", false, nil
	}
	if key, ok := g.originKeys[p.Envelope.KeyID]; !ok || p.Verify(key) != nil {
		return "origin_key_unavailable", false, nil
	}
	if discoveryErr != nil {
		if errors.Is(discoveryErr, smipmail.ErrUnsupported) {
			return "peer_unsupported", false, nil
		}
		return "peer_unverified", true, nil
	}
	if target.Domain != conversation.RemoteDomain || !hasActiveKey(target.Keys) || target.Origin == "" {
		return "peer_unconfigured", false, nil
	}
	return "", false, nil
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
	tx, queries, row, err := lockManagedOutbound(ctx, w, id)
	if err != nil {
		return schema.SmipOutbound{}, err
	}
	defer tx.Rollback(ctx)
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

// lockManagedOutbound rechecks current management authority under the same
// workspace lock used by dispatch, then locks the workspace-scoped record.
func lockManagedOutbound(ctx context.Context, w, id string) (pgx.Tx, *q.Queries, q.SmipOutbox, error) {
	tx, queries, err := lockAccess(ctx, w, true)
	if err != nil {
		return nil, nil, q.SmipOutbox{}, err
	}
	if !workspace.ValidID(id) {
		return nil, nil, q.SmipOutbox{}, errors.Join(router.Errorf(404, "outbound packet not found"), tx.Rollback(ctx))
	}
	row, err := queries.LockSmipOutbound(ctx, q.LockSmipOutboundParams{WorkspaceID: w, ID: id})
	// A member's conversation send is theirs, not the stream operators'.
	if errors.Is(err, pgx.ErrNoRows) || err == nil && row.BindingID == nil {
		err = router.Errorf(404, "outbound packet not found")
	}
	if err != nil {
		return nil, nil, q.SmipOutbox{}, errors.Join(err, tx.Rollback(ctx))
	}
	return tx, queries, row, nil
}
