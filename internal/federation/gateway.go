// Package federation is the opt-in SMIP receive/review adapter. Remote
// signatures authenticate a paired server, never a local workspace member.
package federation

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/env"
	"thura/internal/smip"
)

type Config struct {
	Domain     string                         `json:"domain"`
	Seed       string                         `json:"seed"`
	NotBefore  int64                          `json:"notBefore"`
	NotAfter   int64                          `json:"notAfter"`
	Peers      map[string]map[string]smip.Key `json:"peers"`
	Origins    map[string]string              `json:"origins"`
	OriginKeys map[string]smip.Key            `json:"originKeys"`
	CAFile     string                         `json:"caFile"`
}
type Gateway struct {
	domain     string
	signer     smip.SigningKey
	peers      map[string]map[string]smip.Key
	slots      chan struct{}
	origins    map[string]string
	originKeys map[string]smip.Key
	roots      *x509.CertPool
}

type settings struct {
	Enabled bool   `env:"SMIP_ENABLED"`
	Config  string `env:"SMIP_CONFIG"`
}

// Configure freezes operator trust for this process. Rotation/revocation
// requires restarting all receiving nodes; no request can supply trusted keys.
func Configure(s *lidza.Services) error {
	var settings settings
	if err := env.Load(".", &settings); err != nil {
		return err
	}
	gateway := &Gateway{}
	if settings.Enabled {
		var cfg Config
		dec := json.NewDecoder(strings.NewReader(settings.Config))
		dec.DisallowUnknownFields()
		if dec.Decode(&cfg) != nil || dec.Decode(new(any)) != io.EOF {
			return errors.New("invalid sealed SMIP_CONFIG")
		}
		var err error
		gateway, err = NewGateway(cfg)
		if err != nil {
			return err
		}
	}
	lidza.Provide(s, gateway)
	return nil
}

func NewGateway(cfg Config) (*Gateway, error) {
	seed, err := base64.RawURLEncoding.DecodeString(cfg.Seed)
	if err != nil || len(seed) != ed25519.SeedSize || base64.RawURLEncoding.EncodeToString(seed) != cfg.Seed || len(cfg.Peers) == 0 || len(cfg.Peers) > 100 {
		return nil, errors.New("invalid SMIP operator configuration")
	}
	g := &Gateway{domain: cfg.Domain, signer: smip.SigningKey{Private: ed25519.NewKeyFromSeed(seed), NotBefore: cfg.NotBefore, NotAfter: cfg.NotAfter}, peers: map[string]map[string]smip.Key{}, slots: make(chan struct{}, 2)}
	for domain, keys := range cfg.Peers {
		if domain == cfg.Domain || len(keys) == 0 || len(keys) > 10 {
			return nil, errors.New("invalid SMIP peer configuration")
		}
		copied := map[string]smip.Key{}
		for id, key := range keys {
			key.Public = append(ed25519.PublicKey(nil), key.Public...)
			copied[id] = key
		}
		g.peers[domain] = copied
	}
	if _, err := smip.NewReceiver(g.domain, &DatabaseInbox{}, g.signer, g.peers, func(smip.Envelope) bool { return true }, time.Now); err != nil {
		return nil, errors.New("invalid SMIP domains or signing keys")
	}
	g.origins = map[string]string{}
	if cfg.CAFile != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, errors.New("SMIP system certificate trust unavailable")
		}
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil || !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("invalid SMIP CA file")
		}
		g.roots = roots
	}
	for peer, origin := range cfg.Origins {
		if len(g.peers[peer]) == 0 {
			return nil, errors.New("SMIP endpoint requires a configured peer")
		}
		g.origins[peer] = origin
		client, err := g.client(peer)
		if err != nil {
			return nil, errors.New("invalid SMIP peer HTTPS origin")
		}
		client.Close()
	}
	g.originKeys = map[string]smip.Key{}
	for id, key := range cfg.OriginKeys {
		key.Public = append(ed25519.PublicKey(nil), key.Public...)
		g.originKeys[id] = key
	}
	public := g.signer.Private.Public().(ed25519.PublicKey)
	if _, ok := g.originKeys[smip.KeyID(public)]; !ok {
		g.originKeys[smip.KeyID(public)] = smip.Key{Public: public, NotBefore: g.signer.NotBefore, NotAfter: g.signer.NotAfter}
	}
	check, err := smip.NewClient("https://validation.example", g.domain, g.originKeys, nil)
	if err != nil {
		return nil, errors.New("invalid SMIP historical origin keys")
	}
	check.Close()

	return g, nil
}

func From(ctx context.Context) *Gateway                { return lidza.Service[*Gateway](ctx) }
func Receive(w http.ResponseWriter, req *http.Request) { From(req.Context()).ServeHTTP(w, req) }
func (g *Gateway) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if g.domain == "" {
		http.Error(w, "SMIP is disabled", http.StatusServiceUnavailable)
		return
	}
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "SMIP receiver busy", http.StatusServiceUnavailable)
		return
	}
	inbox := &DatabaseInbox{ctx: req.Context(), gateway: g}
	receiver, err := smip.NewReceiver(g.domain, inbox, g.signer, g.peers, func(e smip.Envelope) bool { return e.Kind == "chat" || e.Kind == "file" }, func() time.Time { return lidza.Now(req.Context()) })
	if err != nil {
		http.Error(w, "SMIP receiver unavailable", http.StatusServiceUnavailable)
		return
	}
	receiver.ServeHTTP(w, req)
}

func (g *Gateway) client(peer string) (*smip.Client, error) {
	origin := g.origins[peer]
	if origin == "" {
		return nil, errors.New("SMIP peer sending endpoint unavailable")
	}
	return smip.NewApplicationClient(origin, peer, g.peers[peer], &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: g.roots})
}

func (g *Gateway) Enabled() bool         { return g.domain != "" }
func (g *Gateway) DispatchKind() string  { return DispatchJob + ":" + g.domain }
func (g *Gateway) ReconcileKind() string { return ReconcileJob + ":" + g.domain }
