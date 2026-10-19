// Package smipmail implements domain-discovered, signed email delivery. It is
// separate from the manually paired SMIP/0.1 chat/file profile.
package smipmail

import (
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
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/env"
	"github.com/jackc/pgx/v5"
	"thura/internal/smip"
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
	roots            *x509.CertPool
	resolver         *net.Resolver
	slots            chan struct{}
	admissions       chan struct{}
	ingest           Ingest
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
		g.roots = roots
	}
	return g, nil
}
func validOrigin(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
}
func From(ctx context.Context) *Gateway { return lidza.Service[*Gateway](ctx) }
func (g *Gateway) Enabled() bool        { return g.domain != "" }
func (g *Gateway) Domain() string       { return g.domain }
func (g *Gateway) tlsConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: g.roots}
}
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
