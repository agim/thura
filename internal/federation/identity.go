package federation

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/agim/lidza"
	"thura/internal/platform/ingress"
	"thura/internal/smip"
	"thura/internal/smipmail"
	"thura/internal/smiptrust"
)

// PairingVersion names the signed identity document SMIP/0.1 operators pair
// with; IdentityPath serves it. The document is public, short-lived and never
// carries a seed: it lets the other operator fetch the keys, while the
// fingerprint read-out between the two operators is what establishes trust.
const PairingVersion = "smip-pairing/1"
const IdentityPath = "/api/v1/smip/v0.1/identity"
const IdentityLifetime = 5 * time.Minute

// MaxPeers bounds paired domains and MaxPeerKeys the keys one document lists.
const MaxPeers = 100
const MaxPeerKeys = 10

var ErrPairing = errors.New("the peer's identity document could not be fetched or verified")

type identityClaims struct {
	Version     string              `json:"version"`
	Domain      string              `json:"domain"`
	Profile     string              `json:"profile"`
	MessagePath string              `json:"messagePath"`
	Keys        map[string]smip.Key `json:"keys"`
	Issued      int64               `json:"issued"`
	Expires     int64               `json:"expires"`
	KeyID       string              `json:"keyId"`
}

// Identity is the signed identity document of one SMIP/0.1 server.
type Identity struct {
	identityClaims
	Signature string `json:"signature"`
}

func (d Identity) signingBytes() []byte {
	b, err := json.Marshal(d.identityClaims)
	if err != nil {
		panic(err)
	}
	return append([]byte("SMIP-PAIRING\n"), b...)
}

// Verify checks the document for domain at now: version, profile, endpoint
// path, key shapes, the issue/expiry window and the signature by one of its
// own current keys.
func (d Identity) Verify(domain string, now time.Time) error {
	if d.Version != PairingVersion || d.Domain != domain || !smip.ValidDomain(domain) || d.Profile != smip.Version || d.MessagePath != smip.ApplicationMessagePath || len(d.Keys) < 1 || len(d.Keys) > MaxPeerKeys || d.Issued > now.Unix()+120 || d.Expires <= now.Unix() || d.Expires <= d.Issued || d.Expires-d.Issued > int64(IdentityLifetime/time.Second) {
		return ErrPairing
	}
	for id, key := range d.Keys {
		if len(key.Public) != ed25519.PublicKeySize || id != smip.KeyID(key.Public) || key.NotBefore < 0 || key.NotAfter <= key.NotBefore {
			return ErrPairing
		}
	}
	key, ok := d.Keys[d.KeyID]
	sig, err := base64.RawURLEncoding.Strict().DecodeString(d.Signature)
	if !ok || key.Revoked || d.Issued < key.NotBefore || d.Expires > key.NotAfter || err != nil || len(sig) != ed25519.SignatureSize || smip.Encode(sig) != d.Signature || !ed25519.Verify(key.Public, d.signingBytes(), sig) {
		return ErrPairing
	}
	return nil
}

// Identity signs this server's current document: the active signing key and
// the retained historical keys with their revocation flags.
func (g *Gateway) Identity(now time.Time) Identity {
	expires := now.Add(IdentityLifetime).Unix()
	if expires > g.signer.NotAfter {
		expires = g.signer.NotAfter
	}
	keys := map[string]smip.Key{}
	for id, key := range g.originKeys {
		keys[id] = key
	}
	d := Identity{identityClaims: identityClaims{Version: PairingVersion, Domain: g.domain, Profile: smip.Version, MessagePath: smip.ApplicationMessagePath, Keys: keys, Issued: now.Unix(), Expires: expires, KeyID: smip.KeyID(g.signer.Private.Public().(ed25519.PublicKey))}}
	d.Signature = smip.Encode(ed25519.Sign(g.signer.Private, d.signingBytes()))
	return d
}

// IdentityDocument serves GET /api/v1/smip/v0.1/identity. It needs nothing
// secret, answers only over direct TLS 1.3 and is 503 while SMIP is disabled.
func IdentityDocument(w http.ResponseWriter, r *http.Request) {
	g := From(r.Context())
	w.Header().Set("Cache-Control", "no-store")
	if !g.Enabled() {
		http.Error(w, "SMIP is disabled", http.StatusServiceUnavailable)
		return
	}
	if !ingress.TLS13(r) {
		http.Error(w, "TLS 1.3 required", http.StatusBadRequest)
		return
	}
	now := lidza.Now(r.Context())
	d := g.Identity(now)
	if d.Verify(g.domain, now) != nil {
		http.Error(w, "Signing key unavailable", http.StatusServiceUnavailable)
		return
	}
	raw, err := json.Marshal(d)
	if err != nil {
		http.Error(w, "Signing key unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(raw); err != nil {
		return
	}
}

// FetchIdentity retrieves and verifies the identity document of domain. An
// explicit origin is operator-pinned and may be a private address, as v0.2
// origins allow; without one the public host smip.<domain> is used and must
// resolve to public addresses only. The SSRF-safe, TLS 1.3-only, no-redirect
// discovery client does the request. The origin used is returned.
func (g *Gateway) FetchIdentity(ctx context.Context, domain, origin string) (Identity, string, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if !smip.ValidDomain(domain) {
		return Identity{}, "", errors.New("enter the peer's DNS domain, such as partner.example")
	}
	if domain == g.domain {
		return Identity{}, "", errors.New("a server cannot pair with itself")
	}
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	pinned := origin != ""
	if !pinned {
		origin = "https://smip." + domain
	} else if !smipmail.ValidOrigin(origin) {
		return Identity{}, "", errors.New("the endpoint must be a bare HTTPS origin, such as https://smip.partner.example or https://10.0.0.5:8443")
	}
	roots, err := smiptrust.Roots(ctx, g.roots)
	if err != nil {
		return Identity{}, origin, err
	}
	client, err := smipmail.DiscoveryClient(ctx, g.resolver, roots, origin, pinned)
	if err != nil {
		if !pinned {
			return Identity{}, origin, fmt.Errorf("%w: smip.%s must resolve to public addresses; enter the endpoint explicitly for a private deployment", ErrPairing, domain)
		}
		return Identity{}, origin, fmt.Errorf("%w: the endpoint host does not resolve", ErrPairing)
	}
	defer client.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, "GET", origin+IdentityPath, nil)
	if err != nil {
		return Identity{}, origin, err
	}
	response, err := client.Do(request)
	if err != nil {
		return Identity{}, origin, fmt.Errorf("%w: connection or certificate verification failed", ErrPairing)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/json" {
		return Identity{}, origin, fmt.Errorf("%w: the endpoint answered HTTP %d instead of a signed identity document", ErrPairing, response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return Identity{}, origin, ErrPairing
	}
	var document Identity
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || decoder.Decode(new(any)) != io.EOF || document.Verify(domain, lidza.Now(ctx)) != nil {
		return Identity{}, origin, fmt.Errorf("%w: the document is not a valid signed identity of %s", ErrPairing, domain)
	}
	canonical, err := json.Marshal(document)
	if err != nil || !bytes.Equal(bytes.TrimSpace(raw), canonical) {
		return Identity{}, origin, ErrPairing
	}
	return document, origin, nil
}

// Fingerprint groups a key ID (the SHA-256 of the public key) for reading
// out loud between operators.
func Fingerprint(keyID string) string {
	var groups []string
	for len(keyID) > 4 {
		groups = append(groups, keyID[:4])
		keyID = keyID[4:]
	}
	return strings.Join(append(groups, keyID), " ")
}
