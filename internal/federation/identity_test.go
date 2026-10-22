package federation

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"thura/internal/smip"
)

func testGateway(t *testing.T, domain string) *Gateway {
	t.Helper()
	seed := sha256.Sum256([]byte("PUBLIC SMIP pairing unit fixture " + domain))
	now := time.Now().Unix()
	g, err := NewGateway(Config{Domain: domain, Seed: smip.Encode(seed[:]), NotBefore: now - 60, NotAfter: now + 86400})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestIdentityDocumentSignatureExpiryAndDomainChecks(t *testing.T) {
	g := testGateway(t, "unit.example")
	now := time.Now()
	d := g.Identity(now)
	if err := d.Verify(g.domain, now); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Identity){
		"domain":     func(d *Identity) { d.Domain = "other.example" },
		"path":       func(d *Identity) { d.MessagePath = "/arbitrary" },
		"profile":    func(d *Identity) { d.Profile = smip.MailVersion },
		"version":    func(d *Identity) { d.Version = "smip-pairing/2" },
		"expiry":     func(d *Identity) { d.Expires++ },
		"issued":     func(d *Identity) { d.Issued += 3600 },
		"signature":  func(d *Identity) { d.Signature = strings.Repeat("A", 86) },
		"no keys":    func(d *Identity) { d.Keys = map[string]smip.Key{} },
		"signer":     func(d *Identity) { d.KeyID = strings.Repeat("0", 64) },
		"revoked":    func(d *Identity) { k := d.Keys[d.KeyID]; k.Revoked = true; d.Keys[d.KeyID] = k },
		"long-lived": func(d *Identity) { d.Expires = d.Issued + int64(IdentityLifetime/time.Second) + 1 },
	} {
		copied := g.Identity(now)
		copied.Keys = map[string]smip.Key{}
		for id, key := range d.Keys {
			copied.Keys[id] = key
		}
		change(&copied)
		if copied.Verify(g.domain, now) == nil {
			t.Fatalf("%s: changed claims verified", name)
		}
	}
	if d.Verify(g.domain, now.Add(IdentityLifetime+time.Minute)) == nil {
		t.Fatal("expired document verified")
	}
	raw, err := json.Marshal(d)
	if err != nil || strings.Contains(string(raw), smip.Encode(g.signer.Private.Seed())) {
		t.Fatal("private key in identity document", err)
	}
	if Fingerprint("abcd1234ef") != "abcd 1234 ef" {
		t.Fatal("fingerprint grouping", Fingerprint("abcd1234ef"))
	}
}

// aRecordResolver answers every A question with the loopback address, which
// public discovery must refuse while an operator-pinned endpoint may use it.
func aRecordResolver(t *testing.T) *net.Resolver {
	t.Helper()
	connection, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, address, err := connection.ReadFrom(buffer)
			if err != nil {
				return
			}
			var request dnsmessage.Message
			if request.Unpack(buffer[:n]) != nil {
				continue
			}
			reply := dnsmessage.Message{Header: dnsmessage.Header{ID: request.Header.ID, Response: true, RecursionAvailable: true}, Questions: request.Questions}
			for _, question := range request.Questions {
				if question.Type == dnsmessage.TypeA {
					reply.Answers = append(reply.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}}})
				}
			}
			wire, err := reply.Pack()
			if err != nil {
				continue
			}
			if _, err = connection.WriteTo(wire, address); err != nil {
				return
			}
		}
	}()
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", connection.LocalAddr().String())
	}}
}

func TestFetchIdentityRefusesPrivateDNSButAcceptsPinnedOrigin(t *testing.T) {
	local, peer := testGateway(t, "local.example"), testGateway(t, "peer.example")
	local.resolver = aRecordResolver(t)
	var hits int
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != IdentityPath {
			http.NotFound(w, r)
			return
		}
		raw, _ := json.Marshal(peer.Identity(time.Now()))
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	server.StartTLS()
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Public DNS pointing smip.peer.example at a private address is refused
	// before any connection; the same address is accepted when the operator
	// enters it explicitly, like v0.2 origins.
	if _, _, err := local.FetchIdentity(ctx, "peer.example", ""); !errors.Is(err, ErrPairing) || hits != 0 {
		t.Fatal("public discovery reached a private address", err)
	}
	if _, _, err := local.FetchIdentity(ctx, "peer.example", server.URL); !errors.Is(err, ErrPairing) || hits != 0 {
		t.Fatal("untrusted certificate accepted", err)
	}
	local.roots = x509.NewCertPool()
	local.roots.AddCert(server.Certificate())
	document, origin, err := local.FetchIdentity(ctx, "peer.example", server.URL+"/")
	if err != nil || origin != server.URL || document.Domain != "peer.example" || len(document.Keys) != 1 {
		t.Fatal("pinned private origin refused", err)
	}
	if _, _, err := local.FetchIdentity(ctx, "other.example", server.URL); !errors.Is(err, ErrPairing) {
		t.Fatal("document for another domain accepted", err)
	}
	if _, _, err := local.FetchIdentity(ctx, "local.example", server.URL); err == nil {
		t.Fatal("self pairing accepted")
	}
	for _, bad := range []string{"http://127.0.0.1", server.URL + "/path", server.URL + "?x=1"} {
		if _, _, err := local.FetchIdentity(ctx, "peer.example", bad); err == nil {
			t.Fatal("invalid endpoint accepted", bad)
		}
	}
	redirecting := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+IdentityPath, http.StatusTemporaryRedirect)
	}))
	redirecting.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	redirecting.StartTLS()
	t.Cleanup(redirecting.Close)
	local.roots.AddCert(redirecting.Certificate())
	before := hits
	if _, _, err := local.FetchIdentity(ctx, "peer.example", redirecting.URL); !errors.Is(err, ErrPairing) || hits != before {
		t.Fatal("redirect followed", err)
	}
}
