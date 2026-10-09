package setup

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	mailpack "github.com/agim/lidza/packs/mail"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/credentials"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
)

const probePrefix = "_thura_setup_checks/"

// A keyed digest binds results to the exact provider settings without storing
// credentials or exposing a digest suitable for guessing a weak password.
func probeFingerprint(v map[string]string, kind string) (string, error) {
	selected := map[string]string{"kind": kind}
	var names []string
	switch kind {
	case "mail":
		names = []string{"MAIL_PROVIDER", "MAIL_FROM"}
		if v["MAIL_PROVIDER"] == "smtp" {
			names = append(names, "MAIL_SMTP_HOST", "MAIL_SMTP_PORT", "MAIL_SMTP_SECURITY", "MAIL_SMTP_USERNAME", "MAIL_SMTP_PASSWORD")
		} else {
			names = append(names, "MAIL_API_KEY", "MAIL_DOMAIN", "MAIL_REGION")
		}
	case "storage":
		names = []string{"STORAGE_PROVIDER", "STORAGE_PREFIX"}
		if v["STORAGE_PROVIDER"] == "local" {
			names = append(names, "STORAGE_DIR")
		} else {
			names = append(names, "STORAGE_ENDPOINT", "STORAGE_REGION", "STORAGE_BUCKET", "STORAGE_ACCESS_KEY", "STORAGE_SECRET_KEY")
		}
	default:
		return "", errors.New("unknown provider check")
	}
	for _, name := range names {
		selected[name] = v[name]
	}
	key, err := credentials.Key(".")
	if err != nil {
		return "", errors.New("server master key is unavailable")
	}
	body, err := json.Marshal(selected)
	if err != nil {
		return "", err
	}
	digest := hmac.New(sha256.New, key)
	digest.Write([]byte("thura.setup.probe.v1\x00"))
	digest.Write(body)
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func probeMessage(kind, state string) string {
	switch state {
	case "succeeded":
		if kind == "mail" {
			return "Mail provider accepted the test email. Confirm it arrived in your administrator inbox; acceptance does not establish delivery."
		}
		return "Storage write, read, metadata, listing and deletion checks passed. Bucket privacy, durability and backups still require deployment verification."
	case "superseded":
		return "A later check replaced this result. Complete the newer check before publishing."
	case "failed":
		return "Provider check failed or timed out. Verify the saved settings and provider logs before starting a new check. Email acceptance may be uncertain; this attempt will not be resent."
	case "cleanup_required":
		return "Storage check could not confirm deletion. Remove only the object _thura_setup_checks/<check ID> under the configured prefix, then retry with a new check."
	default:
		return "Check started; its outcome is not recorded yet or is uncertain. Reload to inspect it. This check ID will not repeat the provider operation."
	}
}

// Probe durably commits intent and audit before network contact. Replays of a
// form ID never repeat a mail send or storage operation, including after a
// crash, client disconnect or failed completion commit.
func Probe(ctx context.Context, kind, revision, requestID string) (string, error) {
	id, err := uuid.Parse(requestID)
	if err != nil || id == uuid.Nil {
		return "", errors.New("reload the setup page to obtain a valid check ID")
	}
	tx, queries, row, err := lock(ctx, revision)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	subject := auth.CurrentUser(ctx).ID
	prior, err := queries.GetSetupProbe(ctx, id.String())
	if err == nil {
		if prior.Kind != kind || prior.Subject != subject || prior.Revision != row.Revision {
			return "", router.Errorf(409, "check ID already belongs to another request; reload")
		}
		return probeMessage(prior.Kind, prior.State), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	v, err := unseal(row.Draft)
	if err != nil {
		return "", err
	}
	if err = validate(v); err != nil {
		return "", err
	}
	fingerprint, err := probeFingerprint(v, kind)
	if err != nil {
		return "", err
	}
	now := lidza.Now(ctx)
	count, err := queries.CountRecentSetupProbes(ctx, q.CountRecentSetupProbesParams{Subject: subject, Kind: kind, CreatedAt: now.Add(-15 * time.Minute)})
	if err != nil {
		return "", err
	}
	if count >= 3 {
		return "", router.Errorf(429, "at most three checks per provider in fifteen minutes; wait before starting a new check")
	}
	// Recipient is server-owned account data, never an arbitrary form address.
	recipient := ""
	if kind == "mail" {
		profile, err := auth.From(ctx).Profile(ctx, subject)
		if err != nil {
			return "", errors.New("administrator account could not be loaded")
		}
		address, err := mail.ParseAddress(profile.Email)
		if err != nil || address.Address != profile.Email {
			return "", errors.New("administrator needs a valid plain email address")
		}
		recipient = profile.Email
	}
	if err = queries.SupersedeSetupProbeSuccesses(ctx, q.SupersedeSetupProbeSuccessesParams{Kind: kind, Fingerprint: fingerprint}); err != nil {
		return "", err
	}
	if err = queries.CreateSetupProbe(ctx, q.CreateSetupProbeParams{ID: id.String(), Kind: kind, Subject: subject, Revision: row.Revision, Fingerprint: fingerprint, CreatedAt: now}); err != nil {
		return "", err
	}
	if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "server.setup.check.start", Resource: "server-setup", Meta: map[string]string{"kind": kind, "check_id": id.String(), "revision": fmt.Sprint(row.Revision)}}); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	state := "failed"
	if kind == "mail" {
		if checkMail(bounded, v, recipient) == nil {
			state = "succeeded"
		}
	} else {
		state = checkStorage(bounded, v, id.String())
	}
	// Complete independently of a disconnected browser, with a short deadline.
	finish, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	done, err := db.From(ctx).Begin(finish)
	if err != nil {
		return "", errors.New("check outcome could not be recorded; do not assume it failed or resend automatically")
	}
	defer done.Rollback(finish)
	if err = q.New(done).FinishSetupProbe(finish, q.FinishSetupProbeParams{ID: id.String(), State: state, UpdatedAt: lidza.Now(ctx)}); err == nil {
		err = audit.From(ctx).RecordTx(finish, done, audit.Event{Action: "server.setup.check.finish", Resource: "server-setup", Meta: map[string]string{"kind": kind, "check_id": id.String(), "state": state}})
	}
	if err == nil {
		err = done.Commit(finish)
	}
	if err != nil {
		return "", errors.New("check outcome could not be recorded; reload and inspect the check ID before trying again")
	}
	return probeMessage(kind, state), nil
}

func checkMail(ctx context.Context, v map[string]string, recipient string) error {
	var cfg mailpack.Config
	if err := env.Fill(&cfg, v); err != nil {
		return err
	}
	cfg.SMTPTimeout = 15 * time.Second
	client, err := mailpack.New(cfg, nil, nil)
	if err != nil {
		return err
	}
	_, err = client.Send(ctx, mailpack.Message{To: recipient, Subject: "Thura server setup: email check", Text: "Your administrator requested this test from the Thura setup wizard. Receiving it confirms delivery to this inbox. No credentials are included."})
	return err
}

func checkStorage(ctx context.Context, v map[string]string, id string) (state string) {
	var cfg storage.Config
	if env.Fill(&cfg, v) != nil {
		return "failed"
	}
	cfg.Timeout = 15 * time.Second
	client, err := storage.New(cfg)
	if err != nil {
		return "failed"
	}
	key := probePrefix + id
	// Delete even after an ambiguous Put; the key is unique to this intent.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := client.Delete(cleanup, key); err != nil && !errors.Is(err, storage.ErrNotFound) {
			state = "cleanup_required"
			return
		}
		if _, err := client.Stat(cleanup, key); !errors.Is(err, storage.ErrNotFound) {
			state = "cleanup_required"
		}
	}()
	payload := []byte("Thura private storage setup check\n")
	if _, err := client.Put(ctx, key, bytes.NewReader(payload), storage.PutOptions{ContentType: "text/plain"}); err != nil {
		return "failed"
	}
	reader, object, err := client.Get(ctx, key)
	if err != nil {
		return "failed"
	}
	body, err := io.ReadAll(io.LimitReader(reader, int64(len(payload)+1)))
	closeErr := reader.Close()
	if err != nil || closeErr != nil || !bytes.Equal(body, payload) || object.Size != int64(len(payload)) {
		return "failed"
	}
	object, err = client.Stat(ctx, key)
	if err != nil || object.Size != int64(len(payload)) {
		return "failed"
	}
	objects, err := client.List(ctx, key, 2)
	if err != nil {
		return "failed"
	}
	for _, object := range objects {
		if object.Key == key {
			return "succeeded"
		}
	}
	return "failed"
}

func requireProviderChecks(ctx context.Context, queries *q.Queries, v map[string]string) error {
	for _, kind := range []string{"mail", "storage"} {
		fingerprint, err := probeFingerprint(v, kind)
		if err != nil {
			return err
		}
		ok, err := queries.HasSuccessfulSetupProbe(ctx, q.HasSuccessfulSetupProbeParams{Kind: kind, Fingerprint: fingerprint, CreatedAt: lidza.Now(ctx).Add(-24 * time.Hour)})
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("run a successful %s check within the last 24 hours for the saved provider settings before publishing", kind)
		}
	}
	return nil
}

type ProbeView struct {
	CreatedAt                time.Time
	ID, Kind, State, Message string
	Revision                 int32
	Current                  bool
}

func probeViews(ctx context.Context, v map[string]string) ([]ProbeView, error) {
	rows, err := q.New(db.From(ctx)).ListSetupProbes(ctx)
	if err != nil {
		return nil, err
	}
	fingerprints := map[string]string{}
	for _, kind := range []string{"mail", "storage"} {
		fingerprints[kind], err = probeFingerprint(v, kind)
		if err != nil {
			return nil, err
		}
	}
	out := make([]ProbeView, 0, len(rows))
	for _, row := range rows {
		out = append(out, ProbeView{CreatedAt: row.CreatedAt, ID: row.ID, Kind: row.Kind, State: row.State, Message: probeMessage(row.Kind, row.State), Revision: row.Revision, Current: row.Fingerprint == fingerprints[row.Kind] && row.CreatedAt.After(lidza.Now(ctx).Add(-24*time.Hour))})
	}
	return out, nil
}
