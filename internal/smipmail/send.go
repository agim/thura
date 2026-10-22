package smipmail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/smip"
)

func packetFor(row q.MailTransmission, delivery q.MailDelivery) (smip.Packet, error) {
	var packet smip.Packet
	var originKey smip.Key
	snapshot, err := readFrozen(row)
	if err != nil {
		return packet, err
	}
	if json.Unmarshal([]byte(delivery.Packet), &packet) != nil || json.Unmarshal([]byte(row.OriginKey), &originKey) != nil {
		return packet, errors.New("invalid stored mail signature")
	}
	payload, err := json.Marshal(snapshot.Payload)
	if err != nil {
		return packet, err
	}
	if packet.Envelope.Payload != "" {
		return packet, errors.New("stored mail signature must reference the shared snapshot")
	}
	packet.Envelope.Payload = smip.Encode(payload)
	e := packet.Envelope
	if e.ID != delivery.ID || e.From != row.Origin || e.To != delivery.Domain || e.Recipient != delivery.Recipient || e.Created != snapshot.Payload.Created || e.Version != smip.MailVersion || packet.Verify(originKey) != nil {
		return packet, errors.New("mail snapshot integrity check failed")
	}
	return packet, nil
}
func (g *Gateway) hold(ctx context.Context, initial q.MailTransmission, delivery q.MailDelivery, reason string, blocked bool) error {
	tx, queries, row, err := lock(ctx, initial.ID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := queries.ListMailDeliveries(ctx, row.ID)
	if err != nil {
		return err
	}
	current, err := selected(rows, delivery.ID)
	if err != nil {
		return err
	}
	if current.State == "accepted" || current.State == "blocked" {
		return nil
	}
	current.Reason = reason
	current.NextAttempt = lidza.Now(ctx).Add(30 * time.Second)
	if blocked {
		current.State = "blocked"
	}
	if err = saveDelivery(ctx, queries, current); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (g *Gateway) sendSecure(ctx context.Context, initial q.MailTransmission, delivery q.MailDelivery) error {
	if g.providerOnly {
		return g.hold(ctx, initial, delivery, "secure_transport_disabled_no_fallback", false)
	}
	if delivery.State == "accepted" || delivery.State == "blocked" || lidza.Now(ctx).Before(delivery.NextAttempt) {
		return nil
	}
	packet, err := packetFor(initial, delivery)
	if err != nil {
		return g.hold(ctx, initial, delivery, "invalid_immutable_packet", true)
	}
	peer, err := g.Resolve(ctx, delivery.Domain, false)
	if err != nil {
		return g.hold(ctx, initial, delivery, "secure_domain_unavailable_no_fallback", false)
	}
	var frozenPeer Peer
	if json.Unmarshal([]byte(delivery.PeerDocument), &frozenPeer) != nil || frozenPeer.Domain != delivery.Domain || peer.Origin != frozenPeer.Origin {
		return g.hold(ctx, initial, delivery, "secure_endpoint_changed_no_fallback", true)
	}
	origin, known := g.keys[packet.Envelope.KeyID]
	if !known || packet.Verify(origin) != nil {
		return g.hold(ctx, initial, delivery, "origin_key_unavailable", true)
	}
	raw, err := json.Marshal(packet)
	if err != nil {
		return err
	}
	origins, err := g.pinnedOrigins(ctx)
	if err != nil {
		return g.hold(ctx, initial, delivery, "secure_connection_unavailable_no_fallback", false)
	}
	_, pinned := origins[delivery.Domain]
	client, err := g.client(ctx, peer.Origin, pinned)
	if err != nil {
		return g.hold(ctx, initial, delivery, "secure_connection_unavailable_no_fallback", false)
	}
	defer client.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, peer.Origin+smip.MailMessagePath, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Thura-SMIP-Domain", delivery.Domain)
	tx, queries, row, err := lock(ctx, initial.ID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := queries.ListMailDeliveries(ctx, row.ID)
	if err != nil {
		return err
	}
	current, err := selected(rows, delivery.ID)
	if err != nil {
		return err
	}
	now := lidza.Now(ctx)
	if current.State == "accepted" || current.State == "blocked" || now.Before(current.NextAttempt) {
		return nil
	}
	allowed, err := eligible(ctx, queries, row)
	if err != nil {
		return err
	}
	if !allowed {
		current.State = "blocked"
		current.Reason = "sender_authorization_revoked"
		if err = saveDelivery(ctx, queries, current); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if current.Attempts >= MaxAttempts {
		current.State = "blocked"
		current.Reason = "retry_budget_exhausted"
		if err = saveDelivery(ctx, queries, current); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if current.Attempts == 0 && packet.Envelope.Expires <= now.Unix() {
		current.State = "blocked"
		current.Reason = "expired_before_first_attempt"
		if err = saveDelivery(ctx, queries, current); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	current.State = "uncertain"
	current.Reason = "secure_attempt_in_progress"
	current.Attempts++
	current.AttemptID = deliveryID()
	current.NextAttempt = now.Add(time.Minute)
	if err = saveDelivery(ctx, queries, current); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	// Only this exact signed packet is retried. No provider call exists on this path.
	receipt, status, sendErr := sendPacket(client, request, packet, peer.Document.Keys, lidza.Now(ctx))
	finish, queries, latest, err := lock(ctx, row.ID)
	if err != nil {
		return err
	}
	defer finish.Rollback(ctx)
	rows, err = queries.ListMailDeliveries(ctx, latest.ID)
	if err != nil {
		return err
	}
	result, err := selected(rows, current.ID)
	if err != nil {
		return err
	}
	if result.AttemptID != current.AttemptID || result.State != "uncertain" {
		return errors.New("secure mail attempt was superseded")
	}
	result.Reason = "secure_outcome_uncertain_no_fallback"
	result.NextAttempt = lidza.Now(ctx).Add(time.Minute)
	var backoff *peerBackoff
	if errors.As(sendErr, &backoff) {
		result.NextAttempt = lidza.Now(ctx).Add(backoff.Delay)
	}
	if sendErr == nil {
		body, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		result.State, result.Reason, result.Receipt = "accepted", "verified_durable_receipt", string(body)
	} else if status >= 400 && status < 500 && status != http.StatusTooManyRequests {
		result.State = "blocked"
		result.Reason = "secure_peer_rejected_no_fallback"
	}
	if err = saveDelivery(ctx, queries, result); err != nil {
		return err
	}
	return finish.Commit(ctx)
}
func sendPacket(client *http.Client, request *http.Request, packet smip.Packet, keys map[string]smip.Key, now time.Time) (smip.Receipt, int, error) {
	response, err := client.Do(request)
	if err != nil {
		return smip.Receipt{}, 0, err
	}
	defer response.Body.Close()
	status := response.StatusCode
	if status != http.StatusOK && status != http.StatusCreated {
		if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
			seconds, err := strconv.Atoi(response.Header.Get("Retry-After"))
			if err == nil && seconds > 0 && seconds <= 3600 {
				return smip.Receipt{}, status, &peerBackoff{Delay: time.Duration(seconds) * time.Second}
			}
		}
		return smip.Receipt{}, status, errors.New("secure peer did not acknowledge delivery")
	}
	if response.Header.Get("Content-Type") != "application/json" {
		return smip.Receipt{}, status, errors.New("invalid secure receipt media type")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(raw) > 4096 {
		return smip.Receipt{}, status, errors.New("invalid receipt size")
	}
	receipt, err := smip.DecodeReceipt(raw)
	if err != nil {
		return receipt, status, err
	}
	key, known := keys[receipt.KeyID]
	if !known || receipt.Accepted > now.Unix()+120 || receipt.Verify(packet, key) != nil {
		return receipt, status, errors.New("unverified secure receipt")
	}
	return receipt, status, nil
}
func Deliveries(ctx context.Context, item string) ([]q.MailDelivery, error) {
	queries := q.New(db.From(ctx))
	row, err := queries.GetMailTransmission(ctx, item)
	if errors.Is(err, pgx.ErrNoRows) {
		return []q.MailDelivery{}, nil
	}
	if err != nil {
		return nil, err
	}
	return queries.ListMailDeliveries(ctx, row.ID)
}

type peerBackoff struct{ Delay time.Duration }

func (e *peerBackoff) Error() string { return "secure peer requested temporary backoff" }
