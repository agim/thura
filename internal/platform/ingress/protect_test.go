package ingress

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agim/lidza/packs/cache"
	"github.com/agim/lidza/pkg/gateway"
)

type testAuthority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newTestAuthority(t *testing.T) testAuthority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "isolated ingress test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return testAuthority{cert, key, pool}
}

func (a testAuthority) issue(t *testing.T, name string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: serial, DNSNames: []string{name}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, a.cert, &key.PublicKey, a.key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

func TestProtectedRoutesRequireOriginalTLSOverRealMutualTLS(t *testing.T) {
	ca := newTestAuthority(t)
	edgeCert, appCert := ca.issue(t, "edge.internal"), ca.issue(t, "app.internal")
	var storeFailed atomic.Bool
	claim := gateway.Claims(cache.NewMemory(128), "thura-test-ingress:")
	authorize := func(_ context.Context, authority string) error {
		if authority != "public.example" {
			return errors.New("wrong authority")
		}
		return nil
	}
	protect, err := Protect(gateway.Policy{Peers: []string{gateway.Pin(edgeCert.Leaf)}, Authorize: authorize, Claim: func(ctx context.Context, key string, ttl time.Duration) (bool, error) {
		if storeFailed.Load() {
			return false, errors.New("nonce store unavailable")
		}
		return claim(ctx, key, ttl)
	}})
	if err != nil {
		t.Fatal(err)
	}
	var admitted atomic.Int32
	handler := protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ProtectedPath(r.URL.Path) {
			if r.TLS == nil || r.TLS.Version != tls.VersionTLS13 || !TLS13(r) {
				t.Error("protected handler received no verified original TLS")
			}
			admitted.Add(1)
		}
		w.WriteHeader(200)
	}))
	plain := httptest.NewServer(handler)
	defer plain.Close()
	for _, path := range []string{"/api/v1/smip/v0.1/messages", "/api/v1/smip/v0.2/mail", "/api/v1/smip/v0.2/capabilities", "/api/v1/setup/transport"} {
		r, err := http.NewRequest("GET", plain.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Host = "public.example"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set(gateway.Header, "forged")
		res, err := plain.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("unprotected HTTP listener accepted %s: %d", path, res.StatusCode)
		}
	}
	res, err := plain.Client().Get(plain.URL + "/api/v1/health")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal("ordinary internal HTTP was disabled")
	}
	backend := httptest.NewUnstartedServer(handler)
	backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	backend.TLS, err = gateway.ServerTLS(appCert, ca.pool)
	if err != nil {
		t.Fatal(err)
	}
	backend.StartTLS()
	defer backend.Close()
	upstream, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	transport, err := gateway.ClientTransport(edgeCert, ca.pool, "app.internal")
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	var capturedMu sync.Mutex
	captured := ""
	proxy := &httputil.ReverseProxy{Transport: transport, Rewrite: func(p *httputil.ProxyRequest) {
		p.Out.URL.Scheme, p.Out.URL.Host = upstream.Scheme, upstream.Host
		if err := gateway.Forward(p.Out, p.In); err != nil {
			p.Out.URL.Scheme = "invalid"
		}
		capturedMu.Lock()
		captured = p.Out.Header.Get(gateway.Header)
		capturedMu.Unlock()
	}}
	edge := httptest.NewUnstartedServer(gateway.Edge(authorize)(proxy))
	edge.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{ca.issue(t, "public.example")}}
	edge.StartTLS()
	defer edge.Close()
	for _, version := range []uint16{tls.VersionTLS12, tls.VersionTLS13} {
		tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: ca.pool, ServerName: "public.example", MinVersion: version, MaxVersion: version}}
		client := &http.Client{Transport: tr, Timeout: 5 * time.Second}
		r, err := http.NewRequest("GET", edge.URL+"/api/v1/setup/transport", nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Host = "public.example"
		r.Header.Set(gateway.Header, "forged-client-value")
		r.Header.Set("X-Forwarded-Proto", "https")
		res, err := client.Do(r)
		tr.CloseIdleConnections()
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		want := http.StatusBadRequest
		if version == tls.VersionTLS13 {
			want = 200
		}
		if res.StatusCode != want {
			t.Fatalf("original TLS %x returned %d, want %d", version, res.StatusCode, want)
		}
	}
	capturedMu.Lock()
	wire := captured
	capturedMu.Unlock()
	// An adversarial authenticated client can send captured metadata directly;
	// full certificate validation remains enabled and the receiver rejects replay.
	attackTransport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: ca.pool, ServerName: "app.internal", MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{edgeCert}}}
	defer attackTransport.CloseIdleConnections()
	attack := &http.Client{Transport: attackTransport, Timeout: 5 * time.Second}
	for _, authority := range []string{"public.example", "unapproved.example"} {
		r, err := http.NewRequest("GET", backend.URL+"/api/v1/setup/transport", nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Host = authority
		r.Header.Set(gateway.Header, wire)
		res, err := attack.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatal("replayed or changed-authority request reached the protected handler")
		}
	}
	storeFailed.Store(true)
	r, err := http.NewRequest("GET", edge.URL+"/api/v1/setup/transport", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Host = "public.example"
	clientTransport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: ca.pool, ServerName: "public.example", MinVersion: tls.VersionTLS13}}
	defer clientTransport.CloseIdleConnections()
	res, err = (&http.Client{Transport: clientTransport, Timeout: 5 * time.Second}).Do(r)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden || admitted.Load() != 1 {
		t.Fatal("nonce-store failure reached the protected handler")
	}
}
