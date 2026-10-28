package mailabuse

import (
	"context"
	"errors"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/jobs"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/mailaccess"
	"thura/schema"
)

func record(ctx context.Context, tx pgx.Tx, w, subject, action, resource string) error {
	return audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: action, Scope: w, Resource: resource, Meta: map[string]string{"actor": subject}})
}
func View(ctx context.Context, item, subject string) (*schema.MailScreeningView, error) {
	queries := q.New(db.From(ctx))
	i, err := queries.GetMailItem(ctx, item)
	if err != nil {
		return nil, err
	}
	box, err := queries.GetMailbox(ctx, i.MailboxID)
	if err != nil {
		return nil, err
	}
	if err = mailaccess.Check(ctx, queries, box, subject); err != nil {
		return nil, err
	}
	s, err := queries.GetMailScreening(ctx, item)
	if errors.Is(err, pgx.ErrNoRows) {
		i, err := queries.GetMailItem(ctx, item)
		if err != nil {
			return nil, err
		}
		if i.Status != "received" {
			return nil, nil
		}
		box, err := queries.GetMailbox(ctx, i.MailboxID)
		if err != nil {
			return nil, err
		}
		can, err := manager(ctx, queries, box.WorkspaceID, subject)
		if err != nil {
			return nil, err
		}
		sender, _, _ := address(i.FromAddress)
		return &schema.MailScreeningView{State: "not_screened", AiState: "not_screened", AvState: "not_screened", Sender: sender, Reputation: "unassessed", Findings: []schema.MailFinding{{Code: "historical_mail", Severity: "warning", Detail: "Received before incoming mail screening. Rescan to assess this message."}}, CanManage: can, UpdatedAt: i.UpdatedAt}, nil
	}
	if err != nil {
		return nil, err
	}
	rep, count, err := reputation(ctx, queries, s.WorkspaceID, s.Identity)
	if err != nil {
		return nil, err
	}
	can, err := manager(ctx, queries, s.WorkspaceID, subject)
	if err != nil {
		return nil, err
	}
	reported, err := queries.MailActorComplaint(ctx, q.MailActorComplaintParams{ItemID: item, Subject: subject})
	if err != nil {
		return nil, err
	}
	findings, err := parseFindings(s.Findings)
	if err != nil {
		return nil, err
	}
	feedback := ""
	f, err := queries.GetMailFilterFeedback(ctx, item)
	if err == nil {
		feedback = f.State
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return &schema.MailScreeningView{State: s.State, Score: int(s.Score), Findings: findings, AiState: s.AiState, AvState: s.AvState, Authenticated: s.Authenticated, Sender: s.Sender, Reputation: rep, Complaints: count, Reported: reported, ApprovalRequired: s.ApprovalRequired, Released: s.Released, CanManage: can, FeedbackState: feedback, UpdatedAt: s.UpdatedAt, SenderAuth: senderAuthView(s)}, nil
}

// Review is called only after mailbox.ItemAccess; it fences role changes and
// background completion, records feedback once per actor/message and never
// lets a benign model verdict override a detected virus.
func Review(ctx context.Context, w, item, subject, action string) error {
	switch action {
	case "report-spam", "not-spam", "mark-safe", "approve-sender", "block-sender", "release", "rescan":
	default:
		return router.Errorf(422, "Unknown mail review action")
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if _, err = queries.LockWorkspace(ctx, w); err != nil {
		return err
	}
	can, err := manager(ctx, queries, w, subject)
	if err != nil {
		return err
	}
	member, err := queries.IsWorkspaceMember(ctx, q.IsWorkspaceMemberParams{Scope: w, Subject: subject})
	if err != nil {
		return err
	}
	if !member {
		return router.Errorf(404, "Message not found")
	}
	if action != "report-spam" && action != "not-spam" && action != "mark-safe" && !can {
		return router.Errorf(403, "Workspace administrator required for mail approval")
	}
	i, err := queries.LockMailItem(ctx, item)
	if err != nil {
		return err
	}
	box, err := queries.GetMailbox(ctx, i.MailboxID)
	if err != nil {
		return err
	}
	if box.WorkspaceID != w {
		return router.Errorf(404, "Message not found")
	}
	if err = mailaccess.Check(ctx, queries, box, subject); err != nil {
		return err
	}
	s, err := queries.LockMailScreening(ctx, item)
	if errors.Is(err, pgx.ErrNoRows) {
		if i.Status != "received" {
			return router.Errorf(409, "Only incoming email can be reviewed")
		}
		box, xerr := queries.GetMailbox(ctx, i.MailboxID)
		if xerr != nil {
			return xerr
		}
		if box.WorkspaceID != w {
			return router.Errorf(404, "Message not found")
		}
		p, xerr := Policy(ctx, queries, w)
		if xerr != nil {
			return xerr
		}
		sender, domain, _ := address(i.FromAddress)
		cfg, xerr := configuration()
		if xerr != nil {
			return xerr
		}
		av := "not_configured"
		if cfg.Clamd != "" {
			av = "pending"
		}
		s, err = queries.AddMailScreening(ctx, q.AddMailScreeningParams{ItemID: item, WorkspaceID: w, Sender: sender, Domain: registrable(domain), Identity: identity(sender, false), State: "quarantined", Findings: "[]", AiState: "pending", AvState: av, PolicyRevision: p.Revision, NextAttempt: lidza.Now(ctx)})
	}
	if err != nil {
		return err
	}
	if s.WorkspaceID != w {
		return router.Errorf(404, "Message not found")
	}
	p, err := Policy(ctx, queries, w)
	if err != nil {
		return err
	}
	now := lidza.Now(ctx)
	switch action {
	case "report-spam", "not-spam", "mark-safe":
		if err = queries.SaveMailComplaint(ctx, q.SaveMailComplaintParams{WorkspaceID: w, ItemID: item, Identity: s.Identity, Subject: subject, Active: action == "report-spam", UpdatedAt: now}); err != nil {
			return err
		}
		if action == "report-spam" {
			s.State = "quarantined"
			s.Released = false
			s.AttemptID = ""
			i.Folder = q.MailFolderSpam
			if err = queueFeedback(ctx, tx, w, item, subject, "spam"); err != nil {
				return err
			}
		} else {
			if s.AvState == "infected" || s.AvState == "pending" || s.AvState == "error" {
				return router.Errorf(409, "Attachment safety checks must complete before marking this message safe")
			}
			rep, _, xerr := reputation(ctx, queries, w, s.Identity)
			if xerr != nil {
				return xerr
			}
			if rep == "blocked" {
				return router.Errorf(409, "Approve the blocked sender before releasing mail")
			}
			if p.ApproveUnknown && !can {
				known, xerr := knownSender(ctx, queries, w, s, rep)
				if xerr != nil {
					return xerr
				}
				if !known {
					return router.Errorf(403, "An administrator must approve this unknown sender's message")
				}
			}
			count, xerr := queries.MailItemComplaintCount(ctx, item)
			if xerr != nil {
				return xerr
			}
			if count > 0 && !can {
				return router.Errorf(409, "Other team members reported this message; an administrator must review it")
			}
			s.State = "released"
			s.Released = true
			s.AttemptID = ""
			s.ApprovalRequired = false
			if p.AiEnabled && s.AiState != "complete" {
				s.AiState = "manual_review"
			}
			i.Folder = q.MailFolderInbox
			if err = queueFeedback(ctx, tx, w, item, subject, "ham"); err != nil {
				return err
			}
		}
	case "block-sender", "approve-sender":
		decision := "blocked"
		if action == "approve-sender" {
			decision = "trusted"
		}
		if err = queries.SaveMailSenderPolicy(ctx, q.SaveMailSenderPolicyParams{WorkspaceID: w, Identity: s.Identity, Decision: decision, UpdatedBy: subject, UpdatedAt: now}); err != nil {
			return err
		}
		if action == "block-sender" {
			s.State = "quarantined"
			s.Released = false
			s.AttemptID = ""
			i.Folder = q.MailFolderSpam
		} else {
			s.ApprovalRequired = false
		}
	case "release":
		rep, _, err := reputation(ctx, queries, w, s.Identity)
		if err != nil {
			return err
		}
		if rep == "blocked" {
			return router.Errorf(409, "Approve the blocked sender before releasing mail")
		}
		if s.AvState == "infected" {
			return router.Errorf(409, "Antivirus detected malware; this attachment cannot be released")
		}
		if (p.AiEnabled && s.AiState != "complete" && s.AiState != "skipped_local") || s.AvState == "pending" || s.AvState == "error" || s.State == "scanning" {
			return router.Errorf(409, "Required scans have not completed; rescan after fixing the service or changing settings")
		}
		if p.ApproveUnknown {
			known, err := knownSender(ctx, queries, w, s, rep)
			if err != nil {
				return err
			}
			if !known {
				return router.Errorf(409, "Approve this unknown sender before releasing the message")
			}
		}
		s.Released = true
		s.State = "released"
		s.AttemptID = ""
		i.Folder = q.MailFolderInbox
		if err = queueFeedback(ctx, tx, w, item, subject, "ham"); err != nil {
			return err
		}
	case "rescan":
		s.State = "pending"
		s.Released = false
		s.Attempts = 0
		s.AttemptID = ""
		s.AiState = "pending"
		s.AvState = "pending"
		s.NextAttempt = now
		s.PolicyRevision = p.Revision
		i.Folder = q.MailFolderSpam
		if _, err = jobs.From(ctx).EnqueueTx(ctx, tx, ScreenJob, schema.MailScreenJob{ID: item}, jobs.Unique(item+":"+now.Format(time.RFC3339Nano))); err != nil {
			return err
		}
	}
	if err = save(ctx, queries, s); err != nil {
		return err
	}
	if err = queries.MoveScreenedMail(ctx, q.MoveScreenedMailParams{ID: item, Folder: i.Folder}); err != nil {
		return err
	}
	if err = record(ctx, tx, w, subject, "mail.review."+action, item); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func GuardMove(ctx context.Context, item string, folder q.MailFolder) error {
	if folder == q.MailFolderSpam || folder == q.MailFolderTrash {
		return nil
	}
	s, err := q.New(db.From(ctx)).GetMailScreening(ctx, item)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.State != "clean" && !s.Released {
		return router.Errorf(409, "Use the mail protection review actions to release quarantined email")
	}
	return nil
}

func HoldMove(ctx context.Context, queries *q.Queries, s q.MailScreening) error {
	s.State = "quarantined"
	s.Released = false
	s.AttemptID = ""
	return save(ctx, queries, s)
}

func knownSender(ctx context.Context, queries *q.Queries, workspace string, screening q.MailScreening, rep string) (bool, error) {
	if rep == "trusted" {
		return true, nil
	}
	if !screening.Authenticated {
		return false, nil
	}
	directory, err := people(ctx, queries, workspace)
	if err != nil {
		return false, err
	}
	for _, person := range directory {
		sender, _, _ := address(person.Email)
		if sender == screening.Sender {
			return true, nil
		}
	}
	return false, nil
}
