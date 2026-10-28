package rspamd

import (
	"encoding/json"
	"strings"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// Envelope is the SMTP transaction observed by the receiving MTA. Rspamd needs
// it to evaluate SPF: the connecting client's IP, its HELO name and the
// envelope sender (MAIL FROM). It never comes from the message's own headers.
type Envelope struct {
	IP, Helo, MailFrom string
}

// Result values for each mechanism. NotChecked means the check did not run
// (for example, no SMTP client IP was supplied, so SPF could not be evaluated).
const (
	Pass       = "pass"
	Fail       = "fail"
	SoftFail   = "softfail"
	Neutral    = "neutral"
	None       = "none"
	TempError  = "temperror"
	PermError  = "permerror"
	NotChecked = "not_checked"
)

// Authentication is the sender authentication Rspamd computed for one message:
// SPF for the envelope sender, DKIM signatures and DMARC for the From domain.
type Authentication struct {
	FromDomain  string
	SPF         string
	SPFDomain   string
	SPFAligned  bool
	DKIM        string
	DKIMDomain  string
	DKIMAligned bool
	DMARC       string
}

// Verified reports whether the message is authentically from its From domain:
// DMARC passed, or SPF passed for an aligned envelope sender, or an aligned
// DKIM signature verified. Alignment is relaxed (same organizational domain).
func (a Authentication) Verified() bool {
	return a.FromDomain != "" && (a.DMARC == Pass || a.SPF == Pass && a.SPFAligned || a.DKIM == Pass && a.DKIMAligned)
}

type symbol struct {
	Options []string `json:"options"`
}

// Authentication extracts SPF, DKIM and DMARC from Rspamd's own symbols. It
// never reads Authentication-Results or other headers carried by the message.
func (r Result) Authentication(fromDomain string, envelope Envelope) Authentication {
	out := Authentication{FromDomain: hostname(fromDomain), SPF: NotChecked, DKIM: None, DMARC: NotChecked}
	options := func(name string) ([]string, bool) {
		raw, ok := r.Symbols[name]
		if !ok {
			return nil, false
		}
		var s symbol
		_ = json.Unmarshal(raw, &s)
		return s.Options, true
	}
	has := func(name string) bool { _, ok := options(name); return ok }

	for _, m := range []struct{ symbol, value string }{
		{"R_SPF_ALLOW", Pass}, {"R_SPF_FAIL", Fail}, {"R_SPF_SOFTFAIL", SoftFail}, {"R_SPF_NEUTRAL", Neutral},
		{"R_SPF_DNSFAIL", TempError}, {"R_SPF_PERMFAIL", PermError}, {"R_SPF_NA", None},
	} {
		// Without the SMTP client IP the result would describe Thura's own
		// connection to Rspamd, never the sender, so SPF stays unchecked.
		if envelope.IP != "" && has(m.symbol) {
			out.SPF = m.value
			break
		}
	}
	out.SPFDomain = SPFDomain(envelope, out.SPF)
	out.SPFAligned = Aligned(out.SPFDomain, out.FromDomain)

	// DKIM options are "domain:s=selector" for each signature.
	signers := func(name string) []string {
		list, _ := options(name)
		domains := []string{}
		for _, option := range list {
			domain, _, _ := strings.Cut(option, ":")
			if domain = hostname(domain); domain != "" {
				domains = append(domains, domain)
			}
		}
		return domains
	}
	if passed := signers("R_DKIM_ALLOW"); has("R_DKIM_ALLOW") {
		out.DKIM = Pass
		for _, domain := range passed {
			if out.DKIMDomain == "" || !out.DKIMAligned && Aligned(domain, out.FromDomain) {
				out.DKIMDomain = domain
				out.DKIMAligned = Aligned(domain, out.FromDomain)
			}
		}
	} else {
		for _, m := range []struct{ symbol, value string }{
			{"R_DKIM_REJECT", Fail}, {"R_DKIM_PERMFAIL", PermError}, {"R_DKIM_TEMPFAIL", TempError}, {"R_DKIM_NA", None},
		} {
			if has(m.symbol) {
				out.DKIM = m.value
				if domains := signers(m.symbol); len(domains) > 0 {
					out.DKIMDomain = domains[0]
				}
				break
			}
		}
	}

	for _, m := range []struct{ symbol, value string }{
		{"DMARC_POLICY_ALLOW", Pass}, {"DMARC_POLICY_ALLOW_WITH_FAILURES", Pass},
		{"DMARC_POLICY_REJECT", Fail}, {"DMARC_POLICY_QUARANTINE", Fail}, {"DMARC_POLICY_SOFTFAIL", Fail},
		{"DMARC_DNSFAIL", TempError}, {"DMARC_BAD_POLICY", PermError}, {"DMARC_NA", None},
	} {
		if has(m.symbol) {
			out.DMARC = m.value
			break
		}
	}
	return out
}

func hostname(raw string) string {
	raw = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if raw == "" || len(raw) > 253 {
		return ""
	}
	ascii, err := idna.Lookup.ToASCII(raw)
	if err != nil {
		return ""
	}
	return ascii
}

func organizational(host string) string {
	if d, err := publicsuffix.EffectiveTLDPlusOne(host); err == nil {
		return d
	}
	return host
}

// SPFDomain is the identity SPF evaluated: the envelope sender's domain, or
// the HELO name for a null envelope sender (bounces).
func SPFDomain(envelope Envelope, spf string) string {
	if spf == "" || spf == NotChecked {
		return ""
	}
	if _, domain, ok := strings.Cut(envelope.MailFrom, "@"); ok {
		return hostname(domain)
	}
	return hostname(envelope.Helo)
}

// Aligned is DMARC relaxed alignment: both names share an organizational domain.
func Aligned(a, b string) bool {
	a, b = hostname(a), hostname(b)
	return a != "" && b != "" && organizational(a) == organizational(b)
}
