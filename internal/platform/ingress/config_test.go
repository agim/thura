package ingress

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/agim/lidza/pkg/gateway"
)

func deploymentFixture(t *testing.T) map[string]string {
	t.Helper()
	ca := newTestAuthority(t)
	server, peer, edge := ca.issue(t, "app.internal"), ca.issue(t, "agent.internal"), ca.issue(t, "edge.internal")
	key, err := x509.MarshalPKCS8PrivateKey(server.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	encoded := func(kind string, der []byte) string {
		return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}))
	}
	return map[string]string{
		"DEPLOY_GATEWAY_ADDR":          "0.0.0.0:3001",
		"DEPLOY_GATEWAY_CERT":          encoded("CERTIFICATE", server.Certificate[0]),
		"DEPLOY_GATEWAY_KEY":           encoded("PRIVATE KEY", key),
		"DEPLOY_GATEWAY_CLIENT_CA":     encoded("CERTIFICATE", ca.cert.Raw),
		"DEPLOY_GATEWAY_PEER_PINS":     gateway.Pin(peer.Leaf),
		"DEPLOY_GATEWAY_DELEGATE_PINS": gateway.Pin(edge.Leaf),
		"DEPLOY_GATEWAY_AUTHORITIES":   "thura.example,team.example",
		"CACHE_URL":                    "redis://127.0.0.1:6379/1",
	}
}

func TestDeploymentGatewayConfigurationIsAtomicAndPrivate(t *testing.T) {
	absent, err := LoadDeployment(func(string) string { return "" })
	if absent != nil || err != nil {
		t.Fatalf("standalone configuration: %v", err)
	}
	values := deploymentFixture(t)
	cfg, err := LoadDeployment(func(name string) string { return values[name] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Address != "0.0.0.0:3001" || cfg.TLS.MinVersion != tls.VersionTLS13 || cfg.TLS.ClientAuth != tls.RequireAndVerifyClientCert || len(cfg.Policy.NativeEdges) != 0 {
		t.Fatal("private gateway transport was weakened")
	}
	for _, authority := range []string{"thura.example", "team.example", "thura.example:443"} {
		if err := cfg.Policy.Authorize(context.Background(), authority); err != nil {
			t.Fatal(err)
		}
	}
	for _, authority := range []string{"attacker.example", "thura.example.attacker.example", "thura.example:444", "THURA.EXAMPLE", "thura.example."} {
		if err := cfg.Policy.Authorize(context.Background(), authority); err == nil {
			t.Fatalf("unconfigured authority accepted: %s", authority)
		}
	}
	for _, field := range append(append([]string{}, deploymentFields()...), "CACHE_URL") {
		name := "DEPLOY_GATEWAY_" + field
		if field == "CACHE_URL" {
			name = field
		}
		t.Run("missing_"+field, func(t *testing.T) {
			_, err := LoadDeployment(func(key string) string {
				if key == name {
					return ""
				}
				return values[key]
			})
			if err == nil {
				t.Fatal("partial gateway configuration accepted")
			}
		})
	}
	for field, bad := range map[string]string{"ADDR": "localhost:3001", "CERT": "invalid", "KEY": "invalid", "CLIENT_CA": "invalid", "PEER_PINS": "invalid", "DELEGATE_PINS": "invalid", "AUTHORITIES": "https://thura.example"} {
		t.Run("invalid_"+field, func(t *testing.T) {
			_, err := LoadDeployment(func(key string) string {
				if key == "DEPLOY_GATEWAY_"+field {
					return bad
				}
				return values[key]
			})
			if err == nil {
				t.Fatal("invalid gateway configuration accepted")
			}
			if strings.Contains(err.Error(), values["DEPLOY_GATEWAY_KEY"]) {
				t.Fatal("private key exposed in error")
			}
		})
	}
	for _, bad := range []string{"memory", "", "https://cache.example", "file:///tmp/cache"} {
		_, err := LoadDeployment(func(key string) string {
			if key == "CACHE_URL" {
				return bad
			}
			return values[key]
		})
		if err == nil {
			t.Fatalf("unshared nonce store accepted: %s", bad)
		}
	}
}
