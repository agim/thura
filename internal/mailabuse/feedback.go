package mailabuse

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/packs/storage"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/rspamd"
	"thura/schema"
)

const FeedbackJob = "thura.mail.feedback"

func queueFeedback(ctx context.Context, tx pgx.Tx, w, item, actor, classification string) error {
	row, err := q.New(tx).SaveMailFilterFeedback(ctx, q.SaveMailFilterFeedbackParams{WorkspaceID: w, ItemID: item, Actor: actor, Classification: classification, NextAttempt: lidza.Now(ctx)})
	if err != nil {
		return err
	}
	if row.State == "learned" || row.State == "recorded" {
		return nil
	}
	_, err = jobs.From(ctx).EnqueueTx(ctx, tx, FeedbackJob, schema.MailScreenJob{ID: item}, jobs.Unique(item))
	return err
}

// LearnFeedback serializes each message's corrections across nodes. The short
// row lock spans only a bounded background call, never an Inbox HTTP request.
// Replays use Rspamd's per-team learning cache; changing class reverses the learn.
func LearnFeedback(ctx context.Context, payload json.RawMessage) error {
	var job schema.MailScreenJob
	if json.Unmarshal(payload, &job) != nil || job.Validate() != nil {
		return errors.New("invalid feedback job")
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	f, err := queries.LockMailFilterFeedback(ctx, job.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if (f.State == "learned" || f.State == "recorded") && f.LearnedRevision == f.Revision || f.State == "cancelled" || lidza.Now(ctx).Before(f.NextAttempt) {
		return nil
	}
	member, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: f.WorkspaceID, Subject: f.Actor})
	if err != nil {
		return err
	}
	state := "learned"
	learned := f.Revision
	if !member {
		state = "cancelled"
		learned = f.LearnedRevision
	} else {
		err = deliverFeedback(ctx, queries, f)
		if errors.Is(err, rspamd.ErrNotLearnable) {
			state = "recorded"
		} else if err != nil {
			state = "error"
			learned = f.LearnedRevision
		}
	}
	attempts := f.Attempts + 1
	delay := time.Duration(attempts) * time.Minute
	if delay > time.Hour {
		delay = time.Hour
	}
	now := lidza.Now(ctx)
	if err = queries.FinishMailFilterFeedback(ctx, q.FinishMailFilterFeedbackParams{ItemID: f.ItemID, State: state, LearnedRevision: learned, Attempts: attempts, NextAttempt: now.Add(delay), UpdatedAt: now, Revision: f.Revision}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func deliverFeedback(ctx context.Context, queries *q.Queries, f q.MailFilterFeedback) error {
	cfg, err := rspamd.Configuration()
	if err != nil {
		return err
	}
	item, err := queries.GetMailItem(ctx, f.ItemID)
	if err != nil {
		return err
	}
	box, err := queries.GetMailbox(ctx, item.MailboxID)
	if err != nil {
		return err
	}
	if box.WorkspaceID != f.WorkspaceID {
		return errors.New("feedback scope mismatch")
	}
	if item.RawKey == "" {
		raw, err := historicalFeedback(item, box)
		if err != nil {
			return err
		}
		return rspamd.Learn(ctx, cfg, f.WorkspaceID, raw, f.Classification)
	}
	stream, _, err := storage.From(ctx).Get(ctx, item.RawKey)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(stream, (20<<20)+1))
	err = errors.Join(err, stream.Close())
	if err != nil || len(raw) > 20<<20 {
		return errors.New("mail cannot be read for learning")
	}
	return rspamd.Learn(ctx, cfg, f.WorkspaceID, raw, f.Classification)
}

// Historical imported mail can lack raw MIME. Rebuild only that explicit case;
// a failed read of an existing source remains a retry, never a hidden fallback.
func historicalFeedback(item q.MailItem, box q.Mailbox) ([]byte, error) {
	var raw bytes.Buffer
	oneLine := func(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }
	writer := multipart.NewWriter(&raw)
	digest := sha256.Sum256([]byte(item.ID))
	boundary := fmt.Sprintf("thura-feedback-%x", digest[:16])
	if err := writer.SetBoundary(boundary); err != nil {
		return nil, err
	}
	fmt.Fprintf(&raw, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", oneLine(item.FromAddress), oneLine(box.Address), mime.QEncoding.Encode("utf-8", oneLine(item.Subject)), boundary)
	for _, body := range []struct{ kind, text string }{{"text/plain", item.TextBody}, {"text/html", item.HTMLBody}} {
		if body.text == "" {
			continue
		}
		part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {body.kind + "; charset=utf-8"}})
		if err != nil {
			return nil, err
		}
		if _, err = io.WriteString(part, body.text); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return raw.Bytes(), nil
}

func ReconcileFeedback(ctx context.Context) error {
	rows, err := q.New(db.From(ctx)).MailFilterFeedbackDue(ctx, lidza.Now(ctx))
	if err != nil {
		return err
	}
	for _, item := range rows {
		if _, err = jobs.From(ctx).Enqueue(ctx, FeedbackJob, schema.MailScreenJob{ID: item}, jobs.Unique(item)); err != nil {
			return err
		}
	}
	return nil
}
