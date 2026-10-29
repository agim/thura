package mailabuse

import (
	"context"
	"net/netip"
	"strings"
	"unicode"

	q "thura/db/queries/gen"
	"thura/internal/rspamd"
	"thura/schema"
)

// Envelope is the SMTP transaction reported by the receiving MTA (managed
// Postfix or a signed relay). It is authenticated by the relay HMAC, never
// read from the message's own headers.
type Envelope = rspamd.Envelope

const (
	authSMIP  = "smip"
	authSMTP  = "spf_dkim_dmarc"
	authNone  = "not_checked"
	maxHelo   = 255
	maxSender = 320
)

// ValidEnvelope accepts an IP literal, a printable HELO name and a printable
// envelope sender (empty for the null sender). Anything else is refused.
func ValidEnvelope(e Envelope) bool {
	ip, err := netip.ParseAddr(e.IP)
	if err != nil || ip.Zone() != "" {
		return false
	}
	return printable(e.Helo, maxHelo) && printable(e.MailFrom, maxSender)
}
func printable(value string, limit int) bool {
	if len(value) > limit {
		return false
	}
	return strings.IndexFunc(value, func(r rune) bool { return r > unicode.MaxASCII || !unicode.IsPrint(r) || r == ' ' }) < 0
}

func envelopeOf(s q.MailScreening) Envelope {
	return Envelope{IP: s.ClientAddress, Helo: s.ClientHelo, MailFrom: s.EnvelopeSender}
}

// storedAuthentication rebuilds the saved SPF/DKIM/DMARC result of a row.
func storedAuthentication(s q.MailScreening) rspamd.Authentication {
	_, domain, _ := address(s.Sender)
	a := rspamd.Authentication{FromDomain: domain, SPF: s.Spf, DKIM: s.Dkim, DKIMDomain: s.DkimDomain, DKIMAligned: s.DkimAligned, DMARC: s.Dmarc}
	a.SPFDomain = rspamd.SPFDomain(envelopeOf(s), s.Spf)
	a.SPFAligned = rspamd.Aligned(a.SPFDomain, domain)
	return a
}

// senderEvidence is the sender-authentication part of the AI evidence: the
// complete answer to whether the message is authentically from its sender.
type senderEvidence struct {
	Verified    bool
	Method      string
	FromDomain  string
	SPF         string
	SPFDomain   string
	SPFAligned  bool
	DKIM        string
	DKIMDomain  string
	DKIMAligned bool
	DMARC       string
}

func evidenceFor(smip bool, a rspamd.Authentication, checked bool) senderEvidence {
	if smip {
		return senderEvidence{Verified: true, Method: authSMIP, FromDomain: a.FromDomain}
	}
	if !checked {
		return senderEvidence{Method: authNone, FromDomain: a.FromDomain, SPF: rspamd.NotChecked, DKIM: rspamd.NotChecked, DMARC: rspamd.NotChecked}
	}
	return senderEvidence{Verified: a.Verified(), Method: authSMTP, FromDomain: a.FromDomain, SPF: a.SPF, SPFDomain: a.SPFDomain, SPFAligned: a.SPFAligned, DKIM: a.DKIM, DKIMDomain: a.DKIMDomain, DKIMAligned: a.DKIMAligned, DMARC: a.DMARC}
}

// senderAuthView is the panel's authentication line; nil until checked.
func senderAuthView(s q.MailScreening) *schema.MailSenderAuthView {
	if s.Authenticated {
		_, domain, _ := address(s.Sender)
		return &schema.MailSenderAuthView{Method: authSMIP, Verified: true, FromDomain: domain}
	}
	if s.Spf == "" {
		return nil
	}
	a := storedAuthentication(s)
	return &schema.MailSenderAuthView{Method: authSMTP, Verified: a.Verified(), FromDomain: a.FromDomain, Spf: a.SPF, SpfAligned: a.SPFAligned, Dkim: a.DKIM, DkimDomain: a.DKIMDomain, DkimAligned: a.DKIMAligned, Dmarc: a.DMARC}
}

// senderVerified reports whether this message is authentically from its From
// address's domain: SMIP, or a stored Rspamd SPF/DKIM/DMARC result that
// verifies. A message not scanned yet, or scanned without Rspamd, is not.
func senderVerified(s q.MailScreening) bool {
	return s.Authenticated || s.Spf != "" && storedAuthentication(s).Verified()
}

// authenticationFailed reports a checked, unverified result in which at least
// one mechanism reached a verdict (a fail, an unaligned pass, an error),
// rather than a sender that simply publishes nothing to check against.
func authenticationFailed(a rspamd.Authentication) bool {
	if a.Verified() {
		return false
	}
	verdict := func(v string) bool { return v != "" && v != rspamd.NotChecked && v != rspamd.None }
	return verdict(a.SPF) || verdict(a.DKIM) || verdict(a.DMARC)
}

// approvedAddress reports whether the From address is one the workspace vouches
// for: an approved (trusted) sender under either identity, or a member's own
// address. A message claiming such an address that fails authentication is
// a likely forgery.
func approvedAddress(ctx context.Context, queries *q.Queries, w, sender, rep string) (bool, error) {
	if rep == "trusted" {
		return true, nil
	}
	for _, id := range []string{identity(sender, true), identity(sender, false)} {
		r, _, err := reputation(ctx, queries, w, id)
		if err != nil {
			return false, err
		}
		if r == "trusted" {
			return true, nil
		}
	}
	accounts, err := queries.MailKnownAccounts(ctx, w)
	if err != nil {
		return false, err
	}
	for _, a := range accounts {
		if email, _, _ := address(a.Email); email != "" && email == sender {
			return true, nil
		}
	}
	return false, nil
}
