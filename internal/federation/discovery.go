package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/agim/lidza/packs/db"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/smip"
	"thura/internal/smipmail"
)

// Chat/file partners are trusted the way SMIP/0.2 mail trusts its domains:
// `_smip.<domain>` TXT "v=SMIP1", the signed smip-pairing/1 identity fetched
// from https://smip.<domain> (or an operator-pinned origin) over verified
// TLS 1.3, its signature and expiry checked, and the result kept in the
// shared registry (smip_mail_domain, profile smip/0.1) with the same sticky
// ever_verified downgrade protection. The verified document's keys are the
// trusted keys until it expires; a rotated key is trusted only once a newly
// fetched, valid document publishes it. A pinned partner (smip_peer or the
// sealed JSON peers) replaces discovery for its domain entirely, including
// its revocations.
//
// Trusting a server only answers "is this really partner.example": the
// workspace's sharing policy and each recipient's acceptance decide whether
// anything reaches a person (policy.go, conversation.go).

// Discovered and unverified states of a partner domain, for views.
const (
	VerifiedPinned    = "pinned"
	VerifiedDiscovery = "discovery"
	VerifiedNone      = "unverified"
)

// partner is one chat/file peer for one request or attempt: its trusted
// keys and the origin to send to, from a pinned partner or from discovery.
type partner struct {
	Domain string
	Keys   map[string]smip.Key
	Origin string
	// Pinned marks an operator-pinned partner; PinnedOrigin a discovered one
	// reached through an operator-pinned origin (a private deployment).
	Pinned, PinnedOrigin bool
}

// registry is this identity's SMIP/0.1 view of the discovery engine mail
// uses: the same resolver, SSRF-safe client, pinned origins (the mail JSON
// origins with the saved ones over them) and CA bundle.
func (g *Gateway) registry(ctx context.Context) smipmail.Registry {
	var origins map[string]string
	if mail := smipmail.From(ctx); mail != nil {
		origins = mail.ConfiguredOrigins()
	}
	return smipmail.Registry{Local: g.domain, Profile: smip.Version, Path: IdentityPath, Resolver: g.resolver, Roots: g.roots, Origins: origins, Slots: g.discovery, Verify: verifyIdentity}
}

// verifyIdentity is the SMIP/0.1 profile check: one canonical, signed,
// unexpired identity document for domain.
func verifyIdentity(raw []byte, domain string, now time.Time) (int64, error) {
	document, err := decodeIdentity(raw)
	if err != nil || document.Verify(domain, now) != nil {
		return 0, ErrPairing
	}
	return document.Expires, nil
}
func decodeIdentity(raw []byte) (Identity, error) {
	var document Identity
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || decoder.Decode(new(any)) != io.EOF {
		return Identity{}, ErrPairing
	}
	canonical, err := json.Marshal(document)
	if err != nil || !bytes.Equal(bytes.TrimSpace(raw), canonical) {
		return Identity{}, ErrPairing
	}
	return document, nil
}

// Discover verifies domain through DNS and its signed identity, from the
// registry while the last verification is fresh. smipmail.ErrUnsupported
// means the domain never verified and publishes no record; any other error
// (lookup failure, bad TLS, bad signature, a verified domain that lost its
// record) is held, never treated as "unsupported".
func (g *Gateway) Discover(ctx context.Context, domain string, force bool) (partner, error) {
	if !g.Enabled() || domain == g.domain || !smip.ValidDomain(domain) {
		return partner{}, smipmail.ErrDiscovery
	}
	found, err := g.registry(ctx).Resolve(ctx, domain, force)
	if err != nil {
		return partner{}, err
	}
	document, err := decodeIdentity(found.Document)
	if err != nil {
		return partner{}, smipmail.ErrDiscovery
	}
	keys := make(map[string]smip.Key, len(document.Keys))
	for id, key := range document.Keys {
		keys[id] = key
	}
	return partner{Domain: domain, Keys: keys, Origin: found.Origin, PinnedOrigin: found.Pinned}, nil
}

// resolvePartner returns domain's partner for this trust snapshot: a pinned
// partner as configured (revoked keys stay refused, a receive-only partner
// has no origin), otherwise the discovered one, whose keys are added to the
// snapshot so the same read verifies the receipt.
func (g *Gateway) resolvePartner(ctx context.Context, trust *Trust, domain string) (partner, error) {
	if keys, ok := trust.Peers[domain]; ok {
		return partner{Domain: domain, Keys: keys, Origin: trust.Origins[domain], Pinned: true}, nil
	}
	p, err := g.Discover(ctx, domain, false)
	if err != nil {
		return partner{}, err
	}
	trust.Peers[domain] = p.Keys
	return p, nil
}

// partnerClient builds the sending client: a pinned partner's configured
// origin as before, a discovered one through the SSRF-safe discovery client.
func (g *Gateway) partnerClient(ctx context.Context, trust Trust, p partner) (*smip.Client, error) {
	if p.Pinned {
		return g.client(trust, p.Domain)
	}
	if p.Origin == "" {
		return nil, errors.New("SMIP partner sending endpoint unavailable")
	}
	httpClient, err := g.registry(ctx).Client(ctx, p.Origin, p.PinnedOrigin)
	if err != nil {
		return nil, err
	}
	// A 16 MiB file needs more than the 15-second discovery budget; the
	// dispatch lease (one minute) still outlives it.
	httpClient.Timeout = 30 * time.Second
	return smip.NewApplicationClientHTTP(p.Origin, p.Domain, p.Keys, httpClient)
}

// verifiedStatus is how a partner domain is trusted right now, for views:
// pinned, verified through discovery (a fresh registry row) or unverified.
func (g *Gateway) verifiedStatus(ctx context.Context, trust Trust, domain string) string {
	if keys, ok := trust.Peers[domain]; ok {
		if hasActiveKey(keys) {
			return VerifiedPinned
		}
		return VerifiedNone
	}
	row, err := q.New(db.From(ctx)).GetSmipDomain(ctx, q.GetSmipDomainParams{Origin: g.domain, Profile: smip.Version, Domain: domain})
	if errors.Is(err, pgx.ErrNoRows) || err != nil || row.State != "verified" {
		return VerifiedNone
	}
	return VerifiedDiscovery
}
