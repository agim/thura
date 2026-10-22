package servicehttp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func selfSigned(t *testing.T, name string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// One Transport per CA file: a new one per call never closed its idle
// connections. A changed file replaces it.
func TestClientReusesTransportPerCAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, selfSigned(t, "one"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := Client(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Client(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if first.Transport != second.Transport {
		t.Fatal("unchanged CA file produced a second transport")
	}
	if err = os.WriteFile(path, selfSigned(t, "two"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err = os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	third, err := Client(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if third.Transport == first.Transport {
		t.Fatal("changed CA file kept the old transport")
	}
	if err = os.WriteFile(path, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = Client(ctx, path); err == nil {
		t.Fatal("invalid CA file was accepted")
	}
	plain, err := Client(ctx, "")
	if err != nil || plain.Transport == third.Transport {
		t.Fatal("client without a CA file must not use the CA transport")
	}
}
