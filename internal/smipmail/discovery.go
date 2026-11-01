package smipmail

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/agim/lidza"
	"github.com/google/uuid"
	"thura/internal/platform/ingress"
	"thura/internal/smip"
	"thura/internal/smiptrust"
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

// client verifies certificates against the roots of this request: the system
// roots plus the saved CA bundle, else the deployment JSON caFile.
func (g *Gateway) client(ctx context.Context, origin string, pinned bool) (*http.Client, error) {
	roots, err := smiptrust.Roots(ctx, g.roots)
	if err != nil {
		return nil, err
	}
	return DiscoveryClient(ctx, g.resolver, roots, origin, pinned)
}

// ValidOrigin reports whether raw is a bare HTTPS origin (no path, query,
// fragment or credentials), the only endpoint shape SMIP peers may use.
func ValidOrigin(raw string) bool { return validOrigin(raw) }

// DiscoveryClient is the bounded, redirect-refusing, TLS 1.3-only client every
// server-to-server fetch shares (mail discovery, SMIP/0.1 pairing, status
// probes). An origin the operator typed or pinned may be a private address;
// one derived from public DNS must resolve to public addresses only.
func DiscoveryClient(ctx context.Context, resolver *net.Resolver, roots *x509.CertPool, origin string, pinned bool) (*http.Client, error) {
	if !validOrigin(origin) {
		return nil, ErrDiscovery
	}
	u, err := url.Parse(origin)
	if err != nil {
		return nil, ErrDiscovery
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	ips, err := resolver.LookupIPAddr(ctx, u.Hostname())
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
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}, ResponseHeaderTimeout: 5 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
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

// registry is this identity's SMIP/0.2 view of the shared discovery engine.
func (g *Gateway) registry() Registry {
	return Registry{Local: g.domain, Profile: smip.MailVersion, Path: smip.DiscoveryPath, Resolver: g.resolver, Roots: g.roots, Origins: g.origins, Required: g.required, Denied: g.denied, Slots: g.slots, Verify: verifyDocument}
}

// verifyDocument is the SMIP/0.2 profile check: one canonical, signed,
// unexpired capability document for domain.
func verifyDocument(raw []byte, domain string, now time.Time) (int64, error) {
	document, err := decodeDocument(raw)
	if err != nil || document.Verify(domain, now) != nil {
		return 0, ErrDiscovery
	}
	return document.Expires, nil
}
func decodeDocument(raw []byte) (Document, error) {
	var document Document
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || decoder.Decode(new(any)) != io.EOF {
		return Document{}, ErrDiscovery
	}
	canonical, err := json.Marshal(document)
	if err != nil || !bytes.Equal(bytes.TrimSpace(raw), canonical) {
		return Document{}, ErrDiscovery
	}
	return document, nil
}
func peerOf(d Discovered) (Peer, error) {
	document, err := decodeDocument(d.Document)
	if err != nil {
		return Peer{}, ErrDiscovery
	}
	return Peer{Domain: d.Domain, Origin: d.Origin, Document: document}, nil
}

// fetch reads the pinned origins for this request and fetches the document.
func (g *Gateway) fetch(ctx context.Context, domain string) (Peer, error) {
	registry := g.registry()
	origins, err := registry.PinnedOrigins(ctx)
	if err != nil {
		return Peer{}, ErrDiscovery
	}
	found, err := registry.Fetch(ctx, domain, origins)
	if err != nil {
		return Peer{}, err
	}
	return peerOf(found)
}

// Resolve returns the verified capability document of domain through the
// shared registry (see Registry.Resolve for the downgrade rules).
func (g *Gateway) Resolve(ctx context.Context, domain string, force bool) (Peer, error) {
	if !g.Enabled() || !smip.ValidDomain(domain) || g.denied[domain] {
		return Peer{}, ErrDiscovery
	}
	found, err := g.registry().Resolve(ctx, domain, force)
	if err != nil {
		return Peer{}, err
	}
	return peerOf(found)
}

// Keep protocol IDs unpredictable and independent of MIME Message-ID values.
func deliveryID() string { return uuid.NewString() }
