package setup

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"thura/internal/smip"
	"thura/internal/smipmail"
)

// MailIdentityView contains public metadata only, never a signing seed.
// Status is the informational DNS/transport view of the saved domain; it
// never gates publication, which relies on the recorded Secure delivery check.
// StatusDeferred means the page renders at once and the setup controls fill
// the status from smip-status.json; Status is set only when the operator
// asked for it in the page itself (the no-script recheck link).
type MailIdentityView struct {
	Domain, KeyID, Expires string
	Saved, StatusDeferred  bool
	Status                 *smipmail.Status
}

func mailIdentityView(v map[string]string) MailIdentityView {
	var cfg smipmail.Config
	if json.Unmarshal([]byte(v["SMIP_MAIL_CONFIG"]), &cfg) == nil {
		seed, err := base64.RawURLEncoding.Strict().DecodeString(cfg.Seed)
		if err == nil && len(seed) == ed25519.SeedSize {
			public := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
			return MailIdentityView{Domain: cfg.Domain, KeyID: smip.KeyID(public), Expires: time.Unix(cfg.NotAfter, 0).UTC().Format("Jan 2, 2006"), Saved: true}
		}
	}
	_, domain, _ := strings.Cut(inboxAddress(v), "@")
	return MailIdentityView{Domain: strings.ToLower(domain)}
}

// Generate once inside the revision-locked draft save. Existing identities,
// including operator-supplied trust settings, are never rotated by a form save.
func configureMailIdentity(v map[string]string, form url.Values, now time.Time) error {
	if v["SMIP_MAIL_ENABLED"] != "true" {
		return nil
	}
	domain := strings.ToLower(strings.TrimSpace(form.Get("smip_mail_domain")))
	if v["SMIP_MAIL_CONFIG"] != "" {
		if form.Has("smip_mail_domain") {
			var cfg smipmail.Config
			if json.Unmarshal([]byte(v["SMIP_MAIL_CONFIG"]), &cfg) != nil || domain != cfg.Domain {
				return errors.New("the saved SMIP identity belongs to another domain; retain its domain and manage additional domains in Team settings")
			}
		}
		return nil
	}
	if domain == "" {
		domain = mailIdentityView(v).Domain
	}
	if !smip.ValidDomain(domain) {
		return errors.New("enter a valid email identity domain, such as example.org")
	}
	_, inboxDomain, _ := strings.Cut(inboxAddress(v), "@")
	if !strings.EqualFold(domain, inboxDomain) {
		return errors.New("the SMIP email identity domain must match the inbox address domain")
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return err
	}
	cfg := smipmail.Config{Domain: domain, Seed: smip.Encode(seed), NotBefore: now.Add(-5 * time.Minute).Unix(), NotAfter: now.Add(365 * 24 * time.Hour).Unix()}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	v["SMIP_MAIL_CONFIG"] = string(raw)
	return nil
}
