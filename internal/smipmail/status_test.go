package smipmail

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// The status is advisory and must report only what was observed: a found or
// missing TXT record, the resolved addresses, and a TLS 1.3 challenge that
// Thura's own route answered.
func TestDomainStatusReportsObservedDNSAndTransportOnly(t *testing.T) {
	transport := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/setup/transport" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("nonce") == "" {
			http.Error(w, "challenge", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"nonce": r.URL.Query().Get("nonce"), "transport": "tls1.3"})
	}))
	transport.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	transport.StartTLS()
	defer transport.Close()
	g := testGateway(t)
	g.resolver = dnsResolver(t, []string{"v=SMIP1"}, dnsmessage.RCodeSuccess)
	g.roots = x509.NewCertPool()
	g.roots.AddCert(transport.Certificate())
	now := time.Now()
	// Without a pinned origin the loopback answer is a private address, so the
	// transport is reported unreachable rather than probed.
	status := g.probeStatus(context.Background(), "status.example", "https://app.example", now)
	if status.TXT != "found" || len(status.Addresses) != 1 || status.Addresses[0] != "127.0.0.1" || status.TLS != "yes" && status.TLS != "no" || status.MatchesServer != "yes" && status.MatchesServer != "no" && status.MatchesServer != "unknown" {
		t.Fatalf("unexpected status %+v", status)
	}
	if status.TLS != "no" {
		t.Fatal("public discovery probed a private address")
	}
	g.origins["status.example"] = transport.URL
	status = g.probeStatus(context.Background(), "status.example", "https://app.example", now)
	if status.TLS != "yes" || status.TXT != "found" {
		t.Fatalf("pinned transport challenge: %+v", status)
	}
	delete(g.origins, "status.example")
	g.resolver = dnsResolver(t, nil, dnsmessage.RCodeNameError)
	status = g.probeStatus(context.Background(), "status.example", "https://app.example", now)
	if status.TXT != "missing" || len(status.Addresses) != 0 || status.TLS != "no" || status.MatchesServer != "unknown" {
		t.Fatalf("absence must not invent success: %+v", status)
	}
	g.resolver = dnsResolver(t, nil, dnsmessage.RCodeServerFailure)
	status = g.probeStatus(context.Background(), "status.example", "https://app.example", now)
	if status.TXT != "error" || status.TLS != "no" {
		t.Fatalf("resolver failure must not read as missing or reachable: %+v", status)
	}
	// Cached for a minute, then refreshed.
	g.resolver = dnsResolver(t, []string{"v=SMIP1"}, dnsmessage.RCodeSuccess)
	first := g.DomainStatus(context.Background(), "cached.example", "https://app.example", now)
	g.resolver = dnsResolver(t, nil, dnsmessage.RCodeNameError)
	if again := g.DomainStatus(context.Background(), "cached.example", "https://app.example", now.Add(30*time.Second)); again.TXT != first.TXT || again.CheckedAt != first.CheckedAt {
		t.Fatal("status was not cached")
	}
	if later := g.DomainStatus(context.Background(), "cached.example", "https://app.example", now.Add(StatusLifetime+time.Second)); later.TXT != "missing" {
		t.Fatal("expired status was not refreshed")
	}
	if g.probeStatus(context.Background(), "not a domain", "", now).Detail == "" {
		t.Fatal("invalid domain accepted")
	}
}
