// Package smiptrust holds the operator's private-deployment trust for SMIP
// server-to-server TLS: pinned SMIP/0.2 mail origins and one private CA
// bundle shared by SMIP/0.1 and SMIP/0.2. The server administrator sets them
// on the SMIP peers admin page; they live in Postgres and are read on every
// discovery, delivery, pairing fetch and status probe, so a change takes
// effect on the next request on every node without a restart. Values from the
// sealed deployment JSON stay supported; the database wins where both exist.
//
// Nothing here weakens verification: the CA bundle is added to the system
// roots, never replaces certificate checks, and holds certificates only.
package smiptrust

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/internal/smip"
)

// MaxOrigins bounds the pinned origins (the same bound as the JSON origins);
// MaxBundleBytes and MaxCertificates bound the CA bundle.
const (
	MaxOrigins      = 100
	MaxBundleBytes  = 256 << 10
	MaxCertificates = 100
)

// ValidOrigin reports whether raw is a bare HTTPS origin: scheme https, a
// host, an optional port in range, and no credentials, path, query or
// fragment. It is the only endpoint shape SMIP peers may use.
func ValidOrigin(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return true
}

// NormalizeOrigin trims an operator-typed origin to its stored form.
func NormalizeOrigin(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// Origin is one pinned mail origin as the admin page shows it.
type Origin struct {
	Domain, Origin, AddedBy string
	AddedAt                 time.Time
}

// Certificate is the public description of one trusted CA certificate.
type Certificate struct {
	Subject, Issuer, Fingerprint string
	NotBefore, NotAfter          time.Time
	CA, Expired                  bool
}

// ParseBundle validates a PEM bundle: every block must be a CERTIFICATE that
// parses, nothing else may be in it (a private key is refused outright), and
// it is bounded in size and count. It returns the public description of each
// certificate.
func ParseBundle(raw []byte, now time.Time) ([]Certificate, error) {
	if len(raw) > MaxBundleBytes {
		return nil, fmt.Errorf("the CA bundle is larger than %d KiB", MaxBundleBytes>>10)
	}
	var out []Certificate
	rest := raw
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if strings.Contains(block.Type, "PRIVATE KEY") {
			return nil, errors.New("the bundle contains a private key; paste only CA certificates and keep private keys off this page")
		}
		if block.Type != "CERTIFICATE" || len(block.Headers) > 0 {
			return nil, fmt.Errorf("the bundle contains a %q block; only CERTIFICATE blocks are accepted", block.Type)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("certificate %d does not parse: %v", len(out)+1, err)
		}
		if len(out) == MaxCertificates {
			return nil, fmt.Errorf("the bundle holds more than %d certificates", MaxCertificates)
		}
		sum := sha256.Sum256(cert.Raw)
		out = append(out, Certificate{Subject: cert.Subject.String(), Issuer: cert.Issuer.String(), Fingerprint: hex.EncodeToString(sum[:]), NotBefore: cert.NotBefore.UTC(), NotAfter: cert.NotAfter.UTC(), CA: cert.BasicConstraintsValid && cert.IsCA, Expired: now.After(cert.NotAfter)})
	}
	if strings.TrimSpace(string(rest)) != "" {
		return nil, errors.New("the bundle has text that is not a PEM certificate; paste only -----BEGIN CERTIFICATE----- blocks")
	}
	if len(out) == 0 {
		return nil, errors.New("the bundle holds no PEM certificate")
	}
	return out, nil
}

// available is false outside the application (unit tests building a gateway
// directly); only the deployment JSON applies there.
func available(ctx context.Context) bool {
	_, ok := lidza.Optional[*db.DB](ctx)
	return ok
}

// Origins returns the pinned mail origins saved on the admin page.
func Origins(ctx context.Context) ([]Origin, error) {
	if !available(ctx) {
		return nil, nil
	}
	rows, err := q.New(db.From(ctx)).ListSmipPinnedOrigins(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Origin, 0, len(rows))
	for _, row := range rows {
		out = append(out, Origin{Domain: row.Domain, Origin: row.Origin, AddedBy: row.AddedBy, AddedAt: row.AddedAt})
	}
	return out, nil
}

// MergeOrigins overlays the saved origins on the deployment JSON origins:
// the saved row wins for its domain. It is read per call, never cached.
func MergeOrigins(ctx context.Context, configured map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(configured))
	for domain, origin := range configured {
		out[domain] = origin
	}
	saved, err := Origins(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range saved {
		out[row.Domain] = row.Origin
	}
	return out, nil
}

// Bundle is the saved CA bundle, when there is one.
type Bundle struct {
	PEM          string
	UpdatedBy    string
	UpdatedAt    time.Time
	Certificates []Certificate
}

// SavedBundle reads the CA bundle saved on the admin page.
func SavedBundle(ctx context.Context) (Bundle, bool, error) {
	if !available(ctx) {
		return Bundle{}, false, nil
	}
	row, err := q.New(db.From(ctx)).GetSmipCaBundle(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return Bundle{}, false, nil
	}
	if err != nil {
		return Bundle{}, false, err
	}
	certs, err := ParseBundle([]byte(row.Pem), lidza.Now(ctx))
	if err != nil {
		return Bundle{}, false, fmt.Errorf("the saved SMIP CA bundle is unreadable: %w", err)
	}
	return Bundle{PEM: row.Pem, UpdatedBy: row.UpdatedBy, UpdatedAt: row.UpdatedAt, Certificates: certs}, true, nil
}

// pools caches parsed pools by the SHA-256 of their PEM, so the database row
// is still read on every call (no staleness) but not re-parsed.
var pools = struct {
	sync.Mutex
	byHash map[[32]byte]*x509.CertPool
}{byHash: map[[32]byte]*x509.CertPool{}}

// Roots returns the roots for one server-to-server request: the system roots
// plus the saved bundle when there is one, else configured (the pool built
// from the deployment JSON caFile, nil for the system roots alone). The
// saved row is read on every call, so saving or clearing it applies to the
// next request on every node.
func Roots(ctx context.Context, configured *x509.CertPool) (*x509.CertPool, error) {
	bundle, ok, err := SavedBundle(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return configured, nil
	}
	key := sha256.Sum256([]byte(bundle.PEM))
	pools.Lock()
	defer pools.Unlock()
	if pool, ok := pools.byHash[key]; ok {
		return pool, nil
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, errors.New("SMIP system certificate trust unavailable")
	}
	if !pool.AppendCertsFromPEM([]byte(bundle.PEM)) {
		return nil, errors.New("the saved SMIP CA bundle is unreadable")
	}
	if len(pools.byHash) >= 8 {
		pools.byHash = map[[32]byte]*x509.CertPool{}
	}
	pools.byHash[key] = pool
	return pool, nil
}

func actor(ctx context.Context) (string, error) {
	user := auth.CurrentUser(ctx)
	if user == nil {
		return "", errors.New("sign in as the server administrator")
	}
	return user.ID, nil
}

// change runs one audited write under the trust lock: the row and its audit
// event commit together or not at all.
func change(ctx context.Context, write func(*q.Queries) (audit.Event, error)) error {
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	if err = queries.LockSmipTrust(ctx); err != nil {
		return err
	}
	event, err := write(queries)
	if err != nil {
		return err
	}
	if err = audit.From(ctx).RecordTx(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// PinOrigin saves (or replaces) the pinned mail origin of domain. The domain
// must be a DNS name and the origin a bare HTTPS origin; the origin may name
// a private address, since an operator typed it, but its certificate must
// still verify against the system roots or the CA bundle.
func PinOrigin(ctx context.Context, domain, origin string) (string, string, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	origin = NormalizeOrigin(origin)
	if !smip.ValidDomain(domain) {
		return "", "", errors.New("enter the partner's mail domain as a DNS hostname, such as partner.example")
	}
	if !ValidOrigin(origin) {
		return "", "", errors.New("the origin must be a bare HTTPS origin, such as https://smip.partner.example or https://10.0.0.5:8443, with no path, query or credentials")
	}
	by, err := actor(ctx)
	if err != nil {
		return "", "", err
	}
	err = change(ctx, func(queries *q.Queries) (audit.Event, error) {
		rows, err := queries.ListSmipPinnedOrigins(ctx)
		if err != nil {
			return audit.Event{}, err
		}
		replaced := ""
		for _, row := range rows {
			if row.Domain == domain {
				replaced = row.Origin
			}
		}
		if replaced == "" && len(rows) >= MaxOrigins {
			return audit.Event{}, fmt.Errorf("at most %d pinned origins can be saved", MaxOrigins)
		}
		if err := queries.UpsertSmipPinnedOrigin(ctx, q.UpsertSmipPinnedOriginParams{Domain: domain, Origin: origin, AddedBy: by, AddedAt: lidza.Now(ctx)}); err != nil {
			return audit.Event{}, err
		}
		meta := map[string]string{"domain": domain, "origin": origin}
		if replaced != "" {
			meta["replaced"] = replaced
		}
		return audit.Event{Action: "smip.trust.origin.pin", Resource: "smip-origin/" + domain, Meta: meta}, nil
	})
	return domain, origin, err
}

// UnpinOrigin removes the saved origin of domain. A deployment JSON origin
// for the same domain applies again afterwards.
func UnpinOrigin(ctx context.Context, domain string) (string, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if _, err := actor(ctx); err != nil {
		return "", err
	}
	err := change(ctx, func(queries *q.Queries) (audit.Event, error) {
		n, err := queries.DeleteSmipPinnedOrigin(ctx, domain)
		if err != nil {
			return audit.Event{}, err
		}
		if n == 0 {
			return audit.Event{}, errors.New("no origin is saved for that domain")
		}
		return audit.Event{Action: "smip.trust.origin.unpin", Resource: "smip-origin/" + domain, Meta: map[string]string{"domain": domain}}, nil
	})
	return domain, err
}

// SaveBundle validates and stores the CA bundle, replacing any saved one.
func SaveBundle(ctx context.Context, raw []byte) ([]Certificate, error) {
	certs, err := ParseBundle(raw, lidza.Now(ctx))
	if err != nil {
		return nil, err
	}
	by, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	normalized := strings.TrimSpace(string(raw)) + "\n"
	err = change(ctx, func(queries *q.Queries) (audit.Event, error) {
		if err := queries.UpsertSmipCaBundle(ctx, q.UpsertSmipCaBundleParams{Pem: normalized, UpdatedBy: by, UpdatedAt: lidza.Now(ctx)}); err != nil {
			return audit.Event{}, err
		}
		fingerprints := make([]string, 0, len(certs))
		for _, cert := range certs {
			fingerprints = append(fingerprints, cert.Fingerprint[:16])
		}
		return audit.Event{Action: "smip.trust.ca.save", Resource: "smip-ca/default", Meta: map[string]string{"certificates": strconv.Itoa(len(certs)), "sha256": strings.Join(fingerprints, ",")}}, nil
	})
	return certs, err
}

// ClearBundle removes the saved CA bundle; the deployment JSON caFile
// bundles, if any, apply again.
func ClearBundle(ctx context.Context) error {
	if _, err := actor(ctx); err != nil {
		return err
	}
	return change(ctx, func(queries *q.Queries) (audit.Event, error) {
		n, err := queries.DeleteSmipCaBundle(ctx)
		if err != nil {
			return audit.Event{}, err
		}
		if n == 0 {
			return audit.Event{}, errors.New("no CA bundle is saved")
		}
		return audit.Event{Action: "smip.trust.ca.clear", Resource: "smip-ca/default"}, nil
	})
}
