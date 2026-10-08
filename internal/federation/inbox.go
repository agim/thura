package federation

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/agim/lidza/packs/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/smip"
)

const MaxInboxRecords = 1000
const MaxInboxBytes = 64 << 20

type DatabaseInbox struct {
	ctx     context.Context
	gateway *Gateway
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
func (s *DatabaseInbox) Put(record smip.Record) (smip.Record, bool, error) {
	ctx := s.ctx
	e := record.Packet.Envelope
	key, known := s.gateway.peers[e.From][e.KeyID]
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
		return smip.Record{}, false, smip.ErrAdmissionDenied
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
	old, err := queries.FindSmipReceipt(ctx, q.FindSmipReceiptParams{Origin: e.From, MessageID: e.ID})
	if err == nil {
		if old.Digest != record.Packet.Digest() {
			return smip.Record{}, false, smip.ErrConflict
		}
		stored, err := decode(old)
		return stored, false, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return smip.Record{}, false, err
	}
	binding, err = queries.GetSmipBinding(ctx, q.GetSmipBindingParams{WorkspaceID: binding.WorkspaceID, ID: binding.ID})
	if err != nil {
		return smip.Record{}, false, err
	}
	if !binding.Enabled || binding.Sender != e.Sender || binding.Peer != e.From || binding.Stream != e.Stream || binding.Recipient != e.Recipient {
		return smip.Record{}, false, smip.ErrAdmissionDenied
	}
	usage, err := queries.SmipInboxUsage(ctx, binding.WorkspaceID)
	if err != nil {
		return smip.Record{}, false, err
	}
	if usage.Records >= MaxInboxRecords || usage.Bytes+int64(len(payload)) > MaxInboxBytes {
		return smip.Record{}, false, errors.New("SMIP inbox quota exceeded")
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return smip.Record{}, false, err
	}
	originJSON, err := json.Marshal(key)
	if err != nil {
		return smip.Record{}, false, err
	}
	receiptJSON, err := json.Marshal(receiptKey)
	if err != nil {
		return smip.Record{}, false, err
	}
	body, name := "", ""
	if e.Kind == "chat" {
		body = string(payload)
	} else {
		b, err := base64.RawURLEncoding.DecodeString(e.Name)
		if err != nil {
			return smip.Record{}, false, err
		}
		name = string(b)
	}
	_, err = queries.AcceptSmipPacket(ctx, q.AcceptSmipPacketParams{WorkspaceID: binding.WorkspaceID, BindingID: binding.ID, Origin: e.From, MessageID: e.ID, Digest: record.Packet.Digest(), Record: string(raw), OriginKey: string(originJSON), ReceiptKey: string(receiptJSON), Kind: e.Kind, Sender: e.Sender, Recipient: e.Recipient, Stream: e.Stream, Body: body, Name: name, Size: int32(len(payload)), AcceptedAt: time.Unix(record.Receipt.Accepted, 0)})
	if err != nil {
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.Code == "23505" {
			return smip.Record{}, false, smip.ErrConflict
		}
		return smip.Record{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return smip.Record{}, false, err
	}
	return record, true, nil
}
