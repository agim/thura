package objectgc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/packs/storage"
	"github.com/jackc/pgx/v5"
	"regexp"
	"strings"
	q "thura/db/queries/gen"
	"time"
)

const DeleteJob = "thura.storage.delete"
const SweepJob = "thura.storage.sweep"

type deletePayload struct {
	Key string `json:"key"`
}
type sweepPayload struct {
	Prefix string `json:"prefix"`
}

var allowed = regexp.MustCompile(`^drive/(files|uploads|previews)/[0-9a-f/-]*$`)

// Mail attachments are written before their row commits, so a rollback or a
// retried delivery leaves them behind. A sweep starts at one mailbox
// (mail/<box>/), never at the bare namespace, and the subdivided prefixes it
// queues stay under it. Raw .eml objects (mail/<box>/raw/) stay out:
// mail_item.raw_key has no index to check them against.
var mailAllowed = regexp.MustCompile(`^mail/[0-9a-f-]+/[0-9a-f/-]*((inbound|attachment)-[0-9]*)?$`)

func sweepable(key string) bool {
	return (allowed.MatchString(key) && len(key) <= 100) || (mailAllowed.MatchString(key) && len(key) <= 120)
}
func referenced(ctx context.Context, queries *q.Queries, key string) (bool, error) {
	if strings.HasPrefix(key, "mail/") {
		return queries.MailObjectReferenced(ctx, key)
	}
	return queries.DriveObjectReferenced(ctx, key)
}

func QueueDelete(ctx context.Context, tx pgx.Tx, key string) error {
	if !allowed.MatchString(key) || len(key) > 100 {
		return fmt.Errorf("invalid Drive object key")
	}
	h := sha256.Sum256([]byte(key))
	_, err := jobs.From(ctx).EnqueueTx(ctx, tx, DeleteJob, deletePayload{Key: key}, jobs.Unique(hex.EncodeToString(h[:])))
	return err
}
func Delete(ctx context.Context, raw json.RawMessage) error {
	var in deletePayload
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if !allowed.MatchString(in.Key) || len(in.Key) > 100 {
		return fmt.Errorf("invalid Drive object key")
	}
	referenced, err := q.New(db.From(ctx)).DriveObjectReferenced(ctx, in.Key)
	if err != nil {
		return err
	}
	if referenced {
		return fmt.Errorf("object remains referenced; refusing deletion")
	}
	return storage.From(ctx).Delete(ctx, in.Key)
}
func Sweep(ctx context.Context, raw json.RawMessage) error {
	var in sweepPayload
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if in.Prefix == "" {
		for _, prefix := range []string{"drive/files/", "drive/uploads/", "drive/previews/"} {
			if err := queueSweep(ctx, prefix); err != nil {
				return err
			}
		}
		mailboxes, err := q.New(db.From(ctx)).ListMailboxIDs(ctx)
		if err != nil {
			return err
		}
		for _, id := range mailboxes {
			if err = queueSweep(ctx, "mail/"+id+"/"); err != nil {
				return err
			}
		}
		return nil
	}
	return SweepPrefix(ctx, in.Prefix, lidza.Now(ctx).Add(-48*time.Hour))
}
func queueSweep(ctx context.Context, prefix string) error {
	_, err := jobs.From(ctx).Enqueue(ctx, SweepJob, sweepPayload{Prefix: prefix}, jobs.Unique(prefix))
	return err
}

// Prefix subdivision bounds each object listing without starving objects beyond
// the provider's first page. Random UUID keys form a fixed, finite alphabet.
func SweepPrefix(ctx context.Context, prefix string, cutoff time.Time) error {
	if !sweepable(prefix) {
		return fmt.Errorf("invalid object prefix")
	}
	objects, err := storage.From(ctx).List(ctx, prefix, 500)
	if err != nil {
		return err
	}
	if len(objects) == 500 {
		if len(prefix) >= 95 {
			return fmt.Errorf("object prefix too dense for bounded cleanup")
		}
		for _, ch := range "0123456789abcdef-/" {
			if err = queueSweep(ctx, prefix+string(ch)); err != nil {
				return err
			}
		}
		return nil
	}
	queries := q.New(db.From(ctx))
	for _, o := range objects {
		if !sweepable(o.Key) || o.ModifiedAt.IsZero() || !o.ModifiedAt.Before(cutoff) {
			continue
		}
		ref, err := referenced(ctx, queries, o.Key)
		if err != nil {
			return err
		}
		if ref {
			continue
		}
		if err = storage.From(ctx).Delete(ctx, o.Key); err != nil {
			return err
		}
	}
	return nil
}
