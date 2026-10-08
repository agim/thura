package federation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/smip"
	"thura/internal/workspace"
	"thura/schema"
)

func outboundView(row q.SmipOutbox) schema.SmipOutbound {
	return schema.SmipOutbound{ID: row.ID, BindingID: row.BindingID, Kind: row.Kind, Body: row.Body, Name: row.Name, Size: int(row.Size), SourceFileID: row.SourceFileID, State: row.State, Reason: row.Reason, Attempts: int(row.Attempts), LastStatus: int(row.LastStatus), NextAttempt: row.NextAttempt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}
func sameFile(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func Queue(ctx context.Context, w string, in schema.SmipSendInput) (schema.SmipOutbound, error) {
	body := ""
	if in.Body != nil {
		body = *in.Body
	}
	tx, queries, err := lockAccess(ctx, w, false)
	if err != nil {
		return schema.SmipOutbound{}, err
	}
	defer tx.Rollback(ctx)
	if err = in.Validate(); err != nil {
		return schema.SmipOutbound{}, err
	}
	if !workspace.ValidID(in.TransactionID) || !workspace.ValidID(in.BindingID) {
		return schema.SmipOutbound{}, router.Errorf(422, "valid transaction and binding IDs required")
	}
	old, err := queries.GetSmipOutbound(ctx, q.GetSmipOutboundParams{WorkspaceID: w, ID: in.TransactionID})
	if err == nil {
		if old.Subject != auth.CurrentUser(ctx).ID || old.BindingID != in.BindingID || old.Body != body || !sameFile(old.SourceFileID, in.FileID) {
			return schema.SmipOutbound{}, router.Errorf(409, "transaction ID already used")
		}
		return outboundView(old), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return schema.SmipOutbound{}, err
	}
	g := From(ctx)
	binding, err := queries.GetSmipBinding(ctx, q.GetSmipBindingParams{WorkspaceID: w, ID: in.BindingID})
	if errors.Is(err, pgx.ErrNoRows) {
		return schema.SmipOutbound{}, router.Errorf(404, "binding not found")
	}
	if err != nil {
		return schema.SmipOutbound{}, err
	}
	if !binding.Enabled {
		return schema.SmipOutbound{}, router.Errorf(409, "SMIP binding is disabled")
	}
	if g.origins[binding.Peer] == "" {
		return schema.SmipOutbound{}, router.Errorf(503, "SMIP peer sending endpoint is not configured")
	}
	kind, name, data := "chat", "", []byte(body)
	if in.FileID != nil {
		if body != "" || !workspace.ValidID(*in.FileID) {
			return schema.SmipOutbound{}, router.Errorf(422, "choose a file without chat text")
		}
		file, err := queries.GetDriveFile(ctx, q.GetDriveFileParams{WorkspaceID: w, ID: *in.FileID})
		if errors.Is(err, pgx.ErrNoRows) {
			return schema.SmipOutbound{}, router.Errorf(404, "file not found")
		}
		if err != nil {
			return schema.SmipOutbound{}, err
		}
		content, err := drive.Content(ctx, file, int(file.CurrentVersion))
		if err != nil {
			return schema.SmipOutbound{}, err
		}
		data, err = base64.StdEncoding.DecodeString(content.Data)
		if err != nil {
			return schema.SmipOutbound{}, err
		}
		kind, name = "file", file.Name
	} else if strings.TrimSpace(body) == "" {
		return schema.SmipOutbound{}, router.Errorf(422, "message required")
	}
	usage, err := queries.SmipOutboxUsage(ctx, w)
	if err != nil {
		return schema.SmipOutbound{}, err
	}
	if usage.Records >= MaxInboxRecords || usage.Bytes+int64(len(data)) > MaxInboxBytes {
		return schema.SmipOutbound{}, router.Errorf(409, "SMIP outbox capacity reached")
	}
	now := lidza.Now(ctx)
	e := smip.Envelope{Version: smip.Version, ID: in.TransactionID, From: g.domain, To: binding.Peer, Sender: binding.Recipient, Recipient: binding.Sender, Stream: binding.Stream, Kind: kind, Created: now.Unix(), Expires: now.Unix() + 7*86400, Payload: smip.Encode(data)}
	if kind == "file" {
		e.Name = smip.Encode([]byte(name))
	}
	packet, err := smip.Sign(e, g.signer.Private)
	if err != nil {
		return schema.SmipOutbound{}, router.Errorf(422, "message does not satisfy SMIP limits")
	}
	originKey, known := g.originKeys[packet.Envelope.KeyID]
	if !known || packet.Verify(originKey) != nil {
		return schema.SmipOutbound{}, router.Errorf(503, "SMIP origin signing key unavailable")
	}
	raw, err := json.Marshal(packet)
	if err != nil {
		return schema.SmipOutbound{}, err
	}
	keyJSON, err := json.Marshal(originKey)
	if err != nil {
		return schema.SmipOutbound{}, err
	}
	row, err := queries.AddSmipOutbound(ctx, q.AddSmipOutboundParams{ID: in.TransactionID, WorkspaceID: w, BindingID: binding.ID, Subject: auth.CurrentUser(ctx).ID, Packet: string(raw), OriginKey: string(keyJSON), Kind: kind, Body: body, Name: name, Size: int32(len(data)), SourceFileID: in.FileID, Origin: g.domain, NextAttempt: now})
	if err != nil {
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.Code == "23505" {
			return schema.SmipOutbound{}, router.Errorf(409, "transaction ID already used")
		}
		return schema.SmipOutbound{}, err
	}
	if _, err = jobs.From(ctx).EnqueueTx(ctx, tx, g.DispatchKind(), schema.SmipDispatchJob{ID: row.ID}, jobs.Unique(row.ID)); err != nil {
		return schema.SmipOutbound{}, err
	}
	if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.message.queue", Scope: w, Resource: "smip-outbox/" + row.ID}); err != nil {
		return schema.SmipOutbound{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return schema.SmipOutbound{}, err
	}
	return outboundView(row), nil
}
func ListOutbox(ctx context.Context, w, cursor string) (schema.SmipOutboxList, error) {
	out := schema.SmipOutboxList{Items: []schema.SmipOutbound{}}
	if err := workspace.RequireMember(ctx, w); err != nil {
		return out, err
	}
	if cursor != "" && !workspace.ValidID(cursor) {
		return out, router.Errorf(422, "invalid outbox cursor")
	}
	rows, err := q.New(db.From(ctx)).ListSmipOutbox(ctx, q.ListSmipOutboxParams{WorkspaceID: w, ID: cursor})
	if err != nil {
		return out, err
	}
	if len(rows) > 50 {
		out.NextCursor = rows[49].ID
		rows = rows[:50]
	}
	for _, r := range rows {
		out.Items = append(out.Items, schema.SmipOutbound{ID: r.ID, BindingID: r.BindingID, Kind: r.Kind, Body: r.Body, Name: r.Name, Size: int(r.Size), SourceFileID: r.SourceFileID, State: r.State, Reason: r.Reason, Attempts: int(r.Attempts), LastStatus: int(r.LastStatus), NextAttempt: r.NextAttempt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt})
	}
	return out, nil
}
func decodeOutbound(row q.SmipOutbox) (smip.Packet, error) {
	var p smip.Packet
	var key smip.Key
	if json.Unmarshal([]byte(row.Packet), &p) != nil || json.Unmarshal([]byte(row.OriginKey), &key) != nil || p.Verify(key) != nil || p.Envelope.ID != row.ID || p.Envelope.From != row.Origin || p.Envelope.Kind != row.Kind {
		return p, errors.New("invalid signed SMIP outbox")
	}
	b, err := p.Envelope.Bytes()
	if err != nil || len(b) != int(row.Size) {
		return p, errors.New("invalid SMIP outbox bytes")
	}
	return p, nil
}
