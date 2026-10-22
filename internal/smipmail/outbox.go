package smipmail

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/packs/mail"
	"github.com/agim/lidza/pkg/credentials"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/mailaccess"
	"thura/internal/smip"
)

const DispatchJob = "thura.smip-mail.dispatch"
const ReconcileJob = "thura.smip-mail.reconcile"
const MaxAttempts = 32

type frozen struct {
	Payload Payload  `json:"payload"`
	Blind   []string `json:"blind,omitempty"`
}

func Prepare(ctx context.Context, tx pgx.Tx, mailbox q.Mailbox, item q.MailItem, msg mail.Message, cfg mail.Config) (q.MailTransmission, error) {
	_, senderDomain, _ := strings.Cut(strings.ToLower(mailbox.Address), "@")
	g, resolveErr := forDomain(ctx, mailbox.WorkspaceID, senderDomain)
	if resolveErr != nil {
		return q.MailTransmission{}, resolveErr
	}
	queries := q.New(tx)
	if _, err := queries.LockWorkspace(ctx, mailbox.WorkspaceID); err != nil {
		return q.MailTransmission{}, err
	}
	allowed, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: mailbox.WorkspaceID, Subject: item.AuthorID})
	if err != nil || !allowed {
		return q.MailTransmission{}, errors.New("mail sender is no longer a workspace member")
	}
	if err = mailaccess.Check(ctx, queries, mailbox, item.AuthorID); err != nil {
		return q.MailTransmission{}, err
	}
	addresses, err := recipients(msg)
	if err != nil {
		return q.MailTransmission{}, err
	}
	sender, err := plainAddress(msg.From)
	if err != nil {
		return q.MailTransmission{}, err
	}
	if !smip.MailAddressAt(sender, g.domain) {
		return q.MailTransmission{}, errors.New("mailbox sender does not match the enabled SMIP mail domain")
	}
	now := lidza.Now(ctx)
	snapshot := frozen{Payload: Payload{MessageID: "<" + item.ID + "@" + g.domain + ">", Created: now.Unix(), Message: msg}, Blind: msg.Bcc}
	snapshot.Payload.Message.Bcc = nil
	if err = snapshot.Payload.Validate(); err != nil {
		return q.MailTransmission{}, err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil || len(payload) > smip.MaxPayload {
		return q.MailTransmission{}, errors.New("mail snapshot exceeds SMIP limits")
	}
	usage, err := queries.MailTransmissionUsage(ctx, mailbox.WorkspaceID)
	if err != nil {
		return q.MailTransmission{}, err
	}
	if usage.Records >= 1000 || usage.Bytes+int64(len(payload)) > 128<<20 {
		return q.MailTransmission{}, errors.New("mail delivery retention capacity reached")
	}
	key, err := credentials.Key(".")
	if err != nil {
		return q.MailTransmission{}, err
	}
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return q.MailTransmission{}, err
	}
	sealed, err := credentials.Encrypt(key, configJSON)
	if err != nil {
		return q.MailTransmission{}, err
	}
	public := g.keys[smip.KeyID(g.signer.Private.Public().(ed25519.PublicKey))]
	originKey, err := json.Marshal(public)
	if err != nil {
		return q.MailTransmission{}, err
	}
	row, err := queries.AddMailTransmission(ctx, q.AddMailTransmissionParams{Origin: g.domain, WorkspaceID: mailbox.WorkspaceID, MailboxID: mailbox.ID, ItemID: item.ID, AuthorID: item.AuthorID, Payload: string(payload), ProviderConfig: sealed, OriginKey: string(originKey), CreatedAt: now})
	if err != nil {
		return row, err
	}
	for _, recipient := range addresses {
		_, domain, _ := strings.Cut(recipient, "@")
		if err = queries.AddMailDelivery(ctx, q.AddMailDeliveryParams{TransmissionID: row.ID, Recipient: recipient, Domain: domain, NextAttempt: now}); err != nil {
			return row, err
		}
	}
	if err = queries.SetMailMessageIdentity(ctx, q.SetMailMessageIdentityParams{ID: item.ID, MessageID: snapshot.Payload.MessageID}); err != nil {
		return row, err
	}
	return row, nil
}
func readFrozen(row q.MailTransmission) (frozen, error) {
	var snapshot frozen
	if json.Unmarshal([]byte(row.Payload), &snapshot) != nil || snapshot.Payload.Validate() != nil {
		return snapshot, errors.New("invalid immutable mail snapshot")
	}
	return snapshot, nil
}
func saveTransmission(ctx context.Context, queries *q.Queries, row q.MailTransmission) error {
	return queries.SaveMailTransmission(ctx, q.SaveMailTransmissionParams{ID: row.ID, State: row.State, APIState: row.APIState, APIAttemptID: row.APIAttemptID, APIResult: row.APIResult, UpdatedAt: lidza.Now(ctx)})
}
func saveDelivery(ctx context.Context, queries *q.Queries, row q.MailDelivery) error {
	return queries.SaveMailDelivery(ctx, q.SaveMailDeliveryParams{ID: row.ID, Route: row.Route, State: row.State, Reason: row.Reason, Packet: row.Packet, PeerDocument: row.PeerDocument, Receipt: row.Receipt, Attempts: row.Attempts, AttemptID: row.AttemptID, NextAttempt: row.NextAttempt, UpdatedAt: lidza.Now(ctx)})
}
func lock(ctx context.Context, id string) (pgx.Tx, *q.Queries, q.MailTransmission, error) {
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return nil, nil, q.MailTransmission{}, err
	}
	queries := q.New(tx)
	row, err := queries.FindMailTransmission(ctx, id)
	if err == nil {
		_, err = queries.LockWorkspace(ctx, row.WorkspaceID)
	}
	if err == nil {
		row, err = queries.LockMailTransmission(ctx, id)
	}
	if err != nil {
		return nil, nil, row, errors.Join(err, tx.Rollback(ctx))
	}
	return tx, queries, row, nil
}
func selected(rows []q.MailDelivery, id string) (q.MailDelivery, error) {
	for _, row := range rows {
		if row.ID == id {
			return row, nil
		}
	}
	return q.MailDelivery{}, errors.New("mail recipient disappeared")
}
func (g *Gateway) plan(ctx context.Context, initial q.MailTransmission) error {
	queries := q.New(db.From(ctx))
	rows, err := queries.ListMailDeliveries(ctx, initial.ID)
	if err != nil {
		return err
	}
	snapshot, err := readFrozen(initial)
	if err != nil {
		return err
	}
	wire, err := json.Marshal(snapshot.Payload)
	if err != nil {
		return err
	}
	for _, candidate := range rows {
		if candidate.Route != "pending" || lidza.Now(ctx).Before(candidate.NextAttempt) {
			continue
		}
		var peer Peer
		resolveErr := ErrUnsupported
		if !g.providerOnly {
			peer, resolveErr = g.Resolve(ctx, candidate.Domain, false)
		}
		tx, queries, row, err := lock(ctx, initial.ID)
		if err != nil {
			return err
		}
		recipients, err := queries.ListMailDeliveries(ctx, row.ID)
		if err != nil {
			return errors.Join(err, tx.Rollback(ctx))
		}
		current, err := selected(recipients, candidate.ID)
		if err != nil {
			return errors.Join(err, tx.Rollback(ctx))
		}
		if current.Route != "pending" {
			if err = tx.Rollback(ctx); err != nil {
				return err
			}
			continue
		}
		current.Reason = "domain_verification_unavailable"
		current.NextAttempt = lidza.Now(ctx).Add(30 * time.Second)
		if errors.Is(resolveErr, ErrRegistryFull) {
			// A full registry is an operator condition, not a peer outage: hold
			// the recipient under its own reason without a 30-second retry storm.
			// The provider is never selected for an unverified domain.
			current.Reason = "registry_capacity"
			current.NextAttempt = lidza.Now(ctx).Add(5 * time.Minute)
		}
		if resolveErr == nil {
			sender, err := plainAddress(snapshot.Payload.Message.From)
			if err != nil {
				return errors.Join(err, tx.Rollback(ctx))
			}
			packet, err := smip.Sign(smip.Envelope{Version: smip.MailVersion, ID: current.ID, From: g.domain, To: current.Domain, Sender: sender, Recipient: current.Recipient, Stream: "mail", Kind: "mail", Created: snapshot.Payload.Created, Expires: snapshot.Payload.Created + int64(smip.MaxLifetime/time.Second), Payload: smip.Encode(wire)}, g.signer.Private)
			if err != nil {
				return errors.Join(err, tx.Rollback(ctx))
			}
			// Payload bytes are shared by all recipients; signatures and all identities
			// remain immutable per recipient without duplicating attachments in Postgres.
			packet.Envelope.Payload = ""
			encoded, err := json.Marshal(packet)
			if err != nil {
				return errors.Join(err, tx.Rollback(ctx))
			}
			document, err := json.Marshal(peer)
			if err != nil {
				return errors.Join(err, tx.Rollback(ctx))
			}
			current.Route, current.Packet, current.PeerDocument, current.Reason = "smip", string(encoded), string(document), "verified_domain"
			current.NextAttempt = lidza.Now(ctx)
		} else if errors.Is(resolveErr, ErrUnsupported) {
			if g.providerOnly {
				if err = queries.EnsureTeamProviderPolicy(ctx, q.EnsureTeamProviderPolicyParams{Origin: g.domain, Domain: current.Domain}); err != nil {
					return errors.Join(err, tx.Rollback(ctx))
				}
			}
			capability, err := queries.LockSmipMailDomain(ctx, q.LockSmipMailDomainParams{Origin: g.domain, Domain: current.Domain})
			if err != nil {
				return errors.Join(err, tx.Rollback(ctx))
			}
			if !capability.EverVerified && !g.required[current.Domain] {
				current.Route, current.Reason = "provider", "no_smip_capability"
				current.NextAttempt = lidza.Now(ctx)
			}
		}
		if err = saveDelivery(ctx, queries, current); err == nil {
			err = tx.Commit(ctx)
		} else {
			err = errors.Join(err, tx.Rollback(ctx))
		}
		if err != nil {
			return err
		}
	}
	tx, queries, row, err := lock(ctx, initial.ID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err = queries.ListMailDeliveries(ctx, row.ID)
	if err != nil {
		return err
	}
	all := true
	for _, r := range rows {
		all = all && r.Route != "pending"
	}
	if all && row.State == "planning" {
		row.State = "pending"
	}
	if err = saveTransmission(ctx, queries, row); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func eligible(ctx context.Context, queries *q.Queries, row q.MailTransmission) (bool, error) {
	g, identityErr := forDomain(ctx, row.WorkspaceID, row.Origin)
	if identityErr != nil || !g.Enabled() || row.Origin != g.domain {
		return false, nil
	}
	box, err := queries.GetMailbox(ctx, row.MailboxID)
	if err != nil {
		return false, err
	}
	if !mailaccess.Owns(box, row.AuthorID) {
		return false, nil
	}
	return queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: row.WorkspaceID, Subject: row.AuthorID})
}
func Dispatch(ctx context.Context, raw json.RawMessage) error {
	var input struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &input) != nil {
		return errors.New("invalid mail delivery job")
	}
	row, err := q.New(db.From(ctx)).FindMailTransmission(ctx, input.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.State == "accepted" || row.State == "blocked" {
		return nil
	}
	g, identityErr := forDomain(ctx, row.WorkspaceID, row.Origin)
	if identityErr != nil || !g.Enabled() || row.Origin != g.domain {
		return nil
	} // Disabling never enters the old provider sender.
	if row.State == "planning" {
		if err = g.plan(ctx, row); err != nil {
			return err
		}
	}
	row, err = q.New(db.From(ctx)).FindMailTransmission(ctx, row.ID)
	if err != nil {
		return err
	}
	if row.State == "pending" {
		if err = g.sendProvider(ctx, row); err != nil {
			return err
		}
		recipients, err := q.New(db.From(ctx)).ListMailDeliveries(ctx, row.ID)
		if err != nil {
			return err
		}
		for _, recipient := range recipients {
			if recipient.Route == "smip" {
				if err = g.sendSecure(ctx, row, recipient); err != nil {
					return err
				}
			}
		}
	}
	return summarize(ctx, row.ID)
}
func Job(id string) json.RawMessage {
	b, err := json.Marshal(map[string]string{"id": id})
	if err != nil {
		panic(err)
	}
	return b
}
func Reconcile(ctx context.Context, _ json.RawMessage) error {
	rows, err := q.New(db.From(ctx)).DueAllMailTransmissions(ctx)
	if err != nil {
		return err
	}
	for _, id := range rows {
		if _, err = jobs.From(ctx).Enqueue(ctx, DispatchJob, struct {
			ID string `json:"id"`
		}{id}, jobs.Unique(id)); err != nil {
			return err
		}
	}
	return nil
}
func filterProvider(msg mail.Message, rows []q.MailDelivery) mail.Message {
	allowed := map[string]bool{}
	for _, row := range rows {
		if row.Route == "provider" {
			allowed[strings.ToLower(row.Recipient)] = true
		}
	}
	filter := func(group string) []string {
		if group == "" {
			return nil
		}
		addresses, err := stdAddresses(group)
		if err != nil {
			return nil
		}
		result := []string{}
		for _, address := range addresses {
			if allowed[strings.ToLower(address)] {
				result = append(result, address)
				delete(allowed, strings.ToLower(address))
			}
		}
		return result
	}
	// One provider submission contains only its selected envelope recipients;
	// secure recipients cannot accidentally be sent again through the provider.
	to := filter(msg.To)
	cc := filter(strings.Join(msg.Cc, ","))
	bcc := filter(strings.Join(msg.Bcc, ","))
	msg.To = strings.Join(to, ", ")
	msg.Cc = cc
	msg.Bcc = bcc
	return msg
}
func stdAddresses(raw string) ([]string, error) { m := mail.Message{To: raw}; return recipients(m) }
func (g *Gateway) sendProvider(ctx context.Context, initial q.MailTransmission) error {
	for {
		sent, err := g.sendProviderGroup(ctx, initial)
		if err != nil || !sent {
			return err
		}
	}
}
func (g *Gateway) sendProviderGroup(ctx context.Context, initial q.MailTransmission) (bool, error) {
	tx, queries, row, err := lock(ctx, initial.ID)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	recipients, err := queries.ListMailDeliveries(ctx, row.ID)
	if err != nil {
		return false, err
	}
	var group []q.MailDelivery
	for _, recipient := range recipients {
		if recipient.Route == "provider" && recipient.State == "pending" && recipient.Attempts == 0 {
			group = append(group, recipient)
		}
	}
	if len(group) == 0 {
		return false, nil
	}
	// Fence a newly learned secure requirement or operator policy tightening
	// against the provider-attempt commit. Never rewrite a frozen route.
	sort.Slice(group, func(i, j int) bool { return group[i].Domain < group[j].Domain })
	permitted := group[:0]
	for _, recipient := range group {

		capability, err := queries.LockSmipMailDomain(ctx, q.LockSmipMailDomainParams{Origin: g.domain, Domain: recipient.Domain})
		if err != nil {
			return false, err
		}
		if capability.EverVerified || g.required[recipient.Domain] || g.denied[recipient.Domain] {
			recipient.State = "blocked"
			recipient.Reason = "secure_policy_changed_before_provider_attempt"
			if err = saveDelivery(ctx, queries, recipient); err != nil {
				return false, err
			}
		} else {
			permitted = append(permitted, recipient)
		}
	}
	group = permitted
	if len(group) == 0 {
		return false, tx.Commit(ctx)
	}
	allowed, err := eligible(ctx, queries, row)
	if err != nil {
		return false, err
	}
	if !allowed {
		for _, recipient := range recipients {
			if recipient.State != "accepted" && recipient.State != "captured" {
				recipient.State = "blocked"
				recipient.Reason = "sender_authorization_revoked"
				if err = saveDelivery(ctx, queries, recipient); err != nil {
					return false, err
				}
			}
		}
		row.State = "blocked"
		if err = saveTransmission(ctx, queries, row); err != nil {
			return false, err
		}
		return false, tx.Commit(ctx)
	}
	snapshot, err := readFrozen(row)
	if err != nil {
		return false, err
	}
	key, err := credentials.Key(".")
	if err != nil {
		return false, err
	}
	plain, err := credentials.Decrypt(key, row.ProviderConfig)
	if err != nil {
		return false, err
	}
	var cfg mail.Config
	if json.Unmarshal(plain, &cfg) != nil {
		return false, errors.New("invalid saved provider configuration")
	}
	transport, err := mail.New(cfg, nil, nil)
	if err != nil {
		return false, err
	}
	message := snapshot.Payload.Message
	message.Bcc = snapshot.Blind
	message = filterProvider(message, group)
	// The official pack requires a To recipient. Promote a visible Cc if needed.
	// With only blind recipients, submit one recipient per group: no blind address
	// can be exposed to another recipient and no secure recipient is added back.
	if message.To == "" && len(message.Cc) > 0 {
		message.To = message.Cc[0]
		message.Cc = message.Cc[1:]
	}
	if message.To == "" && len(message.Bcc) > 0 {
		address := message.Bcc[0]
		for _, recipient := range group {
			if strings.EqualFold(recipient.Recipient, address) {
				group = []q.MailDelivery{recipient}
				break
			}
		}
		message.To = address
		message.Bcc = nil
	}
	// This marker commits BEFORE the one provider call. A failure/timeout/crash
	// remains uncertain, because the pack cannot prove whether an API accepted it.
	row.APIState = "uncertain"
	row.APIAttemptID = deliveryID()
	for i := range group {
		group[i].State = "uncertain"
		group[i].Attempts = 1
		group[i].AttemptID = row.APIAttemptID
		group[i].Reason = "provider_outcome_unknown_no_automatic_retry"
		if err = saveDelivery(ctx, queries, group[i]); err != nil {
			return false, err
		}
	}
	if err = saveTransmission(ctx, queries, row); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	result, sendErr := transport.Send(ProviderContext(ctx), message)
	finish, queries, current, err := lock(ctx, row.ID)
	if err != nil {
		return false, err
	}
	defer finish.Rollback(ctx)
	latest, err := queries.ListMailDeliveries(ctx, row.ID)
	if err != nil {
		return false, err
	}
	outcome := "uncertain"
	if sendErr == nil {
		outcome = "accepted"
		if cfg.Provider == "log" || cfg.Provider == "outbox" {
			outcome = "captured"
		}
		current.APIResult = result
	}
	for _, recipient := range group {
		saved, err := selected(latest, recipient.ID)
		if err != nil {
			return false, err
		}
		if saved.State != "uncertain" || saved.AttemptID != recipient.AttemptID {
			return false, errors.New("provider recipient attempt was superseded")
		}
		saved.State = outcome
		saved.Reason = "provider_accepted"
		if outcome == "uncertain" {
			saved.Reason = "provider_outcome_unknown_no_automatic_retry"
		} else {
			saved.Receipt = result
		}
		if err = saveDelivery(ctx, queries, saved); err != nil {
			return false, err
		}
	}
	latest, err = queries.ListMailDeliveries(ctx, row.ID)
	if err != nil {
		return false, err
	}
	current.APIState = "accepted"
	for _, recipient := range latest {
		if recipient.Route == "provider" && recipient.State == "uncertain" {
			current.APIState = "uncertain"
		}
	}
	if err = saveTransmission(ctx, queries, current); err != nil {
		return false, err
	}
	return true, finish.Commit(ctx)
}
func summarize(ctx context.Context, id string) error {
	tx, queries, row, err := lock(ctx, id)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	recipients, err := queries.ListMailDeliveries(ctx, id)
	if err != nil {
		return err
	}
	pending, uncertain, blocked, captured := false, false, false, false
	for _, recipient := range recipients {
		state := recipient.State
		pending = pending || recipient.Route == "pending" || state == "pending" || recipient.Route == "smip" && state == "uncertain"
		uncertain = uncertain || state == "uncertain"
		blocked = blocked || state == "blocked"
		captured = captured || state == "captured"
	}
	status, folder := "sent", q.MailFolderSent
	if pending {
		status = "delivery_pending"
		folder = q.MailFolderDrafts
		row.State = "pending"
		for _, r := range recipients {
			if r.Route == "pending" {
				row.State = "planning"
			}
		}
	} else if uncertain {
		status = "delivery_uncertain"
		folder = q.MailFolderDrafts
		row.State = "blocked"
	} else if blocked {
		status = "delivery_blocked"
		folder = q.MailFolderDrafts
		row.State = "blocked"
	} else {
		row.State = "accepted"
		if captured {
			status = "captured"
		}
	}
	if err = queries.SetMailOutcome(ctx, q.SetMailOutcomeParams{ID: row.ItemID, Status: status, Folder: folder, ProviderID: row.APIResult, DeliveryReason: ""}); err != nil {
		return err
	}
	if err = saveTransmission(ctx, queries, row); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
