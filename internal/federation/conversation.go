package federation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	q "thura/db/queries/gen"
	"thura/internal/drive"
	"thura/internal/smip"
	"thura/internal/smipmail"
	"thura/internal/workspace"
	"thura/schema"
)

// External conversation states. A pending request is visible only to its
// recipient and raises nothing else; accepted lets messages flow both ways;
// declined and spam refuse the sender until the recipient accepts again.
const (
	ConversationPending  = "pending"
	ConversationAccepted = "accepted"
	ConversationDeclined = "declined"
	ConversationSpam     = "spam"
)

// visible reports whether the signed-in member may see a conversation: the
// owner of the private address it belongs to, or any member for a shared
// address (the Mail rule, mailaccess.Owns). Everyone else gets 404.
func visible(row q.SmipConversation, subject string) bool {
	return row.OwnerSubject == nil || *row.OwnerSubject == subject
}

func conversationView(row q.SmipConversation, verified, preview, previewKind string, waiting int) schema.SmipConversationView {
	return schema.SmipConversationView{ID: row.ID, LocalAddress: row.LocalAddress, RemoteAddress: row.RemoteAddress, RemoteDomain: row.RemoteDomain, State: row.State, Verified: verified, Shared: row.OwnerSubject == nil, Started: row.StartedBy != "", Preview: preview, PreviewKind: previewKind, Waiting: waiting, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

// clip bounds a request preview without splitting a character.
func clip(text string, max int) string {
	if len(text) <= max {
		return text
	}
	text = text[:max]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text + "…"
}

// externalAddress is the signed-in member's own address at the local SMIP
// domain: their private mailbox in this workspace, or "" without one.
func externalAddress(ctx context.Context, queries *q.Queries, w, subject, domain string) (q.Mailbox, bool, error) {
	if domain == "" {
		return q.Mailbox{}, false, nil
	}
	boxes, err := queries.ListMailboxes(ctx, w)
	if err != nil {
		return q.Mailbox{}, false, err
	}
	for _, box := range boxes {
		if box.Private && box.OwnerSubject != nil && *box.OwnerSubject == subject && smip.AddressAt(strings.ToLower(box.Address), domain) {
			return box, true, nil
		}
	}
	return q.Mailbox{}, false, nil
}

// ListConversations is a member's Chat view of external correspondents:
// their own requests and conversations and those of the workspace's shared
// addresses, with what the policy currently allows.
func ListConversations(ctx context.Context, w string) (schema.SmipConversationList, error) {
	out := schema.SmipConversationList{Items: []schema.SmipConversationView{}, Chat: schema.SmipSharingModeOff, Files: schema.SmipSharingModeOff}
	if err := workspace.RequireMember(ctx, w); err != nil {
		return out, err
	}
	g := From(ctx)
	out.Configured = g.Enabled()
	if !out.Configured {
		return out, nil
	}
	subject := auth.CurrentUser(ctx).ID
	queries := q.New(db.From(ctx))
	role, err := queries.WorkspaceRole(ctx, q.WorkspaceRoleParams{Scope: w, Subject: subject})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	out.Manager = role == "owner" || role == "admin"
	p, err := loadPolicy(ctx, queries, w)
	if err != nil {
		return out, err
	}
	out.Chat, out.Files = p.chat, p.files
	if box, ok, err := externalAddress(ctx, queries, w, subject, g.domain); err != nil {
		return out, err
	} else if ok {
		out.Address = strings.ToLower(box.Address)
	}
	trust, err := g.Trust(ctx)
	if err != nil {
		return out, err
	}
	rows, err := queries.ListSmipConversations(ctx, q.ListSmipConversationsParams{WorkspaceID: w, Subject: subject})
	if err != nil {
		return out, err
	}
	status := map[string]string{}
	for _, row := range rows {
		if _, ok := status[row.RemoteDomain]; !ok {
			status[row.RemoteDomain] = g.verifiedStatus(ctx, trust, row.RemoteDomain)
		}
		preview := clip(row.PreviewBody, 200)
		if row.PreviewKind == "file" {
			preview = row.PreviewName
		}
		conversation := q.SmipConversation{ID: row.ID, WorkspaceID: row.WorkspaceID, MailboxID: row.MailboxID, OwnerSubject: row.OwnerSubject, LocalAddress: row.LocalAddress, RemoteAddress: row.RemoteAddress, RemoteDomain: row.RemoteDomain, Stream: row.Stream, State: row.State, StartedBy: row.StartedBy, DecidedBy: row.DecidedBy, DecidedAt: row.DecidedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
		out.Items = append(out.Items, conversationView(conversation, status[row.RemoteDomain], preview, row.PreviewKind, int(row.Received)))
	}
	return out, nil
}

// readConversation loads one conversation the signed-in member may see.
func readConversation(ctx context.Context, queries *q.Queries, w, id string, lock bool) (q.SmipConversation, error) {
	if !workspace.ValidID(id) {
		return q.SmipConversation{}, router.Errorf(404, "conversation not found")
	}
	var row q.SmipConversation
	var err error
	if lock {
		row, err = queries.LockSmipConversation(ctx, q.LockSmipConversationParams{WorkspaceID: w, ID: id})
	} else {
		row, err = queries.GetSmipConversation(ctx, q.GetSmipConversationParams{WorkspaceID: w, ID: id})
	}
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !visible(row, auth.CurrentUser(ctx).ID) {
		return q.SmipConversation{}, router.Errorf(404, "conversation not found")
	}
	return row, err
}

// GetConversation returns one conversation and its messages, both
// directions in order. A pending request shows what the sender sent as its
// preview; nothing is delivered elsewhere until it is accepted.
func GetConversation(ctx context.Context, w, id string) (schema.SmipConversationDetail, error) {
	out := schema.SmipConversationDetail{Items: []schema.SmipConversationItem{}}
	if err := workspace.RequireMember(ctx, w); err != nil {
		return out, err
	}
	queries := q.New(db.From(ctx))
	row, err := readConversation(ctx, queries, w, id, false)
	if err != nil {
		return out, err
	}
	return conversationDetail(ctx, queries, row)
}

func conversationDetail(ctx context.Context, queries *q.Queries, row q.SmipConversation) (schema.SmipConversationDetail, error) {
	out := schema.SmipConversationDetail{Items: []schema.SmipConversationItem{}}
	g := From(ctx)
	trust, err := g.Trust(ctx)
	if err != nil {
		return out, err
	}
	incoming, err := queries.ListSmipConversationInbox(ctx, q.ListSmipConversationInboxParams{WorkspaceID: row.WorkspaceID, ConversationID: &row.ID})
	if err != nil {
		return out, err
	}
	outgoing, err := queries.ListSmipConversationOutbox(ctx, q.ListSmipConversationOutboxParams{WorkspaceID: row.WorkspaceID, ConversationID: &row.ID})
	if err != nil {
		return out, err
	}
	preview, kind := "", ""
	for _, item := range incoming {
		state := "received"
		if item.Kind == "file" {
			state = "pending"
			if item.ImportedAt != nil {
				state = "saved"
			} else if item.DeclinedAt != nil {
				state = "declined"
			}
		}
		out.Items = append(out.Items, schema.SmipConversationItem{ID: item.ID, Direction: "in", Kind: item.Kind, Body: item.Body, Name: item.Name, Size: int(item.Size), State: state, FileID: item.ImportedFileID, At: item.AcceptedAt})
		preview, kind = clip(item.Body, 200), item.Kind
		if item.Kind == "file" {
			preview = item.Name
		}
	}
	for _, item := range outgoing {
		out.Items = append(out.Items, schema.SmipConversationItem{ID: item.ID, Direction: "out", Kind: item.Kind, Body: item.Body, Name: item.Name, Size: int(item.Size), State: item.State, Reason: item.Reason, At: item.CreatedAt})
	}
	sort.SliceStable(out.Items, func(i, j int) bool { return out.Items[i].At.Before(out.Items[j].At) })
	waiting := 0
	if row.State == ConversationPending {
		waiting = len(incoming)
	}
	out.Conversation = conversationView(row, g.verifiedStatus(ctx, trust, row.RemoteDomain), preview, kind, waiting)
	return out, nil
}

// parseExternalAddress splits name@partner.example with the wire's token
// alphabet for the name and a DNS identity for the domain.
func parseExternalAddress(raw string) (string, string, bool) {
	address := strings.ToLower(strings.TrimSpace(raw))
	at := strings.LastIndexByte(address, '@')
	if at < 1 {
		return "", "", false
	}
	domain := address[at+1:]
	return address, domain, smip.ValidDomain(domain) && smip.AddressAt(address, domain)
}

// reachability turns a discovery failure into what the member reads.
func reachability(err error, domain string) error {
	if errors.Is(err, smipmail.ErrUnsupported) {
		return router.Errorf(422, "%s doesn't support secure delivery.", domain)
	}
	return router.Errorf(503, "%s can't be verified right now, so nothing was sent. Try again later.", domain)
}

// checkPartner is the discovery half of an outbound check: a pinned partner
// must still have an active key and a sending endpoint; any other domain
// must verify through discovery. It runs outside any transaction.
func checkPartner(ctx context.Context, g *Gateway, domain string) error {
	trust, err := g.Trust(ctx)
	if err != nil {
		return err
	}
	p, err := g.resolvePartner(ctx, &trust, domain)
	if err != nil {
		return reachability(err, domain)
	}
	if !hasActiveKey(p.Keys) || p.Origin == "" {
		return router.Errorf(422, "%s doesn't support secure delivery from this server.", domain)
	}
	return nil
}

// StartConversation opens an external chat from the signed-in member's own
// address to name@partner.example, after the policy and discovery allow it.
// It is idempotent per (address, recipient): an existing conversation is
// returned, and one the member declined earlier is accepted again because
// they chose to write.
func StartConversation(ctx context.Context, w string, in schema.SmipConversationInput) (schema.SmipConversationDetail, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return schema.SmipConversationDetail{}, err
	}
	g := From(ctx)
	if !g.Enabled() {
		return schema.SmipConversationDetail{}, router.Errorf(503, "External chat isn't available on this server.")
	}
	if err := in.Validate(); err != nil {
		return schema.SmipConversationDetail{}, err
	}
	remote, domain, ok := parseExternalAddress(in.Address)
	if !ok {
		return schema.SmipConversationDetail{}, router.Errorf(422, "Enter an address such as name@partner.example.")
	}
	if domain == g.domain {
		return schema.SmipConversationDetail{}, router.Errorf(422, "%s is this server; start a workspace chat instead.", domain)
	}
	queries := q.New(db.From(ctx))
	p, err := loadPolicy(ctx, queries, w)
	if err != nil {
		return schema.SmipConversationDetail{}, err
	}
	if !p.allows("chat", domain) {
		return schema.SmipConversationDetail{}, router.Errorf(403, "%s", p.refusal("chat", domain))
	}
	if err = checkPartner(ctx, g, domain); err != nil {
		return schema.SmipConversationDetail{}, err
	}
	tx, queries, err := lockAccess(ctx, w, false)
	if err != nil {
		return schema.SmipConversationDetail{}, err
	}
	defer tx.Rollback(ctx)
	subject := auth.CurrentUser(ctx).ID
	box, ok, err := externalAddress(ctx, queries, w, subject, g.domain)
	if err != nil {
		return schema.SmipConversationDetail{}, err
	}
	if !ok {
		return schema.SmipConversationDetail{}, router.Errorf(409, "Your account has no address at %s for external chat. Ask an administrator for a personal mailbox there.", g.domain)
	}
	// The policy may have changed while discovery ran.
	if p, err = loadPolicy(ctx, queries, w); err != nil {
		return schema.SmipConversationDetail{}, err
	}
	if !p.allows("chat", domain) {
		return schema.SmipConversationDetail{}, router.Errorf(403, "%s", p.refusal("chat", domain))
	}
	local := strings.ToLower(box.Address)
	now := lidza.Now(ctx)
	row, err := queries.FindSmipConversation(ctx, q.FindSmipConversationParams{WorkspaceID: w, LocalAddress: local, RemoteAddress: remote})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		count, err := queries.CountSmipConversations(ctx, w)
		if err != nil {
			return schema.SmipConversationDetail{}, err
		}
		if count >= MaxConversations {
			return schema.SmipConversationDetail{}, router.Errorf(409, "This workspace has reached its limit of external conversations.")
		}
		row, err = queries.AddSmipConversation(ctx, q.AddSmipConversationParams{ID: in.RequestID, WorkspaceID: w, MailboxID: box.ID, OwnerSubject: box.OwnerSubject, LocalAddress: local, RemoteAddress: remote, RemoteDomain: domain, Stream: uuid.NewString(), State: ConversationAccepted, StartedBy: subject, CreatedAt: now})
		if err != nil {
			var conflict *pgconn.PgError
			if errors.As(err, &conflict) && conflict.Code == "23505" {
				return schema.SmipConversationDetail{}, router.Errorf(409, "request ID already used")
			}
			return schema.SmipConversationDetail{}, err
		}
		if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.conversation.start", Scope: w, Resource: "smip-conversation/" + row.ID, Meta: map[string]string{"domain": domain}}); err != nil {
			return schema.SmipConversationDetail{}, err
		}
	case err != nil:
		return schema.SmipConversationDetail{}, err
	case row.State != ConversationAccepted:
		if err = decide(ctx, tx, queries, row, ConversationAccepted, "smip.request.accept"); err != nil {
			return schema.SmipConversationDetail{}, err
		}
		row.State = ConversationAccepted
	}
	if err = tx.Commit(ctx); err != nil {
		return schema.SmipConversationDetail{}, err
	}
	return conversationDetail(ctx, q.New(db.From(ctx)), row)
}

func decide(ctx context.Context, tx pgx.Tx, queries *q.Queries, row q.SmipConversation, state, action string) error {
	now := lidza.Now(ctx)
	if err := queries.DecideSmipConversation(ctx, q.DecideSmipConversationParams{WorkspaceID: row.WorkspaceID, ID: row.ID, State: state, DecidedBy: auth.CurrentUser(ctx).ID, DecidedAt: &now}); err != nil {
		return err
	}
	return audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: action, Scope: row.WorkspaceID, Resource: "smip-conversation/" + row.ID, Meta: map[string]string{"domain": row.RemoteDomain}})
}

// Decisions a recipient can take on a request or conversation.
const (
	DecisionAccept  = "accept"
	DecisionDecline = "decline"
	DecisionSpam    = "spam"
	DecisionBlock   = "block"
)

// Decide records the recipient's decision. Accept opens the conversation
// (also after an earlier decline, which is how a member changes their mind
// in Settings); decline and spam refuse the sender from now on; block
// refuses the whole domain for the workspace and is for owners and admins
// only. Each decision is audited with the change.
func Decide(ctx context.Context, w, id, decision string) (schema.SmipConversationDetail, error) {
	tx, queries, err := lockAccess(ctx, w, false)
	if err != nil {
		return schema.SmipConversationDetail{}, err
	}
	defer tx.Rollback(ctx)
	row, err := readConversation(ctx, queries, w, id, true)
	if err != nil {
		return schema.SmipConversationDetail{}, err
	}
	subject := auth.CurrentUser(ctx).ID
	switch decision {
	case DecisionAccept:
		if row.State != ConversationAccepted {
			err = decide(ctx, tx, queries, row, ConversationAccepted, "smip.request.accept")
			row.State = ConversationAccepted
		}
	case DecisionDecline:
		if row.State != ConversationDeclined {
			err = decide(ctx, tx, queries, row, ConversationDeclined, "smip.request.decline")
			row.State = ConversationDeclined
		}
	case DecisionSpam:
		if row.State != ConversationSpam {
			err = decide(ctx, tx, queries, row, ConversationSpam, "smip.request.spam")
			row.State = ConversationSpam
		}
	case DecisionBlock:
		role, roleErr := queries.WorkspaceRole(ctx, q.WorkspaceRoleParams{Scope: w, Subject: subject})
		if roleErr != nil || role != "owner" && role != "admin" {
			return schema.SmipConversationDetail{}, router.Errorf(403, "Only workspace owners and admins can block a domain.")
		}
		now := lidza.Now(ctx)
		if err = queries.BlockSmipDomain(ctx, q.BlockSmipDomainParams{WorkspaceID: w, Domain: row.RemoteDomain, BlockedBy: subject, BlockedAt: now}); err != nil {
			return schema.SmipConversationDetail{}, err
		}
		if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.domain.block", Scope: w, Resource: "smip-policy/" + w, Meta: map[string]string{"domain": row.RemoteDomain}}); err != nil {
			return schema.SmipConversationDetail{}, err
		}
		if row.State != ConversationDeclined {
			err = decide(ctx, tx, queries, row, ConversationDeclined, "smip.request.decline")
			row.State = ConversationDeclined
		}
	default:
		return schema.SmipConversationDetail{}, router.Errorf(404, "unknown decision")
	}
	if err != nil {
		return schema.SmipConversationDetail{}, err
	}
	row, err = queries.GetSmipConversation(ctx, q.GetSmipConversationParams{WorkspaceID: w, ID: row.ID})
	if err != nil {
		return schema.SmipConversationDetail{}, err
	}
	detail, err := conversationDetail(ctx, queries, row)
	if err != nil {
		return detail, err
	}
	return detail, tx.Commit(ctx)
}

// SendConversationMessage queues chat text or an immutable copy of a Drive
// file to an accepted conversation, through the same signed durable outbox
// and dispatch as stream sends. The policy is checked here and again before
// every attempt; a partner that never supports secure delivery is refused
// at once, one that cannot be verified right now is queued and held.
func SendConversationMessage(ctx context.Context, w, id string, in schema.SmipConversationMessageInput) (schema.SmipConversationItem, error) {
	if err := workspace.RequireMember(ctx, w); err != nil {
		return schema.SmipConversationItem{}, err
	}
	if err := in.Validate(); err != nil {
		return schema.SmipConversationItem{}, err
	}
	if !workspace.ValidID(in.TransactionID) {
		return schema.SmipConversationItem{}, router.Errorf(422, "valid transaction ID required")
	}
	g := From(ctx)
	if !g.Enabled() {
		return schema.SmipConversationItem{}, router.Errorf(503, "External chat isn't available on this server.")
	}
	body := ""
	if in.Body != nil {
		body = *in.Body
	}
	// Discovery runs before any lock; a definitive "unsupported" refuses.
	if peek, err := readConversation(ctx, q.New(db.From(ctx)), w, id, false); err == nil {
		if err := checkPartner(ctx, g, peek.RemoteDomain); err != nil {
			var failure *router.HTTPError
			if !errors.As(err, &failure) || failure.Status != 503 {
				return schema.SmipConversationItem{}, err
			}
		}
	}
	tx, queries, err := lockAccess(ctx, w, false)
	if err != nil {
		return schema.SmipConversationItem{}, err
	}
	defer tx.Rollback(ctx)
	row, err := readConversation(ctx, queries, w, id, true)
	if err != nil {
		return schema.SmipConversationItem{}, err
	}
	subject := auth.CurrentUser(ctx).ID
	old, err := queries.GetSmipOutbound(ctx, q.GetSmipOutboundParams{WorkspaceID: w, ID: in.TransactionID})
	if err == nil {
		if old.Subject != subject || old.ConversationID == nil || *old.ConversationID != row.ID || old.Body != body || !sameFile(old.SourceFileID, in.FileID) {
			return schema.SmipConversationItem{}, router.Errorf(409, "transaction ID already used")
		}
		return schema.SmipConversationItem{ID: old.ID, Direction: "out", Kind: old.Kind, Body: old.Body, Name: old.Name, Size: int(old.Size), State: old.State, Reason: old.Reason, At: old.CreatedAt}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return schema.SmipConversationItem{}, err
	}
	switch row.State {
	case ConversationPending:
		return schema.SmipConversationItem{}, router.Errorf(409, "Accept the chat request before replying.")
	case ConversationDeclined, ConversationSpam:
		return schema.SmipConversationItem{}, router.Errorf(409, "You declined this sender. Accept them again to reply.")
	}
	kind, name, data := "chat", "", []byte(body)
	if in.FileID != nil {
		if body != "" || !workspace.ValidID(*in.FileID) {
			return schema.SmipConversationItem{}, router.Errorf(422, "choose a file without chat text")
		}
		kind = "file"
	} else if strings.TrimSpace(body) == "" {
		return schema.SmipConversationItem{}, router.Errorf(422, "message required")
	}
	p, err := loadPolicy(ctx, queries, w)
	if err != nil {
		return schema.SmipConversationItem{}, err
	}
	if !p.allows(kind, row.RemoteDomain) {
		return schema.SmipConversationItem{}, router.Errorf(403, "%s", p.refusal(kind, row.RemoteDomain))
	}
	if kind == "file" {
		file, err := queries.GetDriveFile(ctx, q.GetDriveFileParams{WorkspaceID: w, ID: *in.FileID})
		if errors.Is(err, pgx.ErrNoRows) {
			return schema.SmipConversationItem{}, router.Errorf(404, "file not found")
		}
		if err != nil {
			return schema.SmipConversationItem{}, err
		}
		if file.Trashed {
			return schema.SmipConversationItem{}, router.Errorf(404, "file not found")
		}
		content, err := drive.Content(ctx, file, int(file.CurrentVersion))
		if err != nil {
			return schema.SmipConversationItem{}, err
		}
		if data, err = base64.StdEncoding.DecodeString(content.Data); err != nil {
			return schema.SmipConversationItem{}, err
		}
		name = file.Name
	}
	usage, err := queries.SmipOutboxUsage(ctx, w)
	if err != nil {
		return schema.SmipConversationItem{}, err
	}
	if usage.Records >= MaxInboxRecords || usage.Bytes+int64(len(data)) > MaxInboxBytes {
		return schema.SmipConversationItem{}, router.Errorf(409, "This workspace's outgoing external messages are at capacity.")
	}
	now := lidza.Now(ctx)
	e := smip.Envelope{Version: smip.Version, ID: in.TransactionID, From: g.domain, To: row.RemoteDomain, Sender: row.LocalAddress, Recipient: row.RemoteAddress, Stream: row.Stream, Kind: kind, Created: now.Unix(), Expires: now.Unix() + 7*86400, Payload: smip.Encode(data)}
	if kind == "file" {
		e.Name = smip.Encode([]byte(name))
	}
	packet, err := smip.Sign(e, g.signer.Private)
	if err != nil {
		return schema.SmipConversationItem{}, router.Errorf(422, "message does not satisfy SMIP limits")
	}
	originKey, known := g.originKeys[packet.Envelope.KeyID]
	if !known || packet.Verify(originKey) != nil {
		return schema.SmipConversationItem{}, router.Errorf(503, "SMIP origin signing key unavailable")
	}
	raw, err := json.Marshal(packet)
	if err != nil {
		return schema.SmipConversationItem{}, err
	}
	keyJSON, err := json.Marshal(originKey)
	if err != nil {
		return schema.SmipConversationItem{}, err
	}
	out, err := queries.AddSmipOutbound(ctx, q.AddSmipOutboundParams{ID: in.TransactionID, WorkspaceID: w, ConversationID: &row.ID, Subject: subject, Packet: string(raw), OriginKey: string(keyJSON), Kind: kind, Body: body, Name: name, Size: int32(len(data)), SourceFileID: in.FileID, Origin: g.domain, NextAttempt: now})
	if err != nil {
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.Code == "23505" {
			return schema.SmipConversationItem{}, router.Errorf(409, "transaction ID already used")
		}
		return schema.SmipConversationItem{}, err
	}
	if err = queries.TouchSmipConversation(ctx, q.TouchSmipConversationParams{WorkspaceID: w, ID: row.ID, UpdatedAt: now}); err != nil {
		return schema.SmipConversationItem{}, err
	}
	if _, err = jobs.From(ctx).EnqueueTx(ctx, tx, g.DispatchKind(), schema.SmipDispatchJob{ID: out.ID}, jobs.Unique(out.ID)); err != nil {
		return schema.SmipConversationItem{}, err
	}
	if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.message.queue", Scope: w, Resource: "smip-outbox/" + out.ID}); err != nil {
		return schema.SmipConversationItem{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return schema.SmipConversationItem{}, err
	}
	return schema.SmipConversationItem{ID: out.ID, Direction: "out", Kind: out.Kind, Body: out.Body, Name: out.Name, Size: int(out.Size), State: out.State, Reason: out.Reason, At: out.CreatedAt}, nil
}

// lockIncomingFile locks one incoming file of a conversation the member may
// see, for saving or declining it.
func lockIncomingFile(ctx context.Context, w, id, itemID string) (pgx.Tx, *q.Queries, q.SmipConversation, q.SmipInbox, error) {
	tx, queries, err := lockAccess(ctx, w, false)
	if err != nil {
		return nil, nil, q.SmipConversation{}, q.SmipInbox{}, err
	}
	fail := func(err error) (pgx.Tx, *q.Queries, q.SmipConversation, q.SmipInbox, error) {
		return nil, nil, q.SmipConversation{}, q.SmipInbox{}, errors.Join(err, tx.Rollback(ctx))
	}
	row, err := readConversation(ctx, queries, w, id, true)
	if err != nil {
		return fail(err)
	}
	if !workspace.ValidID(itemID) {
		return fail(router.Errorf(404, "file not found"))
	}
	item, err := queries.LockSmipConversationInbox(ctx, q.LockSmipConversationInboxParams{WorkspaceID: w, ConversationID: &row.ID, ID: itemID})
	if errors.Is(err, pgx.ErrNoRows) || err == nil && item.Kind != "file" {
		return fail(router.Errorf(404, "file not found"))
	}
	if err != nil {
		return fail(err)
	}
	if row.State != ConversationAccepted {
		return fail(router.Errorf(409, "Accept the chat request before saving its files."))
	}
	return tx, queries, row, item, nil
}

// SaveIncomingFile saves an incoming file share into Drive (a chosen folder
// or the root) after the file policy allows it, through the same immutable
// import as stream transfers. A retry returns the saved file.
func SaveIncomingFile(ctx context.Context, w, id, itemID string, in schema.SmipImportInput) (schema.DriveFile, error) {
	tx, queries, row, item, err := lockIncomingFile(ctx, w, id, itemID)
	if err != nil {
		return schema.DriveFile{}, err
	}
	defer tx.Rollback(ctx)
	if item.DeclinedAt != nil && item.ImportedFileID == nil {
		return schema.DriveFile{}, router.Errorf(409, "You declined this file.")
	}
	if item.ImportedFileID == nil {
		p, err := loadPolicy(ctx, queries, w)
		if err != nil {
			return schema.DriveFile{}, err
		}
		if !p.allows("file", row.RemoteDomain) {
			return schema.DriveFile{}, router.Errorf(403, "%s", p.refusal("file", row.RemoteDomain))
		}
	}
	file, err := importRecord(ctx, tx, queries, w, item, in.FolderID, "smip.file.save")
	if err != nil || item.ImportedFileID != nil {
		return file, err
	}
	if err = tx.Commit(ctx); err != nil {
		return schema.DriveFile{}, err
	}
	return file, nil
}

// DeclineIncomingFile declines an incoming file share; the signed record
// and receipt stay, nothing reaches Drive.
func DeclineIncomingFile(ctx context.Context, w, id, itemID string) (schema.SmipConversationDetail, error) {
	tx, queries, row, item, err := lockIncomingFile(ctx, w, id, itemID)
	if err != nil {
		return schema.SmipConversationDetail{}, err
	}
	defer tx.Rollback(ctx)
	if item.ImportedFileID != nil {
		return schema.SmipConversationDetail{}, router.Errorf(409, "This file is already saved to Drive.")
	}
	if item.DeclinedAt == nil {
		now := lidza.Now(ctx)
		if err = queries.DeclineSmipFile(ctx, q.DeclineSmipFileParams{WorkspaceID: w, ID: item.ID, DeclinedAt: &now, DecidedBy: auth.CurrentUser(ctx).ID}); err != nil {
			return schema.SmipConversationDetail{}, err
		}
		if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.file.decline", Scope: w, Resource: "smip-inbox/" + item.ID, Meta: map[string]string{"domain": row.RemoteDomain}}); err != nil {
			return schema.SmipConversationDetail{}, err
		}
	}
	detail, err := conversationDetail(ctx, queries, row)
	if err != nil {
		return detail, err
	}
	return detail, tx.Commit(ctx)
}
