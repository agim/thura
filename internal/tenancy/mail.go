package tenancy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/mail"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	"os"
	"strings"
	q "thura/db/queries/gen"
	"thura/internal/smip"
	"thura/schema"
	"time"
)

// MailSecrets is internal-only. No HTTP response contains this structure.
// Workspace/domain binding is inside the authenticated ciphertext as well.
type MailSecrets struct {
	WorkspaceID   string
	DomainID      string
	Config        mail.Config
	Seed          string
	NotBefore     int64
	NotAfter      int64
	Keys          map[string]smip.Key
	InboundSecret string
}

func LoadMail(ctx context.Context, id, domainID string) (q.TeamMailConfig, MailSecrets, error) {
	row, e := q.New(db.From(ctx)).GetTeamMailConfig(ctx, q.GetTeamMailConfigParams{WorkspaceID: id, DomainID: domainID})
	var secret MailSecrets
	if e == nil {
		e = Open(row.Sealed, &secret)
		if e == nil && (secret.WorkspaceID != id || secret.DomainID != domainID) {
			e = errors.New("team secret binding mismatch")
		}
	}
	return row, secret, e
}
func SaveMail(ctx context.Context, id, domainID string, in schema.TeamMailInput) (schema.TeamMailView, error) {
	var out schema.TeamMailView
	tx, queries, e := management(ctx, id)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if !validID(domainID) {
		return out, router.Errorf(404, "Domain not found")
	}
	d, e := queries.GetTeamDomain(ctx, q.GetTeamDomainParams{WorkspaceID: id, ID: domainID})
	if e != nil {
		return out, router.Errorf(404, "Domain not found")
	}
	if !d.Mail || !Active(d, lidza.Now(ctx)) {
		return out, router.Errorf(409, "Verify this email domain before configuring sending")
	}
	secret := MailSecrets{WorkspaceID: id, DomainID: domainID}
	previous, e := queries.GetTeamMailConfig(ctx, q.GetTeamMailConfigParams{WorkspaceID: id, DomainID: domainID})
	if e == nil {
		if e = Open(previous.Sealed, &secret); e != nil {
			return out, e
		}
		if secret.WorkspaceID != id || secret.DomainID != domainID {
			return out, errors.New("team secret binding mismatch")
		}
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	cfg := mail.Config{Provider: in.Provider, Domain: d.Name, Region: in.Region, SMTPHost: text(in.SmtpHost), SMTPPort: in.SmtpPort, SMTPUsername: text(in.SmtpUsername), SMTPSecurity: text(in.SmtpSecurity), SMTPTimeout: 30 * time.Second, MaxAttempts: 1, MaxRecipients: 50, MaxAttachments: 10, MaxAttachmentBytes: 10 << 20}
	// Preserve redacted credentials only for the same provider. Switching provider
	// requires its own credentials; never hand a prior vendor's key to a new one.
	if in.Provider == previous.Provider {
		cfg.APIKey = secret.Config.APIKey
		cfg.SMTPPassword = secret.Config.SMTPPassword
	}
	if in.APIKey != nil {
		cfg.APIKey = *in.APIKey
	}
	if in.SmtpPassword != nil {
		cfg.SMTPPassword = *in.SmtpPassword
	}
	switch cfg.Provider {
	case "mailgun", "sendgrid", "postmark", "resend":
		if cfg.APIKey == "" {
			return out, router.Errorf(422, "An API key is required")
		}
		if cfg.Region != "" && cfg.Region != "us" && cfg.Region != "eu" {
			return out, router.Errorf(422, "Choose the US or EU API region")
		}
		cfg.SMTPHost = ""
		cfg.SMTPUsername = ""
		cfg.SMTPPassword = ""
		cfg.SMTPPort = 0
	case "postfix":
		if len(os.Getenv("THURA_POSTFIX_TOKEN")) < 32 {
			return out, router.Errorf(409, "The operator must enable the included Postfix service first")
		}
		cfg.Provider = "smtp"
		cfg.SMTPHost = "127.0.0.1"
		cfg.SMTPPort = 2525
		cfg.SMTPSecurity = "none"
		cfg.SMTPUsername = ""
		cfg.SMTPPassword = ""
		cfg.APIKey = ""
	case "smtp":
		allowed := false
		for _, host := range strings.Split(os.Getenv("THURA_TENANT_SMTP_HOSTS"), ",") {
			allowed = allowed || strings.EqualFold(strings.TrimSpace(host), cfg.SMTPHost) && cfg.SMTPHost != ""
		}
		if !allowed {
			return out, router.Errorf(422, "Ask the operator to allow this SMTP host in THURA_TENANT_SMTP_HOSTS")
		}
		if cfg.SMTPPort < 1 || cfg.SMTPPort > 65535 || (cfg.SMTPSecurity != "tls" && cfg.SMTPSecurity != "starttls") {
			return out, router.Errorf(422, "SMTP requires a valid port and TLS or STARTTLS")
		}
		cfg.APIKey = ""
	default:
		return out, router.Errorf(422, "Choose Mailgun, SendGrid, Postmark, Resend, SMTP or Postfix")
	}
	if in.Postfix && len(os.Getenv("THURA_POSTFIX_TOKEN")) < 32 {
		return out, router.Errorf(409, "The operator must enable Postfix receiving first")
	}
	if _, e = mail.New(cfg, nil, nil); e != nil {
		return out, router.Errorf(422, "Mail provider configuration is incomplete")
	}
	secret.Config = cfg
	if secret.Seed == "" {
		raw := make([]byte, 32)
		if _, e = rand.Read(raw); e != nil {
			return out, e
		}
		secret.Seed = base64.RawURLEncoding.EncodeToString(raw)
		secret.NotBefore = lidza.Now(ctx).Add(-time.Minute).Unix()
		secret.NotAfter = lidza.Now(ctx).Add(365 * 24 * time.Hour).Unix()
	}
	if in.InboundSecret != nil {
		if len(*in.InboundSecret) < 32 {
			return out, router.Errorf(422, "Relay signing secret must contain at least 32 characters")
		}
		secret.InboundSecret = *in.InboundSecret
	}
	if secret.InboundSecret == "" {
		raw := make([]byte, 32)
		if _, e = rand.Read(raw); e != nil {
			return out, e
		}
		secret.InboundSecret = base64.RawURLEncoding.EncodeToString(raw)
	}
	sealed, e := Seal(secret)
	if e != nil {
		return out, e
	}
	row, e := queries.SaveTeamMailConfig(ctx, q.SaveTeamMailConfigParams{WorkspaceID: id, DomainID: domainID, Provider: in.Provider, Sealed: sealed, Enabled: in.Enabled, Smip: in.Smip, Postfix: in.Postfix})
	if e != nil {
		return out, e
	}
	if e = record(ctx, tx, id, "team.mail.configure", domainID); e != nil {
		return out, e
	}
	return MailView(row, secret), tx.Commit(ctx)
}
func MailView(row q.TeamMailConfig, s MailSecrets) schema.TeamMailView {
	out := schema.TeamMailView{DomainID: row.DomainID, Provider: row.Provider, Region: s.Config.Region, SmtpHost: s.Config.SMTPHost, SmtpPort: s.Config.SMTPPort, SmtpUsername: s.Config.SMTPUsername, SmtpSecurity: s.Config.SMTPSecurity, HasAPIKey: s.Config.APIKey != "", HasSmtpPassword: s.Config.SMTPPassword != "", Enabled: row.Enabled, Smip: row.Smip, Postfix: row.Postfix}
	seed, e := base64.RawURLEncoding.DecodeString(s.Seed)
	if e == nil && len(seed) == 32 {
		pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
		out.PublicKey = base64.RawURLEncoding.EncodeToString(pub)
		out.KeyID = smip.KeyID(pub)
		out.KeyExpiresAt = ptrTime(time.Unix(s.NotAfter, 0))
	}
	return out
}

// ResolveMail returns managed=true even when a team configuration is missing or
// disabled. Callers must never fall back to deployment credentials in that case.
func ResolveMail(ctx context.Context, m q.Mailbox) (cfg mail.Config, managed bool, e error) {
	_, name, ok := strings.Cut(strings.ToLower(m.Address), "@")
	if !ok {
		return cfg, false, errors.New("invalid mailbox address")
	}
	queries := q.New(db.From(ctx))
	setup, setupErr := queries.GetTeamSetup(ctx, m.WorkspaceID)
	d, err := queries.BoundTeamDomain(ctx, name)
	if errors.Is(err, pgx.ErrNoRows) && errors.Is(setupErr, pgx.ErrNoRows) {
		return cfg, false, nil
	}
	managed = true
	if err != nil || d.WorkspaceID != m.WorkspaceID || !d.Mail || !Active(d, lidza.Now(ctx)) {
		return cfg, managed, router.Errorf(409, "Email domain is not currently verified for this team")
	}
	if setupErr != nil || setup.Status != "ready" {
		return cfg, managed, router.Errorf(409, "Complete team setup before sending")
	}
	row, s, err := LoadMail(ctx, m.WorkspaceID, d.ID)
	if err != nil || !row.Enabled {
		return cfg, managed, router.Errorf(409, "Team mail provider is unavailable or disabled")
	}
	cfg = s.Config
	cfg.From = m.Address
	cfg.AppURL = PrimaryURL(ctx, m.WorkspaceID)
	return cfg, managed, nil
}
func InboundSecret(ctx context.Context, m q.Mailbox) (string, bool, error) {
	_, name, _ := strings.Cut(strings.ToLower(m.Address), "@")
	queries := q.New(db.From(ctx))
	_, setupErr := queries.GetTeamSetup(ctx, m.WorkspaceID)
	d, e := queries.BoundTeamDomain(ctx, name)
	if errors.Is(e, pgx.ErrNoRows) && errors.Is(setupErr, pgx.ErrNoRows) {
		return "", false, nil
	}
	if e != nil || d.WorkspaceID != m.WorkspaceID || !d.Mail || !Active(d, lidza.Now(ctx)) {
		return "", true, errors.New("team receiving domain is not verified")
	}
	row, s, e := LoadMail(ctx, m.WorkspaceID, d.ID)
	if e != nil || !row.Enabled {
		return "", true, errors.New("team receiving configuration unavailable")
	}
	return s.InboundSecret, true, nil
}

func RotateIdentity(ctx context.Context, id, domainID string) (schema.TeamMailView, error) {
	var out schema.TeamMailView
	tx, queries, e := management(ctx, id)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if !validID(domainID) {
		return out, router.Errorf(404, "Domain not found")
	}
	d, e := queries.GetTeamDomain(ctx, q.GetTeamDomainParams{WorkspaceID: id, ID: domainID})
	if e != nil || !Active(d, lidza.Now(ctx)) {
		return out, router.Errorf(409, "Verify domain before rotating its identity")
	}
	row, e := queries.GetTeamMailConfig(ctx, q.GetTeamMailConfigParams{WorkspaceID: id, DomainID: domainID})
	if e != nil {
		return out, router.Errorf(404, "Mail configuration not found")
	}
	var s MailSecrets
	if e = Open(row.Sealed, &s); e != nil {
		return out, e
	}
	if s.WorkspaceID != id || s.DomainID != domainID {
		return out, errors.New("team secret binding mismatch")
	}
	old, e := base64.RawURLEncoding.DecodeString(s.Seed)
	if e != nil || len(old) != 32 {
		return out, errors.New("invalid previous identity")
	}
	if s.Keys == nil {
		s.Keys = map[string]smip.Key{}
	}
	now := lidza.Now(ctx)
	for key, k := range s.Keys {
		if k.NotAfter <= now.Unix() {
			delete(s.Keys, key)
		}
	}
	if len(s.Keys) >= 8 {
		return out, router.Errorf(409, "Too many active historical identities; wait for older keys to expire")
	}
	pub := ed25519.NewKeyFromSeed(old).Public().(ed25519.PublicKey)
	s.Keys[smip.KeyID(pub)] = smip.Key{Public: pub, NotBefore: s.NotBefore, NotAfter: s.NotAfter}
	raw := make([]byte, 32)
	if _, e = rand.Read(raw); e != nil {
		return out, e
	}
	s.Seed = base64.RawURLEncoding.EncodeToString(raw)
	s.NotBefore = now.Add(-time.Minute).Unix()
	s.NotAfter = now.Add(365 * 24 * time.Hour).Unix()
	sealed, e := Seal(s)
	if e != nil {
		return out, e
	}
	row, e = queries.SaveTeamMailConfig(ctx, q.SaveTeamMailConfigParams{WorkspaceID: id, DomainID: domainID, Provider: row.Provider, Sealed: sealed, Enabled: row.Enabled, Smip: row.Smip, Postfix: row.Postfix})
	if e != nil {
		return out, e
	}
	if e = record(ctx, tx, id, "team.smip.rotate", domainID); e != nil {
		return out, e
	}
	return MailView(row, s), tx.Commit(ctx)
}

// PostfixSecret is used only by the private authenticated sidecar endpoint.
func PostfixSecret(ctx context.Context, m q.Mailbox) (string, bool, error) {
	secret, managed, e := InboundSecret(ctx, m)
	if !managed || e != nil {
		return secret, managed, e
	}
	_, name, _ := strings.Cut(strings.ToLower(m.Address), "@")
	queries := q.New(db.From(ctx))
	d, e := queries.BoundTeamDomain(ctx, name)
	if e != nil {
		return "", true, e
	}
	row, _, e := LoadMail(ctx, m.WorkspaceID, d.ID)
	if e != nil || !row.Postfix {
		return "", true, errors.New("postfix receiving not enabled")
	}
	setup, e := queries.GetTeamSetup(ctx, m.WorkspaceID)
	if e != nil || setup.Status != "ready" {
		return "", true, errors.New("team setup not published")
	}
	return secret, true, nil
}

func text(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
