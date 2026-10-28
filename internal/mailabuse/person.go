package mailabuse

import (
	"context"
	"net/mail"
	"strings"

	q "thura/db/queries/gen"
	"thura/internal/labels"
)

// AutomatedReason applies the deterministic "Real person" header rules to an
// inbound message: a mailing-list, auto-submitted, bulk or no-reply message is
// automated whatever its text says, and the screening model's opinion of the
// sender is never used for it. It returns "" when the headers do not decide.
func AutomatedReason(h mail.Header) string {
	if strings.TrimSpace(h.Get("List-Unsubscribe")) != "" {
		return "List-Unsubscribe header"
	}
	if v, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(h.Get("Auto-Submitted"))), ";"); strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "no" {
		return "Auto-Submitted header"
	}
	switch strings.ToLower(strings.TrimSpace(h.Get("Precedence"))) {
	case "bulk", "list", "junk":
		return "Precedence header"
	}
	if automatedSender(h.Get("From")) {
		return "no-reply sender address"
	}
	return ""
}

// automatedSender recognises no-reply, do-not-reply and mailer-daemon style
// local parts, ignoring separators and a +tag: no-reply, no_reply+x,
// donotreply-billing, MAILER-DAEMON, postmaster, bounces.
func automatedSender(from string) bool {
	a, err := mail.ParseAddress(from)
	if err != nil {
		return false
	}
	local, _, _ := strings.Cut(strings.ToLower(a.Address), "@")
	local, _, _ = strings.Cut(local, "+")
	compact := strings.NewReplacer("-", "", "_", "", ".", "").Replace(local)
	for _, prefix := range []string{"noreply", "donotreply", "donotrespond", "mailerdaemon"} {
		if strings.HasPrefix(compact, prefix) {
			return true
		}
	}
	switch compact {
	case "postmaster", "bounce", "bounces":
		return true
	}
	return false
}

// PersonDecision is the recorded "Real person" decision of one message.
type PersonDecision struct{ State, Source, Detail string }

// decidePerson classifies a sender the headers left open. A confident model
// answer decides; without one (no model, model unsure, model skipped) the
// sender is a person when the workspace has written to the address or it is
// a saved contact.
func decidePerson(ctx context.Context, queries *q.Queries, w, sender, aiKind string) (PersonDecision, error) {
	switch aiKind {
	case "person":
		return PersonDecision{"person", "ai", "AI assessment"}, nil
	case "automated":
		return PersonDecision{"automated", "ai", "AI assessment"}, nil
	}
	if sender == "" {
		return PersonDecision{"unknown", "none", ""}, nil
	}
	rows, err := queries.MailSentToCandidates(ctx, q.MailSentToCandidatesParams{WorkspaceID: w, Address: sender})
	if err != nil {
		return PersonDecision{}, err
	}
	for _, r := range rows {
		for _, field := range []string{r.ToAddress, r.Cc, r.Bcc} {
			list, err := mail.ParseAddressList(field)
			if err != nil {
				// A malformed stored list cannot confirm the address; try the next.
				continue
			}
			for _, a := range list {
				if strings.EqualFold(a.Address, sender) {
					return PersonDecision{"person", "replied", "Your workspace has written to this address"}, nil
				}
			}
		}
	}
	contact, err := queries.MailContactAddress(ctx, q.MailContactAddressParams{WorkspaceID: w, Address: sender})
	if err != nil {
		return PersonDecision{}, err
	}
	if contact {
		return PersonDecision{"person", "contact", "Saved contact"}, nil
	}
	return PersonDecision{"unknown", "none", ""}, nil
}

// settlePerson records the decision and keeps the "Real person" tag in step
// with the message: only mail that reached the Inbox (clean or released)
// carries it, so spam and quarantined mail get it once released.
func settlePerson(ctx context.Context, queries *q.Queries, s q.MailScreening, p PersonDecision) error {
	if err := queries.SaveMailPerson(ctx, q.SaveMailPersonParams{ItemID: s.ItemID, PersonState: p.State, PersonSource: p.Source, PersonDetail: p.Detail}); err != nil {
		return err
	}
	return syncPersonLabel(ctx, queries, s.WorkspaceID, s.ItemID, p.State, s.State == "clean" || s.Released)
}
func syncPersonLabel(ctx context.Context, queries *q.Queries, w, item, state string, delivered bool) error {
	if state == "" || state == "pending" {
		return nil
	}
	return labels.ApplySystemLabel(ctx, queries, w, labels.RealPerson, item, state == "person" && delivered)
}
