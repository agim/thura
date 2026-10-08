package smip

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const MessagePath = "/smip/v0.1/messages"

// ApplicationMessagePath is the explicitly selected API-hosted profile endpoint.
const ApplicationMessagePath = "/api/v1/smip/v0.1/messages"

type SigningKey struct {
	Private             ed25519.PrivateKey
	NotBefore, NotAfter int64
}
type Receiver struct {
	slots  chan struct{}
	domain string
	inbox  Inbox
	policy func(Envelope) bool
	now    func() time.Time
	mu     sync.RWMutex
	signer SigningKey
	peers  map[string]map[string]Key
}

func cloneKeys(keys map[string]Key) (map[string]Key, error) {
	result := map[string]Key{}
	for id, k := range keys {
		if len(k.Public) != ed25519.PublicKeySize || id != KeyID(k.Public) || k.NotBefore < 0 || k.NotAfter <= k.NotBefore {
			return nil, errors.New("invalid trusted key")
		}
		k.Public = append(ed25519.PublicKey(nil), k.Public...)
		result[id] = k
	}
	return result, nil
}
func NewReceiver(domain string, inbox Inbox, signer SigningKey, peers map[string]map[string]Key, policy func(Envelope) bool, now func() time.Time) (*Receiver, error) {
	if !validDomain(domain) || inbox == nil || policy == nil {
		return nil, errors.New("receiver requires domain, durable inbox and explicit policy")
	}
	if now == nil {
		now = time.Now
	}
	r := &Receiver{slots: make(chan struct{}, 2), domain: domain, inbox: inbox, policy: policy, now: now, peers: map[string]map[string]Key{}}
	if err := r.SetSigningKey(signer); err != nil {
		return nil, err
	}
	for domain, keys := range peers {
		if err := r.SetPeer(domain, keys); err != nil {
			return nil, err
		}
	}
	return r, nil
}
func (r *Receiver) SetPeer(domain string, keys map[string]Key) error {
	if !validDomain(domain) {
		return errors.New("invalid peer domain")
	}
	cloned, err := cloneKeys(keys)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peers[domain] = cloned
	return nil
}
func (r *Receiver) SetSigningKey(key SigningKey) error {
	if len(key.Private) != ed25519.PrivateKeySize || key.NotBefore < 0 || key.NotAfter <= key.NotBefore {
		return errors.New("invalid receipt signing key")
	}
	key.Private = append(ed25519.PrivateKey(nil), key.Private...)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signer = key
	return nil
}
func writeReceipt(w http.ResponseWriter, r Receipt, status int) {
	b, err := json.Marshal(r)
	if err != nil {
		http.Error(w, "receipt encoding failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if _, err := w.Write(b); err != nil {
		// The durable record remains committed. A disconnected peer can retry
		// the same packet to recover this receipt; never roll back acceptance.
		return
	}
}
func (r *Receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if (req.URL.Path != MessagePath && req.URL.Path != ApplicationMessagePath) || req.URL.RawQuery != "" {
		http.NotFound(w, req)
		return
	}
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if req.TLS == nil || req.TLS.Version < tls.VersionTLS13 {
		http.Error(w, "TLS 1.3 required", http.StatusBadRequest)
		return
	}
	if req.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
		return
	}
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "receiver busy", http.StatusServiceUnavailable)
		return
	}
	if req.ContentLength > MaxWire {
		http.Error(w, "packet too large", http.StatusRequestEntityTooLarge)
		return
	}
	req.Body = http.MaxBytesReader(w, req.Body, MaxWire)
	b, err := io.ReadAll(req.Body)
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			http.Error(w, "packet too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid body", http.StatusBadRequest)
		}
		return
	}
	var p Packet
	if err = strictJSON(b, &p); err != nil {
		http.Error(w, "invalid canonical packet", http.StatusBadRequest)
		return
	}
	e := p.Envelope
	r.mu.RLock()
	key, known := r.peers[e.From][e.KeyID]
	signer := r.signer
	// Fence admission against trust/key replacement: once SetPeer returns,
	// no old-key request can subsequently commit. Policy must not mutate config.
	defer r.mu.RUnlock()
	if !known || p.Verify(key) != nil {
		http.Error(w, "untrusted envelope", http.StatusUnauthorized)
		return
	}
	if e.To != r.domain {
		http.Error(w, "wrong receiver", http.StatusForbidden)
		return
	}
	old, exists, err := r.inbox.Get(e.From, e.ID)
	if err != nil {
		http.Error(w, "inbox unavailable", http.StatusServiceUnavailable)
		return
	}
	if exists {
		if old.Packet.Digest() != p.Digest() {
			http.Error(w, "message ID conflict", http.StatusConflict)
			return
		}
		writeReceipt(w, old.Receipt, http.StatusOK)
		return
	}
	now := r.now().Unix()
	if e.Created > now+120 || e.Expires <= now {
		http.Error(w, "envelope expired or from future", http.StatusUnprocessableEntity)
		return
	}
	if !r.policy(e) {
		http.Error(w, "recipient or stream denied", http.StatusForbidden)
		return
	}
	if now < signer.NotBefore || now >= signer.NotAfter {
		http.Error(w, "receipt key unavailable", http.StatusServiceUnavailable)
		return
	}
	record, created, err := r.inbox.Put(Record{p, receiptFor(p, signer.Private, now)})
	if errors.Is(err, ErrAdmissionDenied) {
		http.Error(w, "recipient or stream denied", http.StatusForbidden)
		return
	}
	if errors.Is(err, ErrConflict) {
		http.Error(w, "message ID conflict", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "inbox unavailable", http.StatusServiceUnavailable)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeReceipt(w, record.Receipt, status)
}

// HTTPError distinguishes peer rejections from retryable transport outcomes.
type HTTPError struct {
	Status    int
	Retryable bool
}

func (e *HTTPError) Error() string { return fmt.Sprintf("SMIP peer returned HTTP %d", e.Status) }

type Client struct {
	endpoint, destination string
	keys                  map[string]Key
	http                  *http.Client
}

// NewClient uses only an operator-configured HTTPS endpoint and pinned peer
// keys. It never discovers keys from an untrusted message or follows redirects.
func NewClient(endpoint, destination string, keys map[string]Key, tlsConfig *tls.Config) (*Client, error) {
	return newClient(endpoint, destination, keys, tlsConfig, MessagePath)
}

// NewApplicationClient selects the API-hosted endpoint during explicit pairing.
// It never probes or falls back to the standalone endpoint.
func NewApplicationClient(endpoint, destination string, keys map[string]Key, tlsConfig *tls.Config) (*Client, error) {
	return newClient(endpoint, destination, keys, tlsConfig, ApplicationMessagePath)
}
func newClient(endpoint, destination string, keys map[string]Key, tlsConfig *tls.Config, messagePath string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || !validDomain(destination) {
		return nil, errors.New("explicit HTTPS origin and destination required")
	}
	trusted, err := cloneKeys(keys)
	if err != nil {
		return nil, err
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13}
	if tlsConfig != nil {
		config = tlsConfig.Clone()
		if config.InsecureSkipVerify {
			return nil, errors.New("TLS certificate verification cannot be disabled")
		}
		if config.MinVersion < tls.VersionTLS13 {
			config.MinVersion = tls.VersionTLS13
		}
	}
	if config.MaxVersion != 0 && config.MaxVersion < config.MinVersion {
		return nil, errors.New("TLS 1.3 required")
	}
	u.Path = messagePath
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: config, ResponseHeaderTimeout: 10 * time.Second}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Client{endpoint: u.String(), destination: destination, keys: trusted, http: client}, nil
}
func (c *Client) Close() { c.http.CloseIdleConnections() }
func (c *Client) Send(ctx context.Context, p Packet) (Receipt, error) {
	if err := p.Envelope.Validate(); err != nil {
		return Receipt{}, err
	}
	if p.Envelope.To != c.destination {
		return Receipt{}, errors.New("packet destination differs from configured peer")
	}
	b, err := json.Marshal(p)
	if err != nil {
		return Receipt{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(b))
	if err != nil {
		return Receipt{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return Receipt{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return Receipt{}, &HTTPError{Status: response.StatusCode, Retryable: response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusInternalServerError || response.StatusCode == http.StatusBadGateway || response.StatusCode == http.StatusServiceUnavailable || response.StatusCode == http.StatusGatewayTimeout}
	}
	if response.Header.Get("Content-Type") != "application/json" {
		return Receipt{}, errors.New("invalid receipt content type")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(raw) > 4096 {
		return Receipt{}, errors.New("invalid receipt size")
	}
	var receipt Receipt
	if err = strictJSON(raw, &receipt); err != nil {
		return Receipt{}, err
	}
	key, known := c.keys[receipt.KeyID]
	if !known {
		return Receipt{}, errors.New("unknown receipt signing key")
	}
	if err = receipt.Verify(p, key); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}
