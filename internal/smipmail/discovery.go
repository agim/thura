package smipmail

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/platform/ingress"
	"thura/internal/smip"
)

const DiscoveryVersion = "smip-discovery/1"
const CacheLifetime = 5 * time.Minute

// RegistryCapacity bounds the domain registry per local identity, counting only
// verified and unexpired rows.
const RegistryCapacity = 1000

var ErrUnsupported = errors.New("recipient domain does not advertise SMIP mail")
var ErrDiscovery = errors.New("secure domain verification is unavailable; delivery is held")
var ErrRegistryFull = errors.New("SMIP domain registry capacity reached")

type documentClaims struct {
	Version  string              `json:"version"`
	Domain   string              `json:"domain"`
	Profile  string              `json:"profile"`
	MailPath string              `json:"mailPath"`
	Keys     map[string]smip.Key `json:"keys"`
	Issued   int64               `json:"issued"`
	Expires  int64               `json:"expires"`
	KeyID    string              `json:"keyId"`
}
type Document struct {
	documentClaims
	Signature string `json:"signature"`
}
type Peer struct {
	Domain, Origin string
	Document       Document
}

func (d Document) signingBytes() []byte {
	b, err := json.Marshal(d.documentClaims)
	if err != nil {
		panic(err)
	}
	return append([]byte("SMIP-DISCOVERY\n"), b...)
}
func (d Document) Verify(domain string, now time.Time) error {
	if d.Version != DiscoveryVersion || d.Domain != domain || !smip.ValidDomain(domain) || d.Profile != smip.MailVersion || d.MailPath != smip.MailMessagePath || len(d.Keys) < 1 || len(d.Keys) > 10 || d.Issued > now.Unix()+120 || d.Expires <= now.Unix() || d.Expires <= d.Issued || d.Expires-d.Issued > int64(CacheLifetime/time.Second) {
		return ErrDiscovery
	}
	for id, key := range d.Keys {
		if len(key.Public) != 32 || id != smip.KeyID(key.Public) || key.NotBefore < 0 || key.NotAfter <= key.NotBefore {
			return ErrDiscovery
		}
	}
	key, ok := d.Keys[d.KeyID]
	sig, err := decodeSignature(d.Signature)
	if !ok || key.Revoked || d.Issued < key.NotBefore || d.Expires > key.NotAfter || err != nil || !ed25519.Verify(key.Public, d.signingBytes(), sig) {
		return ErrDiscovery
	}
	return nil
}
func decodeSignature(raw string) ([]byte, error) {
	b, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(b) != 64 || smip.Encode(b) != raw {
		return nil, ErrDiscovery
	}
	return b, nil
}
func (g *Gateway) document(now time.Time) Document {
	expires := now.Add(CacheLifetime).Unix()
	if expires > g.signer.NotAfter {
		expires = g.signer.NotAfter
	}
	d := Document{documentClaims: documentClaims{Version: DiscoveryVersion, Domain: g.domain, Profile: smip.MailVersion, MailPath: smip.MailMessagePath, Keys: g.keys, Issued: now.Unix(), Expires: expires, KeyID: smip.KeyID(g.signer.Private.Public().(ed25519.PublicKey))}}
	d.Signature = smip.Encode(ed25519.Sign(g.signer.Private, d.signingBytes()))
	return d
}
func (g *Gateway) capabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !g.Enabled() {
		http.NotFound(w, r)
		return
	}
	if !ingress.TLS13(r) {
		http.Error(w, "TLS 1.3 required", http.StatusBadRequest)
		return
	}
	d := g.document(lidza.Now(r.Context()))
	if d.Verify(g.domain, lidza.Now(r.Context())) != nil {
		http.Error(w, "Signing key unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(d); err != nil {
		return
	}
}
func publicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16", "2001::/32"} {
		if netip.MustParsePrefix(prefix).Contains(addr) {
			return false
		}
	}
	return true
}
func (g *Gateway) client(ctx context.Context, origin string, pinned bool) (*http.Client, error) {
	if !validOrigin(origin) {
		return nil, ErrDiscovery
	}
	u, err := url.Parse(origin)
	if err != nil {
		return nil, ErrDiscovery
	}
	ips, err := g.resolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil || len(ips) == 0 {
		return nil, ErrDiscovery
	}
	if !pinned {
		for _, ip := range ips {
			if !publicIP(ip.IP) {
				return nil, ErrDiscovery
			}
		}
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	// Resolve once and dial only those validated IPs; no proxy or second DNS lookup.
	transport := &http.Transport{TLSClientConfig: g.tlsConfig(), ResponseHeaderTimeout: 5 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		for _, ip := range ips {
			connection, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return connection, nil
			}
		}
		return nil, ErrDiscovery
	}}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}
func (g *Gateway) fetch(ctx context.Context, domain string) (Peer, error) {
	if g.denied[domain] {
		return Peer{}, ErrDiscovery
	}
	origin, pinned := g.origins[domain]
	if !pinned {
		records, err := g.resolver.LookupTXT(ctx, "_smip."+domain)
		if err != nil {
			var dns *net.DNSError
			if errors.As(err, &dns) && dns.IsNotFound {
				return Peer{}, ErrUnsupported
			}
			return Peer{}, ErrDiscovery
		}
		count := 0
		for _, record := range records {
			if record == "v=SMIP1" {
				count++
			} else if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(record)), "V=SMIP") {
				return Peer{}, ErrDiscovery
			}
		}
		if count == 0 {
			return Peer{}, ErrUnsupported
		}
		if count != 1 {
			return Peer{}, ErrDiscovery
		}
		origin = "https://smip." + domain
	}
	client, err := g.client(ctx, origin, pinned)
	if err != nil {
		return Peer{}, err
	}
	defer client.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, "GET", origin+smip.DiscoveryPath, nil)
	if err != nil {
		return Peer{}, err
	}
	request.Header.Set("X-Thura-SMIP-Domain", domain)
	response, err := client.Do(request)
	if err != nil {
		return Peer{}, ErrDiscovery
	}
	defer response.Body.Close()
	// An advertised endpoint failing, redirecting or removing mail support never
	// becomes a negative capability result that would silently select a provider.
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/json" {
		return Peer{}, ErrDiscovery
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return Peer{}, ErrDiscovery
	}
	var document Document
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || decoder.Decode(new(any)) != io.EOF || document.Verify(domain, lidza.Now(ctx)) != nil {
		return Peer{}, ErrDiscovery
	}
	canonical, err := json.Marshal(document)
	if err != nil || !bytes.Equal(bytes.TrimSpace(raw), canonical) {
		return Peer{}, ErrDiscovery
	}
	return Peer{Domain: domain, Origin: origin, Document: document}, nil
}
func (g *Gateway) Resolve(ctx context.Context, domain string, force bool) (Peer, error) {
	if !g.Enabled() || !smip.ValidDomain(domain) || g.denied[domain] {
		return Peer{}, ErrDiscovery
	}
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	case <-ctx.Done():
		return Peer{}, ctx.Err()
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return Peer{}, err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	// Coalesce refreshes across nodes; the bounded client prevents an unbounded
	// transaction, and the process semaphore limits discovery connection pressure.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "smip-domain:"+g.domain+":"+domain); err != nil {
		return Peer{}, err
	}
	old, err := queries.GetSmipMailDomain(ctx, q.GetSmipMailDomainParams{Origin: g.domain, Domain: domain})
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Peer{}, err
	}
	now := lidza.Now(ctx)
	expected := "https://smip." + domain
	pinnedOrigin, pinned := g.origins[domain]
	if pinned {
		expected = pinnedOrigin
	}
	if found && !force && now.Before(old.ExpiresAt) && (old.State != "verified" || old.Endpoint == expected) && (old.State != "absent" || !pinned) {
		if old.State == "verified" {
			var d Document
			if json.Unmarshal([]byte(old.Document), &d) != nil || d.Verify(domain, now) != nil {
				return Peer{}, ErrDiscovery
			}
			return Peer{Domain: domain, Origin: old.Endpoint, Document: d}, nil
		}
		if old.State == "absent" && !old.EverVerified && !g.required[domain] {
			return Peer{}, ErrUnsupported
		}
		return Peer{}, ErrDiscovery
	}
	if !found {
		// Only sticky verifications and unexpired checks occupy capacity: an
		// unauthenticated sender naming random domains, or every provider
		// recipient ever mailed, must not lock legitimate new peers out.
		count, err := queries.CountSmipMailDomains(ctx, q.CountSmipMailDomainsParams{Origin: g.domain, ExpiresAt: now})
		if err != nil {
			return Peer{}, err
		}
		if count.Live >= RegistryCapacity {
			return Peer{}, ErrRegistryFull
		}
		if count.Total >= RegistryCapacity {
			// The table stays bounded: expired, never-verified rows make room, oldest first.
			if _, err = queries.EvictSmipMailDomains(ctx, q.EvictSmipMailDomainsParams{Origin: g.domain, ExpiresAt: now, Limit: 100}); err != nil {
				return Peer{}, err
			}
		}
	}
	peer, fetchErr := g.fetch(ctx, domain)
	state, reason, endpoint, document, verified, expires := "error", "verification_failed", "", "", false, now.Add(15*time.Second)
	if fetchErr == nil {
		raw, err := json.Marshal(peer.Document)
		if err != nil {
			return Peer{}, err
		}
		state, reason, endpoint, document, verified, expires = "verified", "", peer.Origin, string(raw), true, time.Unix(peer.Document.Expires, 0)
	} else if errors.Is(fetchErr, ErrUnsupported) && !old.EverVerified && !g.required[domain] {
		state, reason, expires = "absent", "no_smip_record", now.Add(CacheLifetime)
	}
	if err = queries.WriteSmipMailDomain(ctx, q.WriteSmipMailDomainParams{Origin: g.domain, Domain: domain, State: state, EverVerified: verified, Endpoint: endpoint, Document: document, Reason: reason, CheckedAt: now, ExpiresAt: expires}); err != nil {
		return Peer{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Peer{}, err
	}
	if state == "verified" {
		return peer, nil
	}
	if state == "absent" {
		return Peer{}, ErrUnsupported
	}
	return Peer{}, ErrDiscovery
}

// Keep protocol IDs unpredictable and independent of MIME Message-ID values.
func deliveryID() string { return uuid.NewString() }
