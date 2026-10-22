package smiptrust

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestValidOriginAcceptsOnlyBareHTTPSOrigins(t *testing.T) {
	for _, good := range []string{"https://smip.partner.example", "https://10.0.0.5:8443", "https://[fd00::5]:443", "https://partner.example/"} {
		if !ValidOrigin(good) {
			t.Errorf("refused %q", good)
		}
	}
	for _, bad := range []string{"", "http://partner.example", "https://", "https://user:pass@partner.example", "https://partner.example/path", "https://partner.example?x=1", "https://partner.example?", "https://partner.example#frag", "https://partner.example:0", "https://partner.example:65536", "/etc/ssl/ca.pem", "file:///etc/ssl/ca.pem", "partner.example"} {
		if ValidOrigin(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
}

func testCertificate(t *testing.T, ca bool, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "Private SMIP test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: notAfter, IsCA: ca, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestParseBundleValidatesEveryBlock(t *testing.T) {
	now := time.Now()
	ca := testCertificate(t, true, now.Add(24*time.Hour))
	expired := testCertificate(t, false, now.Add(-time.Minute))
	certs, err := ParseBundle(append(append([]byte{}, ca...), expired...), now)
	if err != nil || len(certs) != 2 {
		t.Fatal(certs, err)
	}
	if !certs[0].CA || certs[0].Expired || !strings.Contains(certs[0].Subject, "Private SMIP test CA") || len(certs[0].Fingerprint) != 64 || certs[1].CA || !certs[1].Expired {
		t.Fatalf("unexpected description %+v", certs)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	private := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	for name, bad := range map[string][]byte{
		"empty":       nil,
		"text":        []byte("not a certificate"),
		"private key": append(append([]byte{}, ca...), private...),
		"other block": pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: []byte{1}}),
		"unparsable":  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("truncated")}),
		"trailing":    append(append([]byte{}, ca...), []byte("trailing")...),
		"headers":     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Headers: map[string]string{"Proc-Type": "4,ENCRYPTED"}, Bytes: []byte{1}}),
		"too large":   []byte(strings.Repeat(string(ca), MaxBundleBytes/len(ca)+1)),
		"too many":    []byte(strings.Repeat(string(ca), MaxCertificates+1)),
	} {
		if _, err := ParseBundle(bad, now); err == nil {
			t.Errorf("%s bundle accepted", name)
		}
	}
	if _, err := ParseBundle(private, now); err == nil || !strings.Contains(err.Error(), "private key") {
		t.Errorf("private key not named: %v", err)
	}
}

// Outside the application there is no database: only the deployment JSON
// applies, unchanged.
func TestWithoutDatabaseOnlyConfiguredValuesApply(t *testing.T) {
	ctx := context.Background()
	merged, err := MergeOrigins(ctx, map[string]string{"partner.example": "https://10.0.0.5"})
	if err != nil || len(merged) != 1 || merged["partner.example"] != "https://10.0.0.5" {
		t.Fatal(merged, err)
	}
	pool := x509.NewCertPool()
	roots, err := Roots(ctx, pool)
	if err != nil || roots != pool {
		t.Fatal("configured roots replaced without a saved bundle", err)
	}
}
