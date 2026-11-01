package smipmail

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/smip"
	"thura/internal/smiptrust"
)

// Registry is the domain-discovery engine SMIP/0.2 mail and SMIP/0.1
// chat/files share. Each profile publishes its own signed document on the
// same advertised origin: `_smip.<domain>` TXT "v=SMIP1" names
// https://smip.<domain> (or an operator-pinned origin replaces both), the
// document is fetched through the SSRF-safe TLS 1.3 client, and Verify
// checks its domain, profile, expiry and signature. Results are kept in
// smip_mail_domain, scoped by local identity and profile, with the sticky
// ever_verified flag: a domain that verified once never reads as "does not
// support SMIP" again, so losing its record or signature holds instead of
// downgrading.
type Registry struct {
	// Local is the local identity domain the registry rows belong to.
	Local string
	// Profile is "smip/0.2" (mail) or "smip/0.1" (chat/files).
	Profile string
	// Path is the profile's document path on the discovered origin.
	Path     string
	Resolver *net.Resolver
	// Roots is the deployment JSON CA pool; the saved bundle replaces it per
	// request (smiptrust.Roots).
	Roots *x509.CertPool
	// Origins are the deployment JSON pinned origins; the saved ones are
	// merged over them per request (smiptrust.MergeOrigins).
	Origins          map[string]string
	Required, Denied map[string]bool
	Slots            chan struct{}
	// Verify decodes raw as the profile's document for domain at now and
	// returns its expiry; it must reject anything not canonical, unsigned,
	// expired or naming another domain or profile.
	Verify func(raw []byte, domain string, now time.Time) (expires int64, err error)
}

// Discovered is one verified document and the origin it came from.
type Discovered struct {
	Domain, Origin string
	Document       []byte
	// Pinned is true when an operator-pinned origin replaced DNS.
	Pinned bool
}

// PinnedOrigins is the operator-pinned origin of each partner domain for
// this request: the JSON origins with the admin page's saved origins over
// them.
func (r Registry) PinnedOrigins(ctx context.Context) (map[string]string, error) {
	return smiptrust.MergeOrigins(ctx, r.Origins)
}

// Client is the SSRF-safe, TLS 1.3-only client for origin with this
// request's roots: the system roots plus the saved CA bundle, else the
// deployment JSON caFile.
func (r Registry) Client(ctx context.Context, origin string, pinned bool) (*http.Client, error) {
	roots, err := smiptrust.Roots(ctx, r.Roots)
	if err != nil {
		return nil, err
	}
	return DiscoveryClient(ctx, r.resolver(), roots, origin, pinned)
}

func (r Registry) resolver() *net.Resolver {
	if r.Resolver == nil {
		return net.DefaultResolver
	}
	return r.Resolver
}

// Advertised reads `_smip.<domain>`: exactly one "v=SMIP1" names
// https://smip.<domain>. A definitive absence is ErrUnsupported; a lookup
// failure, a duplicate or an unknown version is ErrDiscovery.
func Advertised(ctx context.Context, resolver *net.Resolver, domain string) (string, error) {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	records, err := resolver.LookupTXT(ctx, "_smip."+domain)
	if err != nil {
		var dns *net.DNSError
		if errors.As(err, &dns) && dns.IsNotFound {
			return "", ErrUnsupported
		}
		return "", ErrDiscovery
	}
	count := 0
	for _, record := range records {
		if record == "v=SMIP1" {
			count++
		} else if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(record)), "V=SMIP") {
			return "", ErrDiscovery
		}
	}
	if count == 0 {
		return "", ErrUnsupported
	}
	if count != 1 {
		return "", ErrDiscovery
	}
	return "https://smip." + domain, nil
}

// Fetch fetches and verifies domain's document through the pinned origins
// the caller already read, so the expected endpoint and the fetched one come
// from the same read.
func (r Registry) Fetch(ctx context.Context, domain string, origins map[string]string) (Discovered, error) {
	if r.Denied[domain] {
		return Discovered{}, ErrDiscovery
	}
	origin, pinned := origins[domain]
	if !pinned {
		var err error
		if origin, err = Advertised(ctx, r.resolver(), domain); err != nil {
			return Discovered{}, err
		}
	}
	client, err := r.Client(ctx, origin, pinned)
	if err != nil {
		return Discovered{}, err
	}
	defer client.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, "GET", origin+r.Path, nil)
	if err != nil {
		return Discovered{}, err
	}
	request.Header.Set("X-Thura-SMIP-Domain", domain)
	response, err := client.Do(request)
	if err != nil {
		return Discovered{}, ErrDiscovery
	}
	defer response.Body.Close()
	// An advertised endpoint failing, redirecting or removing support never
	// becomes a negative capability result.
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/json" {
		return Discovered{}, ErrDiscovery
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return Discovered{}, ErrDiscovery
	}
	raw = bytes.TrimSpace(raw)
	if _, err = r.Verify(raw, domain, lidza.Now(ctx)); err != nil {
		return Discovered{}, ErrDiscovery
	}
	return Discovered{Domain: domain, Origin: origin, Document: raw, Pinned: pinned}, nil
}

// Resolve returns domain's verified document, from the registry while it is
// fresh, else fetched and recorded. ErrUnsupported means a definitive absence
// for a domain that never verified (and is not required); every other
// failure is ErrDiscovery or ErrRegistryFull, which callers hold on.
func (r Registry) Resolve(ctx context.Context, domain string, force bool) (Discovered, error) {
	if r.Local == "" || !smip.ValidDomain(domain) || r.Denied[domain] {
		return Discovered{}, ErrDiscovery
	}
	if r.Slots != nil {
		select {
		case r.Slots <- struct{}{}:
			defer func() { <-r.Slots }()
		case <-ctx.Done():
			return Discovered{}, ctx.Err()
		}
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return Discovered{}, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	// Coalesce refreshes across nodes; the bounded client prevents an unbounded
	// transaction, and the process semaphore limits discovery connection pressure.
	lock := "smip-domain:" + r.Local + ":" + domain
	if r.Profile != smip.MailVersion {
		lock = "smip-domain:" + r.Profile + ":" + r.Local + ":" + domain
	}
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", lock); err != nil {
		return Discovered{}, err
	}
	old, err := queries.GetSmipDomain(ctx, q.GetSmipDomainParams{Origin: r.Local, Profile: r.Profile, Domain: domain})
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Discovered{}, err
	}
	now := lidza.Now(ctx)
	origins, err := r.PinnedOrigins(ctx)
	if err != nil {
		return Discovered{}, ErrDiscovery
	}
	expected := "https://smip." + domain
	pinnedOrigin, pinned := origins[domain]
	if pinned {
		expected = pinnedOrigin
	}
	if found && !force && now.Before(old.ExpiresAt) && (old.State != "verified" || old.Endpoint == expected) && (old.State != "absent" || !pinned) {
		if old.State == "verified" {
			if _, err := r.Verify([]byte(old.Document), domain, now); err != nil {
				return Discovered{}, ErrDiscovery
			}
			return Discovered{Domain: domain, Origin: old.Endpoint, Document: []byte(old.Document), Pinned: pinned}, nil
		}
		if old.State == "absent" && !old.EverVerified && !r.Required[domain] {
			return Discovered{}, ErrUnsupported
		}
		return Discovered{}, ErrDiscovery
	}
	if !found {
		// Only sticky verifications and unexpired checks occupy capacity: an
		// unauthenticated sender naming random domains, or every provider
		// recipient ever mailed, must not lock legitimate new peers out.
		count, err := queries.CountSmipDomains(ctx, q.CountSmipDomainsParams{Origin: r.Local, Profile: r.Profile, ExpiresAt: now})
		if err != nil {
			return Discovered{}, err
		}
		if count.Live >= RegistryCapacity {
			return Discovered{}, ErrRegistryFull
		}
		if count.Total >= RegistryCapacity {
			// The table stays bounded: expired, never-verified rows make room, oldest first.
			if _, err = queries.EvictSmipDomains(ctx, q.EvictSmipDomainsParams{Origin: r.Local, Profile: r.Profile, ExpiresAt: now, Limit: 100}); err != nil {
				return Discovered{}, err
			}
		}
	}
	peer, fetchErr := r.Fetch(ctx, domain, origins)
	state, reason, endpoint, document, verified, expires := "error", "verification_failed", "", "", false, now.Add(15*time.Second)
	if fetchErr == nil {
		until, err := r.Verify(peer.Document, domain, now)
		if err != nil {
			return Discovered{}, ErrDiscovery
		}
		state, reason, endpoint, document, verified, expires = "verified", "", peer.Origin, string(peer.Document), true, time.Unix(until, 0)
	} else if errors.Is(fetchErr, ErrUnsupported) && !old.EverVerified && !r.Required[domain] {
		state, reason, expires = "absent", "no_smip_record", now.Add(CacheLifetime)
	}
	if err = queries.WriteSmipDomain(ctx, q.WriteSmipDomainParams{Origin: r.Local, Profile: r.Profile, Domain: domain, State: state, EverVerified: verified, Endpoint: endpoint, Document: document, Reason: reason, CheckedAt: now, ExpiresAt: expires}); err != nil {
		return Discovered{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Discovered{}, err
	}
	if state == "verified" {
		return peer, nil
	}
	if state == "absent" {
		return Discovered{}, ErrUnsupported
	}
	return Discovered{}, ErrDiscovery
}
