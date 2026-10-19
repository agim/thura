package smipmail

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/smip"
)

func (g *Gateway) receive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !g.Enabled() || g.ingest == nil {
		http.Error(w, "SMIP mail disabled", http.StatusServiceUnavailable)
		return
	}
	if r.TLS == nil || r.TLS.Version < tls.VersionTLS13 {
		http.Error(w, "TLS 1.3 required", http.StatusBadRequest)
		return
	}
	if r.URL.RawQuery != "" || r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Canonical JSON required", http.StatusBadRequest)
		return
	}
	select {
	case g.admissions <- struct{}{}:
		defer func() { <-g.admissions }()
	default:
		http.Error(w, "SMIP mail receiver busy", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, smip.MaxWire)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Mail exceeds size limit", http.StatusRequestEntityTooLarge)
		return
	}
	packet, err := smip.DecodePacket(raw)
	if err != nil || packet.Envelope.Validate() != nil || packet.Envelope.Version != smip.MailVersion || packet.Envelope.To != g.domain {
		http.Error(w, "Invalid mail envelope", http.StatusBadRequest)
		return
	}
	envelope := packet.Envelope
	ctx := r.Context()
	queries := q.New(db.From(ctx))
	mailboxes, err := queries.FindSmipReceivingMailbox(ctx, envelope.Recipient)
	if err != nil {
		http.Error(w, "Mailbox unavailable", http.StatusServiceUnavailable)
		return
	}
	if len(mailboxes) != 1 {
		http.Error(w, "Recipient not accepted", http.StatusForbidden)
		return
	}
	mailbox := mailboxes[0]
	if g.teamID != "" && mailbox.WorkspaceID != g.teamID {
		http.Error(w, "Recipient not accepted", http.StatusForbidden)
		return
	}
	peer, err := g.Resolve(r.Context(), envelope.From, false)
	if err != nil {
		http.Error(w, "Sender domain cannot be verified", http.StatusServiceUnavailable)
		return
	}
	key, known := peer.Document.Keys[envelope.KeyID]
	if !known || packet.Verify(key) != nil {
		http.Error(w, "Sender signature rejected", http.StatusUnauthorized)
		return
	}
	bytes, err := envelope.Bytes()
	if err != nil {
		http.Error(w, "Invalid mail bytes", http.StatusBadRequest)
		return
	}
	payload, err := decodePayload(bytes)
	sender, parseErr := plainAddress(payload.Message.From)
	if err != nil || parseErr != nil || sender != envelope.Sender || payload.Created != envelope.Created {
		http.Error(w, "Mail identity mismatch", http.StatusBadRequest)
		return
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		http.Error(w, "Mailbox unavailable", http.StatusServiceUnavailable)
		return
	}
	defer tx.Rollback(ctx)
	queries = q.New(tx)
	if _, err = queries.LockMailbox(ctx, mailbox.ID); err != nil {
		http.Error(w, "Mailbox unavailable", http.StatusServiceUnavailable)
		return
	}
	old, err := queries.GetSmipMailReceipt(ctx, q.GetSmipMailReceiptParams{Domain: g.domain, Origin: envelope.From, MessageID: envelope.ID})
	if err == nil {
		if old.Digest != packet.Digest() || old.Recipient != envelope.Recipient {
			http.Error(w, "Message identity conflict", http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err = w.Write([]byte(old.Receipt)); err != nil {
			return
		}
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Receipt unavailable", http.StatusServiceUnavailable)
		return
	}
	now := lidza.Now(ctx)
	if envelope.Created > now.Unix()+120 || envelope.Expires <= now.Unix() {
		http.Error(w, "Mail expired", http.StatusUnprocessableEntity)
		return
	}
	if now.Unix() < g.signer.NotBefore || now.Unix() >= g.signer.NotAfter {
		http.Error(w, "Receipt key unavailable", http.StatusServiceUnavailable)
		return
	}
	// Sign only after the mailbox ingestion and receipt can commit together.
	receipt := smip.NewReceipt(packet, g.signer.Private, now.Unix())
	receiptJSON, err := json.Marshal(receipt)
	if err != nil {
		http.Error(w, "Receipt unavailable", http.StatusServiceUnavailable)
		return
	}
	item, err := g.ingest(ctx, tx, mailbox.ID, payload, "smip:"+envelope.From+":"+envelope.ID)
	if err != nil {
		var failure *router.HTTPError
		if errors.As(err, &failure) && failure.Status == http.StatusTooManyRequests {
			seconds := int64(lidza.Now(ctx).UTC().Truncate(time.Hour).Add(time.Hour).Sub(lidza.Now(ctx)).Seconds()) + 1
			w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
			http.Error(w, "Incoming mail rate limit reached", http.StatusTooManyRequests)
			return
		}
		http.Error(w, "Mail storage unavailable", http.StatusServiceUnavailable)
		return
	}
	if err = queries.AddSmipMailReceipt(ctx, q.AddSmipMailReceiptParams{Domain: g.domain, Origin: envelope.From, MessageID: envelope.ID, Recipient: envelope.Recipient, Digest: packet.Digest(), Receipt: string(receiptJSON), ItemID: item, AcceptedAt: time.Unix(receipt.Accepted, 0)}); err != nil {
		http.Error(w, "Receipt unavailable", http.StatusServiceUnavailable)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		http.Error(w, "Mail commit uncertain", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if _, err = w.Write(receiptJSON); err != nil {
		return
	} // A retry returns this same committed receipt.
}
