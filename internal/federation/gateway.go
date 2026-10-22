// Package federation is the opt-in SMIP receive/review adapter. Remote
// signatures authenticate a paired server, never a local workspace member.
package federation

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

// Gateway holds the frozen local identity and the legacy JSON trust. Peers
// paired on the admin page live in smip_peer and are merged at request time
// by Trust, so pairing and revocation need no restart.
type Gateway struct {
	domain     string
	signer     smip.SigningKey
	peers      map[string]map[string]smip.Key
	slots      chan struct{}
	origins    map[string]string
	originKeys map[string]smip.Key
	// roots, caFile and caPEM come from the JSON caFile; the CA bundle saved
	// on the admin page replaces them per request (smiptrust.Roots).
	roots    *x509.CertPool
	caFile   string
	caPEM    []byte
	resolver *net.Resolver
}

type settings struct {
	Enabled bool   `env:"SMIP_ENABLED"`
	Config  string `env:"SMIP_CONFIG"`
}

// Configure freezes the local identity and the legacy JSON peers for this
// process. Database peers are read per request; no request can supply
// trusted keys of its own.
func Configure(s *lidza.Services) error {
	var settings settings
	if err := env.Load(".", &settings); err != nil {
		return err
	}
	gateway := &Gateway{resolver: net.DefaultResolver}
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
	if err != nil || len(seed) != ed25519.SeedSize || base64.RawURLEncoding.EncodeToString(seed) != cfg.Seed || len(cfg.Peers) > MaxPeers {
		return nil, errors.New("invalid SMIP operator configuration")
	}
	g := &Gateway{domain: cfg.Domain, signer: smip.SigningKey{Private: ed25519.NewKeyFromSeed(seed), NotBefore: cfg.NotBefore, NotAfter: cfg.NotAfter}, peers: map[string]map[string]smip.Key{}, slots: make(chan struct{}, 2), resolver: net.DefaultResolver}
	for domain, keys := range cfg.Peers {
		if domain == cfg.Domain || len(keys) == 0 || len(keys) > MaxPeerKeys {
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
		g.roots, g.caFile, g.caPEM = roots, cfg.CAFile, pem
	}
	for peer, origin := range cfg.Origins {
		if len(g.peers[peer]) == 0 {
			return nil, errors.New("SMIP endpoint requires a configured peer")
		}
		g.origins[peer] = origin
		client, err := g.newClient(origin, peer, g.peers[peer], g.roots)
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
	// The complete bounded body arrives before a slot is held; the receiver
	// then re-reads it from memory.
	body, err := smip.ReadBody(w, req)
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			http.Error(w, "packet too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid body", http.StatusBadRequest)
		}
		return
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	select {
	case g.slots <- struct{}{}:
		defer func() { <-g.slots }()
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "SMIP receiver busy", http.StatusServiceUnavailable)
		return
	}
	// Trust is read for this packet alone: a peer paired or revoked on any
	// node is honoured by the next packet every node admits.
	trust, err := g.Trust(req.Context())
	if err != nil {
		http.Error(w, "SMIP trust unavailable", http.StatusServiceUnavailable)
		return
	}
	inbox := &DatabaseInbox{ctx: req.Context(), gateway: g, peers: trust.Peers}
	receiver, err := smip.NewReceiver(g.domain, inbox, g.signer, trust.Peers, func(e smip.Envelope) bool { return e.Kind == "chat" || e.Kind == "file" }, func() time.Time { return lidza.Now(req.Context()) })
	if err != nil {
		http.Error(w, "SMIP receiver unavailable", http.StatusServiceUnavailable)
		return
	}
	receiver.ServeHTTP(w, req)
}

func (g *Gateway) newClient(origin, peer string, keys map[string]smip.Key, roots *x509.CertPool) (*smip.Client, error) {
	return smip.NewApplicationClient(origin, peer, keys, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots})
}

// ConfiguredCA is the deployment JSON caFile and its PEM, shown read-only on
// the admin page.
func (g *Gateway) ConfiguredCA() (string, []byte) { return g.caFile, g.caPEM }

// client builds the sending client for peer from the given trust snapshot:
// the origin and the receipt keys come from the same read.
func (g *Gateway) client(trust Trust, peer string) (*smip.Client, error) {
	origin := trust.Origins[peer]
	if origin == "" {
		return nil, errors.New("SMIP peer sending endpoint unavailable")
	}
	return g.newClient(origin, peer, trust.Peers[peer], trust.Roots)
}

func (g *Gateway) Enabled() bool         { return g.domain != "" }
func (g *Gateway) Domain() string        { return g.domain }
func (g *Gateway) DispatchKind() string  { return DispatchJob + ":" + g.domain }
func (g *Gateway) ReconcileKind() string { return ReconcileJob + ":" + g.domain }
