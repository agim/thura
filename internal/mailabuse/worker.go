package mailabuse

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/packs/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/assistant"
	"thura/internal/rspamd"
	"thura/schema"
)

func aiReady(ctx context.Context) bool { return assistant.MailScreeningReady(ctx) }
func Screen(ctx context.Context, raw json.RawMessage) error {
	var job schema.MailScreenJob
	if json.Unmarshal(raw, &job) != nil || job.Validate() != nil {
		return errors.New("invalid mail screening job")
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	s, err := queries.LockMailScreening(ctx, job.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if terminal(s) || lidza.Now(ctx).Before(s.NextAttempt) || s.Attempts >= 5 {
		return nil
	}
	p, err := Policy(ctx, queries, s.WorkspaceID)
	if err != nil {
		return err
	}
	s.State = "scanning"
	s.Attempts++
	s.AttemptID = uuid.NewString()
	s.PolicyRevision = p.Revision
	s.NextAttempt = lidza.Now(ctx).Add(2 * time.Minute)
	if err = save(ctx, queries, s); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	// Never hold a database lock or inbound receipt transaction across an LLM/AV call.
	queries = q.New(db.From(ctx))
	item, err := queries.GetMailItem(ctx, s.ItemID)
	if err != nil {
		return err
	}
	attachments, err := queries.ListMailAttachments(ctx, s.ItemID)
	if err != nil {
		return err
	}

	directory, err := people(ctx, queries, s.WorkspaceID)
	if err != nil {
		return err
	}
	files := []File{}
	readFailure := false
	for _, file := range attachments {
		stream, _, xerr := storage.From(ctx).Get(ctx, file.ObjectKey)
		if xerr != nil {
			readFailure = true
			break
		}
		data, xerr := io.ReadAll(io.LimitReader(stream, (10<<20)+1))
		xerr = errors.Join(xerr, stream.Close())
		if xerr != nil || len(data) > 10<<20 {
			readFailure = true
			break
		}
		files = append(files, File{Name: file.Name, ContentType: file.ContentType, Data: data})
	}
	e := Analyze(item.FromAddress, item.TextBody, item.HTMLBody, directory, files)
	cfg, err := configuration()
	if err != nil {
		return err
	}
	ai, av := "disabled", "not_configured"
	// The model's view of the sender ("person", "automated" or "unsure"),
	// read from the same screening call; never a second model call.
	aiKind := ""
	failed := readFailure
	deferred := false
	locallyHeld := false
	localFailed := false
	// Sender authentication comes only from Rspamd's own SPF/DKIM/DMARC
	// evaluation (or SMIP), never from headers carried by the message.
	var auth rspamd.Authentication
	authChecked := false
	localConfig, err := rspamd.Configuration()
	if err != nil {
		return err
	}
	if localConfig.URL != "" {
		stream, _, xerr := storage.From(ctx).Get(ctx, item.RawKey)
		if xerr != nil {
			localFailed = true
		} else {
			raw, readErr := io.ReadAll(io.LimitReader(stream, (20<<20)+1))
			readErr = errors.Join(readErr, stream.Close())
			if readErr != nil || len(raw) > 20<<20 {
				localFailed = true
			} else {
				result, scanErr := rspamd.Scan(ctx, localConfig, s.WorkspaceID, raw, envelopeOf(s))
				if scanErr != nil {
					localFailed = true
				} else {
					_, fromDomain, _ := address(item.FromAddress)
					auth = result.Authentication(fromDomain, envelopeOf(s))
					authChecked = true
					locallyHeld = result.Score >= 6 || result.Action == "reject"
					for _, symbol := range []string{"PHISHING", "PHISHED_URL", "PHISHED_GENERIC_SERVICE", "PHISHED_OPENPHISH", "PHISHED_PHISHTANK"} {
						if _, ok := result.Symbols[symbol]; ok {
							locallyHeld = true
						}
					}
					if locallyHeld {
						e.add("rspamd_spam", "danger", "Local spam filter flagged this email", 100)
					}
				}
			}
		}
		if localFailed {
			failed = true
			e.add("rspamd_unavailable", "warning", "Local spam checks could not complete; retrying", 0)
		}
		// Native deterministic findings are also part of the first line of defense.
		locallyHeld = locallyHeld || e.Score >= int(p.SpamThreshold)
	}
	if readFailure {
		e.add("attachment_unavailable", "warning", "An attachment could not be inspected", 0)
		av = "error"
	}
	if cfg.Clamd != "" && !readFailure {
		stream, _, xerr := storage.From(ctx).Get(ctx, item.RawKey)
		if xerr != nil {
			av = "error"
			failed = true
		} else {
			raw, xerr := io.ReadAll(io.LimitReader(stream, (20<<20)+1))
			xerr = errors.Join(xerr, stream.Close())
			if xerr != nil || len(raw) > 20<<20 {
				av = "error"
				failed = true
			} else {
				av, xerr = ScanContent(ctx, cfg.Clamd, raw)
				if xerr != nil {
					av = "error"
					failed = true
				}
				if av == "infected" {
					e.add("antivirus_malware", "danger", "Antivirus flagged unsafe or uninspectable message content", 100)
				}
			}
		}
	}
	if av == "error" {
		e.add("antivirus_unavailable", "warning", "Required attachment checks could not complete; attachments remain quarantined", 0)
	}
	livePolicy, err := queries.GetMailProtection(ctx, s.WorkspaceID)
	if err != nil {
		return err
	}
	if p.AiEnabled && (locallyHeld || av == "infected") {
		ai = "skipped_local"
	}
	if p.AiEnabled && !locallyHeld && !localFailed && av != "infected" && livePolicy.Revision == p.Revision {
		ai = "error"
		if !authChecked {
			_, auth.FromDomain, _ = address(item.FromAddress)
		}
		payload, xerr := json.Marshal(struct {
			From, Subject, Text, HTML string
			SenderAuthentication      senderEvidence
			Files                     []string
			Findings                  []schema.MailFinding
		}{item.FromAddress, item.Subject, item.TextBody, item.HTMLBody, evidenceFor(s.Authenticated, auth, authChecked), fileNames(attachments), e.Findings})
		if xerr != nil {
			return xerr
		}
		var classification schema.MailAIClassification
		if aiReady(ctx) {
			allowed, err := reserveAI(ctx, s.WorkspaceID, cfg.AIPerHour)
			if err != nil {
				return err
			}
			if !allowed {
				deferred = true
				ai = "deferred"
				xerr = errors.New("hourly AI screening budget reached; retrying next hour")
			}
		}
		if !deferred {
			classification, xerr = assistant.ScreenMail(ctx, string(payload))
		}

		if xerr != nil {
			failed = true
			e.add("ai_unavailable", "warning", xerr.Error(), 0)
		} else {
			ai = "complete"
			if classification.SenderKind != nil {
				aiKind = *classification.SenderKind
			}
			if classification.Category == "unknown" {
				failed = true
				ai = "uncertain"
				aiKind = ""
			}
			severity := "info"
			if classification.Risk >= int(p.SpamThreshold) {
				severity = "danger"
			}
			risk := classification.Risk
			if (classification.Category == "phishing" || classification.Category == "malware") && risk < 80 {
				risk = 80
			}
			if classification.Category == "unknown" && risk < 70 {
				risk = 70
			}
			e.add("ai_assessment", severity, classification.Category+": "+classification.Explanation, risk)
		}
	}
	finish, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer finish.Rollback(ctx)
	queries = q.New(finish)
	if _, err = queries.LockWorkspace(ctx, s.WorkspaceID); err != nil {
		return err
	}
	currentItem, err := queries.LockMailItem(ctx, item.ID)
	if err != nil {
		return err
	}
	latest, err := queries.LockMailScreening(ctx, s.ItemID)
	if err != nil {
		return err
	}
	if latest.AttemptID != s.AttemptID || latest.Released {
		return nil
	}
	if authChecked && !s.Authenticated {
		if err = queries.SaveMailScreeningAuth(ctx, q.SaveMailScreeningAuthParams{ItemID: s.ItemID, Spf: auth.SPF, Dkim: auth.DKIM, DkimDomain: auth.DKIMDomain, DkimAligned: auth.DKIMAligned, Dmarc: auth.DMARC}); err != nil {
			return err
		}
	}
	current, err := Policy(ctx, queries, s.WorkspaceID)
	if err != nil {
		return err
	}
	if current.Revision != p.Revision {
		latest.State = "pending"
		latest.Attempts = 0
		latest.AiState = "pending"
		latest.NextAttempt = lidza.Now(ctx)
		if err = save(ctx, queries, latest); err != nil {
			return err
		}
		return finish.Commit(ctx)
	}
	rep, _, err := reputation(ctx, queries, s.WorkspaceID, s.Identity)
	if err != nil {
		return err
	}
	if rep == "blocked" {
		e.add("blocked_sender", "danger", "Sender is blocked by this workspace", 100)
	}
	if rep == "poor" || rep == "poor-domain" {
		e.add("sender_complaints", "danger", "Sender or domain has repeated workspace spam complaints", 80)
	}
	ownComplaints, err := queries.MailItemComplaintCount(ctx, s.ItemID)
	if err != nil {
		return err
	}
	if ownComplaints > 0 {
		e.add("reported_spam", "danger", "Workspace members reported this message as spam", 100)
	}
	latest.State = "clean"
	latest.Score = int32(e.Score)
	latest.AiState = ai
	latest.AvState = av
	latest.NextAttempt = lidza.Now(ctx).Add(retryDelay)
	known := rep == "trusted"
	if s.Authenticated {
		for _, person := range directory {
			sender, _, _ := address(person.Email)
			known = known || sender == s.Sender
		}
	}
	latest.ApprovalRequired = p.ApproveUnknown && !known
	if e.Score >= int(p.SpamThreshold) || latest.ApprovalRequired {
		latest.State = "quarantined"
	}
	if failed {
		latest.State = "error"
	}
	if deferred {
		latest.State = "pending"
		latest.Attempts--
		latest.NextAttempt = lidza.Now(ctx).UTC().Truncate(time.Hour).Add(time.Hour)
	}
	encoded, err := json.Marshal(e.Findings)
	if err != nil {
		return err
	}
	latest.Findings = string(encoded)
	if err = save(ctx, queries, latest); err != nil {
		return err
	}
	// Real person: header decisions stand; mail from before the feature ("")
	// is never classified; otherwise decide once the screening has settled.
	if latest.PersonState != "" && latest.PersonSource != "header" && (latest.State == "clean" || latest.State == "quarantined") {
		decision, err := decidePerson(ctx, queries, s.WorkspaceID, s.Sender, aiKind)
		if err != nil {
			return err
		}
		if err = settlePerson(ctx, queries, latest, decision); err != nil {
			return err
		}
	}
	// A concurrent user move/report cannot be overwritten by background release.
	if latest.State == "clean" && currentItem.Folder == q.MailFolderSpam {
		if err = queries.MoveScreenedMail(ctx, q.MoveScreenedMailParams{ID: item.ID, Folder: q.MailFolderInbox}); err != nil {
			return err
		}
	}
	return finish.Commit(ctx)
}
func fileNames(files []q.MailAttachment) []string {
	names := []string{}
	for _, file := range files {
		names = append(names, file.Name+" ("+file.ContentType+")")
	}
	return names
}
func Reconcile(ctx context.Context, _ json.RawMessage) error {
	queries := q.New(db.From(ctx))
	if err := queries.PruneMailRates(ctx, lidza.Now(ctx).Add(-48*time.Hour)); err != nil {
		return err
	}
	rows, err := queries.MailScreeningDue(ctx, lidza.Now(ctx))
	if err != nil {
		return err
	}
	for _, id := range rows {
		if _, err = jobs.From(ctx).Enqueue(ctx, ScreenJob, schema.MailScreenJob{ID: id}, jobs.Unique(id)); err != nil {
			return err
		}
	}
	return ReconcileFeedback(ctx)
}

func reserveAI(ctx context.Context, w string, limit int) (bool, error) {
	if limit < 1 || limit > 10000 {
		return false, errors.New("invalid AI screening budget")
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	count, err := q.New(tx).MailRate(ctx, q.MailRateParams{WorkspaceID: w, Key: "ai-screening", Bucket: lidza.Now(ctx).UTC().Truncate(time.Hour)})
	if err != nil {
		return false, err
	}
	if count > int32(limit) {
		return false, nil
	}
	return true, tx.Commit(ctx)
}
