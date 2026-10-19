package smipmail

import (
	"context"
	"crypto/ed25519"
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

func testGateway(t *testing.T) *Gateway {
	t.Helper()
	seed := sha256.Sum256([]byte("PUBLIC domain discovery unit fixture"))
	now := time.Now().Unix()
	g, err := NewGateway(Config{Domain: "unit.example", Seed: smip.Encode(seed[:]), NotBefore: now - 60, NotAfter: now + 86400}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return g
}
func TestDiscoveryDocumentAuthenticationAndBounds(t *testing.T) {
	g := testGateway(t)
	now := time.Now()
	d := g.document(now)
	if err := d.Verify(g.domain, now); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Document){func(d *Document) { d.Domain = "other.example" }, func(d *Document) { d.MailPath = "/arbitrary" }, func(d *Document) { d.Profile = "smip/9" }, func(d *Document) { d.Expires++ }, func(d *Document) { d.Signature = strings.Repeat("A", 86) }, func(d *Document) { d.Issued += 3600 }, func(d *Document) { d.Keys = map[string]smip.Key{} }} {
		copy := d
		change(&copy)
		if copy.Verify(g.domain, now) == nil {
			t.Fatal("changed claims verified")
		}
	}
	if d.Verify(g.domain, now.Add(6*time.Minute)) == nil {
		t.Fatal("expired document verified")
	}
	raw, err := json.Marshal(d)
	if err != nil || strings.Contains(string(raw), smip.Encode(g.signer.Private.Seed())) {
		t.Fatal("private key in capabilities", err)
	}
}
func TestDiscoveryRefusesPrivateAddressesRedirectsAndUntrustedTLS(t *testing.T) {
	g := testGateway(t)
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.100.100.200", "::1", "::ffff:127.0.0.1", "fd00::1", "2001:db8::1", "64:ff9b::7f00:1", "198.18.0.1", "0.0.0.0"} {
		if publicIP(net.ParseIP(ip)) {
			t.Error("unsafe address accepted", ip)
		}
	}
	var destinationHits int
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationHits++ }))
	defer target.Close()
	source := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	source.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	source.StartTLS()
	defer source.Close()
	g.origins[g.domain] = source.URL
	if _, err := g.fetch(context.Background(), g.domain); !errors.Is(err, ErrDiscovery) {
		t.Fatal("untrusted TLS accepted", err)
	}
	g.roots = x509.NewCertPool()
	g.roots.AddCert(source.Certificate())
	if _, err := g.fetch(context.Background(), g.domain); !errors.Is(err, ErrDiscovery) || destinationHits != 0 {
		t.Fatal("redirect followed", err)
	}
	if _, err := g.client(context.Background(), source.URL, false); !errors.Is(err, ErrDiscovery) {
		t.Fatal("public discovery reached loopback", err)
	}
}

// An actual DNS responder tests first-contact absence vs unsupported/failing
// advertised protocols without depending on the public resolver or Internet.
func dnsResolver(t *testing.T, records []string, code dnsmessage.RCode) *net.Resolver {
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
			reply := dnsmessage.Message{Header: dnsmessage.Header{ID: request.Header.ID, Response: true, RecursionAvailable: true, RCode: code}, Questions: request.Questions}
			for _, question := range request.Questions {
				if question.Type == dnsmessage.TypeTXT {
					for _, value := range records {
						reply.Answers = append(reply.Answers, dnsmessage.Resource{Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeTXT, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.TXTResource{TXT: []string{value}}})
					}
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
func TestDiscoveryDNSAbsenceIsDifferentFromVerificationFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		records []string
		code    dnsmessage.RCode
		want    error
	}{{"absent", nil, dnsmessage.RCodeSuccess, ErrUnsupported}, {"nxdomain", nil, dnsmessage.RCodeNameError, ErrUnsupported}, {"servfail", nil, dnsmessage.RCodeServerFailure, ErrDiscovery}, {"future version", []string{"v=SMIP2"}, dnsmessage.RCodeSuccess, ErrDiscovery}, {"duplicate", []string{"v=SMIP1", "v=SMIP1"}, dnsmessage.RCodeSuccess, ErrDiscovery}, {"unreachable advertised", []string{"v=SMIP1"}, dnsmessage.RCodeSuccess, ErrDiscovery}} {
		t.Run(tc.name, func(t *testing.T) {
			g := testGateway(t)
			g.resolver = dnsResolver(t, tc.records, tc.code)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := g.fetch(ctx, "recipient.example"); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}
func TestMailProfileAndReceiptCannotCrossProtocols(t *testing.T) {
	g := testGateway(t)
	now := time.Now().Unix()
	packet, err := smip.Sign(smip.Envelope{Version: smip.MailVersion, ID: "unit-id", From: g.domain, To: "recipient.example", Sender: "hello+tag@unit.example", Recipient: "team@recipient.example", Stream: "mail", Kind: "mail", Created: now, Expires: now + 3600, Payload: smip.Encode([]byte("content"))}, g.signer.Private)
	if err != nil {
		t.Fatal(err)
	}
	receipt := smip.NewReceipt(packet, g.signer.Private, now)
	key := smip.Key{Public: g.signer.Private.Public().(ed25519.PublicKey), NotBefore: now - 60, NotAfter: now + 3600}
	if receipt.Verify(packet, key) != nil {
		t.Fatal("mail receipt invalid")
	}
	receipt.Version = smip.Version
	if receipt.Verify(packet, key) == nil {
		t.Fatal("old receipt accepted")
	}
	packet.Envelope.Version = smip.Version
	if packet.Envelope.Validate() == nil {
		t.Fatal("mail accepted under chat/file version")
	}
}
