package federation

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/smip"
)

const MaxInboxRecords = 1000
const MaxInboxBytes = 64 << 20

// MaxConversations bounds a workspace's external conversations and requests;
// MaxPendingItems bounds what one unanswered request may hold, so an
// unaccepted sender cannot fill a workspace's quota.
const MaxConversations = 1000
const MaxPendingItems = 20

type DatabaseInbox struct {
	ctx     context.Context
	gateway *Gateway
	// peers is the trust snapshot the receiver admitting this packet uses.
	peers map[string]map[string]smip.Key
}

func decode(r q.SmipInbox) (smip.Record, error) {
	var record smip.Record
	var origin, receipt smip.Key
	if json.Unmarshal([]byte(r.Record), &record) != nil || json.Unmarshal([]byte(r.OriginKey), &origin) != nil || json.Unmarshal([]byte(r.ReceiptKey), &receipt) != nil {
		return record, errors.New("corrupt SMIP inbox")
	}
	payload, err := record.Packet.Envelope.Bytes()
	if err != nil || record.Packet.Verify(origin) != nil || record.Receipt.Verify(record.Packet, receipt) != nil || record.Packet.Envelope.From != r.Origin || record.Packet.Envelope.ID != r.MessageID || record.Packet.Digest() != r.Digest || record.Packet.Envelope.Kind != r.Kind || len(payload) != int(r.Size) || record.Receipt.Accepted != r.AcceptedAt.Unix() {
		return record, errors.New("invalid SMIP inbox integrity")
	}
	return record, nil
}
func (s *DatabaseInbox) Get(origin, id string) (smip.Record, bool, error) {
	r, err := q.New(db.From(s.ctx)).FindSmipReceipt(s.ctx, q.FindSmipReceiptParams{Origin: origin, MessageID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return smip.Record{}, false, nil
	}
	if err != nil {
		return smip.Record{}, false, err
	}
	record, err := decode(r)
	return record, err == nil, err
}

// Put admits a verified packet. A legacy stream binding (peer, stream,
// recipient) keeps its workspace-level consent; any other packet goes to
// the local address it names as an external conversation, subject to the
// workspace's sharing policy and its recipient's acceptance.
func (s *DatabaseInbox) Put(record smip.Record) (smip.Record, bool, error) {
	ctx := s.ctx
	e := record.Packet.Envelope
	key, known := s.peers[e.From][e.KeyID]
	receiptKey := smip.Key{Public: s.gateway.signer.Private.Public().(ed25519.PublicKey), NotBefore: s.gateway.signer.NotBefore, NotAfter: s.gateway.signer.NotAfter}
	if !known || record.Packet.Verify(key) != nil || record.Receipt.Verify(record.Packet, receiptKey) != nil || e.To != s.gateway.domain {
		return smip.Record{}, false, smip.ErrAdmissionDenied
	}
	payload, err := e.Bytes()
	if err != nil {
		return smip.Record{}, false, err
	}
	if e.Kind == "file" {
		nameBytes, err := base64.RawURLEncoding.DecodeString(e.Name)
		name := string(nameBytes)
		if err != nil {
			return smip.Record{}, false, smip.ErrAdmissionDenied
		}
		clean, err := drive.Name(name)
		if err != nil || clean != name || len(payload) > drive.MaxSize {
			return smip.Record{}, false, smip.ErrAdmissionDenied
		}
	}
	queries := q.New(db.From(ctx))
	binding, err := queries.FindSmipBinding(ctx, q.FindSmipBindingParams{Peer: e.From, Stream: e.Stream, Recipient: e.Recipient})
	if errors.Is(err, pgx.ErrNoRows) {
		return s.putConversation(record, key, receiptKey, payload)
	}
	if err != nil {
		return smip.Record{}, false, err
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return smip.Record{}, false, err
	}
	defer tx.Rollback(ctx)
	queries = q.New(tx)
	if _, err = queries.LockWorkspace(ctx, binding.WorkspaceID); err != nil {
		return smip.Record{}, false, err
	}
	// A concurrent admission may already have committed while this one waited.
	if stored, done, err := existing(ctx, queries, record); done {
		return stored, false, err
	}
	binding, err = queries.GetSmipBinding(ctx, q.GetSmipBindingParams{WorkspaceID: binding.WorkspaceID, ID: binding.ID})
	if err != nil {
		return smip.Record{}, false, err
	}
	if !binding.Enabled || binding.Sender != e.Sender || binding.Peer != e.From || binding.Stream != e.Stream || binding.Recipient != e.Recipient {
		return smip.Record{}, false, smip.ErrAdmissionDenied
	}
	p, err := loadPolicy(ctx, queries, binding.WorkspaceID)
	if err != nil {
		return smip.Record{}, false, err
	}
	if !p.allowsBinding(e.Kind, e.From) {
		return smip.Record{}, false, smip.ErrAdmissionDenied
	}
	if err = store(ctx, queries, record, key, receiptKey, payload, binding.WorkspaceID, &binding.ID, nil); err != nil {
		return smip.Record{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return smip.Record{}, false, err
	}
	return record, true, nil
}

// putConversation admits a packet addressed to a local mailbox address.
// Under the workspace lock it checks the sharing policy (blocked domains
// included), that a private address still has a current member as owner,
// and the recipient's decision: the first packet from a sender opens a
// pending chat request, a declined or spam-marked sender is refused, and a
// pending request holds at most MaxPendingItems packets. Refusals are the
// protocol's 403, never an internal error.
func (s *DatabaseInbox) putConversation(record smip.Record, key, receiptKey smip.Key, payload []byte) (smip.Record, bool, error) {
	ctx := s.ctx
	e := record.Packet.Envelope
	local, remote := strings.ToLower(e.Recipient), strings.ToLower(e.Sender)
	boxes, err := q.New(db.From(ctx)).FindSmipReceivingMailbox(ctx, local)
	if err != nil {
		return smip.Record{}, false, err
	}
	if len(boxes) != 1 {
		return smip.Record{}, false, smip.ErrAdmissionDenied
	}
	box := boxes[0]
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return smip.Record{}, false, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, box.WorkspaceID); err != nil {
		return smip.Record{}, false, err
	}
	if stored, done, err := existing(ctx, queries, record); done {
		return stored, false, err
	}
	p, err := loadPolicy(ctx, queries, box.WorkspaceID)
	if err != nil {
		return smip.Record{}, false, err
	}
	if !p.allows(e.Kind, e.From) {
		return smip.Record{}, false, smip.ErrAdmissionDenied
	}
	var owner *string
	if box.Private {
		if box.OwnerSubject == nil {
			return smip.Record{}, false, smip.ErrAdmissionDenied
		}
		member, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: box.WorkspaceID, Subject: *box.OwnerSubject})
		if err != nil {
			return smip.Record{}, false, err
		}
		if !member {
			return smip.Record{}, false, smip.ErrAdmissionDenied
		}
		owner = box.OwnerSubject
	}
	conversation, err := queries.FindSmipConversation(ctx, q.FindSmipConversationParams{WorkspaceID: box.WorkspaceID, LocalAddress: local, RemoteAddress: remote})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		count, err := queries.CountSmipConversations(ctx, box.WorkspaceID)
		if err != nil {
			return smip.Record{}, false, err
		}
		if count >= MaxConversations {
			return smip.Record{}, false, errors.New("SMIP conversation quota exceeded")
		}
		conversation, err = queries.AddSmipConversation(ctx, q.AddSmipConversationParams{ID: uuid.NewString(), WorkspaceID: box.WorkspaceID, MailboxID: box.ID, OwnerSubject: owner, LocalAddress: local, RemoteAddress: remote, RemoteDomain: e.From, Stream: e.Stream, State: ConversationPending, CreatedAt: time.Unix(record.Receipt.Accepted, 0)})
		if err != nil {
			return smip.Record{}, false, err
		}
	case err != nil:
		return smip.Record{}, false, err
	case conversation.State == ConversationDeclined || conversation.State == ConversationSpam:
		return smip.Record{}, false, smip.ErrAdmissionDenied
	case conversation.State == ConversationPending:
		held, err := queries.CountSmipConversationInbox(ctx, &conversation.ID)
		if err != nil {
			return smip.Record{}, false, err
		}
		if held >= MaxPendingItems {
			return smip.Record{}, false, smip.ErrAdmissionDenied
		}
	}
	if err = store(ctx, queries, record, key, receiptKey, payload, box.WorkspaceID, nil, &conversation.ID); err != nil {
		return smip.Record{}, false, err
	}
	if err = queries.TouchSmipConversation(ctx, q.TouchSmipConversationParams{WorkspaceID: box.WorkspaceID, ID: conversation.ID, UpdatedAt: time.Unix(record.Receipt.Accepted, 0)}); err != nil {
		return smip.Record{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return smip.Record{}, false, err
	}
	return record, true, nil
}

// existing returns the committed record of a concurrent identical admission
// (done=true), a conflict for changed content under the same ID, or
// done=false when the packet is new.
func existing(ctx context.Context, queries *q.Queries, record smip.Record) (smip.Record, bool, error) {
	e := record.Packet.Envelope
	old, err := queries.FindSmipReceipt(ctx, q.FindSmipReceiptParams{Origin: e.From, MessageID: e.ID})
	if err == nil {
		if old.Digest != record.Packet.Digest() {
			return smip.Record{}, true, smip.ErrConflict
		}
		stored, err := decode(old)
		return stored, true, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return smip.Record{}, true, err
	}
	return smip.Record{}, false, nil
}

// acceptedAt is the receipt's acceptance second, with this moment's
// sub-second part when it is the same second, so a conversation orders a
// reply after the message it answers. Integrity checks compare the second.
func acceptedAt(ctx context.Context, record smip.Record) time.Time {
	if now := lidza.Now(ctx); now.Unix() == record.Receipt.Accepted {
		return now
	}
	return time.Unix(record.Receipt.Accepted, 0)
}

// store writes the signed packet and its receipt within the workspace's
// retained quota, for a binding or a conversation.
func store(ctx context.Context, queries *q.Queries, record smip.Record, key, receiptKey smip.Key, payload []byte, workspaceID string, bindingID, conversationID *string) error {
	e := record.Packet.Envelope
	usage, err := queries.SmipInboxUsage(ctx, workspaceID)
	if err != nil {
		return err
	}
	if usage.Records >= MaxInboxRecords || usage.Bytes+int64(len(payload)) > MaxInboxBytes {
		return errors.New("SMIP inbox quota exceeded")
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	originJSON, err := json.Marshal(key)
	if err != nil {
		return err
	}
	receiptJSON, err := json.Marshal(receiptKey)
	if err != nil {
		return err
	}
	body, name := "", ""
	if e.Kind == "chat" {
		body = string(payload)
	} else {
		b, err := base64.RawURLEncoding.DecodeString(e.Name)
		if err != nil {
			return err
		}
		name = string(b)
	}
	_, err = queries.AcceptSmipPacket(ctx, q.AcceptSmipPacketParams{WorkspaceID: workspaceID, BindingID: bindingID, ConversationID: conversationID, Origin: e.From, MessageID: e.ID, Digest: record.Packet.Digest(), Record: string(raw), OriginKey: string(originJSON), ReceiptKey: string(receiptJSON), Kind: e.Kind, Sender: e.Sender, Recipient: e.Recipient, Stream: e.Stream, Body: body, Name: name, Size: int32(len(payload)), AcceptedAt: acceptedAt(ctx, record)})
	if err != nil {
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.Code == "23505" {
			return smip.ErrConflict
		}
		return err
	}
	return nil
}
