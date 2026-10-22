// Package smipmail implements domain-discovered, signed email delivery. It is
// separate from the manually paired SMIP/0.1 chat/file profile.
package smipmail

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/env"
	"github.com/jackc/pgx/v5"
	"thura/internal/smip"
	"thura/internal/smiptrust"
)

type Config struct {
	Domain          string              `json:"domain"`
	Seed            string              `json:"seed"`
	NotBefore       int64               `json:"notBefore"`
	NotAfter        int64               `json:"notAfter"`
	Keys            map[string]smip.Key `json:"keys,omitempty"`
	Origins         map[string]string   `json:"origins,omitempty"`
	RequiredDomains []string            `json:"requiredDomains,omitempty"`
	DeniedDomains   []string            `json:"deniedDomains,omitempty"`
	CAFile          string              `json:"caFile,omitempty"`
}

// Ingest atomically stores the email through the ordinary mailbox/storage code.
// It runs inside the same transaction as the durable SMIP receipt.
type Ingest func(context.Context, pgx.Tx, string, Payload, string) (string, error)
type Gateway struct {
	domain           string
	teamID           string
	providerOnly     bool
	signer           smip.SigningKey
	keys             map[string]smip.Key
	origins          map[string]string
	required, denied map[string]bool
	// roots and caPEM come from the deployment JSON caFile; origins are the
	// JSON origins. The admin page's saved origins and CA bundle are merged
	// over them per request (smiptrust), the saved values winning.
	roots      *x509.CertPool
	caFile     string
	caPEM      []byte
	resolver   *net.Resolver
	slots      chan struct{}
	admissions chan struct{}
	ingest     Ingest
}

func Configure(s *lidza.Services, ingest Ingest) error {
	var settings struct {
		Enabled bool   `env:"SMIP_MAIL_ENABLED"`
		Config  string `env:"SMIP_MAIL_CONFIG"`
	}
	if err := env.Load(".", &settings); err != nil {
		return err
	}
	gateway := &Gateway{ingest: ingest, resolver: net.DefaultResolver, slots: make(chan struct{}, 2), admissions: make(chan struct{}, 2), origins: map[string]string{}, keys: map[string]smip.Key{}}
	if settings.Enabled {
		var cfg Config
		decoder := json.NewDecoder(strings.NewReader(settings.Config))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&cfg) != nil || decoder.Decode(new(any)) != io.EOF {
			return errors.New("invalid sealed SMIP_MAIL_CONFIG")
		}
		var err error
		gateway, err = NewGateway(cfg, ingest)
		if err != nil {
			return err
		}
	}
	lidza.Provide(s, gateway)
	return nil
}
func NewGateway(cfg Config, ingest Ingest) (*Gateway, error) {
	seed, err := base64.RawURLEncoding.Strict().DecodeString(cfg.Seed)
	now := time.Now().Unix()
	if err != nil || len(seed) != 32 || smip.Encode(seed) != cfg.Seed || !smip.ValidDomain(cfg.Domain) || cfg.NotBefore > now || cfg.NotAfter <= now || cfg.NotBefore < 0 || cfg.NotAfter >= 1<<53 || len(cfg.Keys) > 9 || len(cfg.Origins) > 100 || len(cfg.RequiredDomains) > 100 || len(cfg.DeniedDomains) > 100 {
		return nil, errors.New("invalid SMIP mail identity, signing interval or trust configuration")
	}
	g := &Gateway{domain: cfg.Domain, signer: smip.SigningKey{Private: ed25519.NewKeyFromSeed(seed), NotBefore: cfg.NotBefore, NotAfter: cfg.NotAfter}, keys: map[string]smip.Key{}, origins: map[string]string{}, required: map[string]bool{}, denied: map[string]bool{}, resolver: net.DefaultResolver, slots: make(chan struct{}, 2), admissions: make(chan struct{}, 2), ingest: ingest}
	for id, key := range cfg.Keys {
		if len(key.Public) != 32 || id != smip.KeyID(key.Public) || key.NotBefore < 0 || key.NotAfter <= key.NotBefore {
			return nil, errors.New("invalid SMIP mail historical key")
		}
		key.Public = append(ed25519.PublicKey(nil), key.Public...)
		g.keys[id] = key
	}
	public := g.signer.Private.Public().(ed25519.PublicKey)
	id := smip.KeyID(public)
	if key, ok := g.keys[id]; ok && key.Revoked {
		return nil, errors.New("active SMIP mail key is revoked")
	}
	g.keys[id] = smip.Key{Public: public, NotBefore: cfg.NotBefore, NotAfter: cfg.NotAfter}
	for domain, origin := range cfg.Origins {
		if !smip.ValidDomain(domain) || !validOrigin(origin) {
			return nil, errors.New("invalid operator-pinned SMIP mail origin")
		}
		g.origins[domain] = strings.TrimRight(origin, "/")
	}
	for _, domain := range cfg.RequiredDomains {
		if !smip.ValidDomain(domain) {
			return nil, errors.New("invalid required SMIP mail domain")
		}
		g.required[domain] = true
	}
	for _, domain := range cfg.DeniedDomains {
		if !smip.ValidDomain(domain) {
			return nil, errors.New("invalid blocked SMIP mail domain")
		}
		g.denied[domain] = true
	}
	if cfg.CAFile != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(cfg.CAFile)
		if err != nil || !roots.AppendCertsFromPEM(raw) {
			return nil, errors.New("invalid SMIP mail CA bundle")
		}
		g.roots, g.caFile, g.caPEM = roots, cfg.CAFile, raw
	}
	return g, nil
}
func validOrigin(raw string) bool { return smiptrust.ValidOrigin(raw) }

// pinnedOrigins is the operator-pinned origin of each partner domain for this
// request: the JSON origins with the admin page's saved origins over them.
func (g *Gateway) pinnedOrigins(ctx context.Context) (map[string]string, error) {
	return smiptrust.MergeOrigins(ctx, g.origins)
}

// ConfiguredOrigins and ConfiguredCA are the deployment JSON values, shown
// read-only on the admin page; nothing secret is in them.
func (g *Gateway) ConfiguredOrigins() map[string]string {
	out := make(map[string]string, len(g.origins))
	for domain, origin := range g.origins {
		out[domain] = origin
	}
	return out
}
func (g *Gateway) ConfiguredCA() (string, []byte) { return g.caFile, g.caPEM }
func From(ctx context.Context) *Gateway           { return lidza.Service[*Gateway](ctx) }
func (g *Gateway) Enabled() bool                  { return g.domain != "" }
func (g *Gateway) Domain() string                 { return g.domain }
func Capabilities(w http.ResponseWriter, r *http.Request) {
	if g, ok := requestGateway(w, r); ok {
		g.capabilities(w, r)
	}
}
func Receive(w http.ResponseWriter, r *http.Request) {
	if g, ok := requestGateway(w, r); ok {
		g.receive(w, r)
	}
}
