package ingress

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/agim/lidza/packs/cache"
	"github.com/agim/lidza/pkg/gateway"
)

// DeploymentConfig is the published Lidza-deploy v2 gateway contract. No part
// of this private transport is operator-entered or inferred from HTTP headers.
type DeploymentConfig struct {
	Address string
	TLS     *tls.Config
	Policy  gateway.Policy
}

func deploymentFields() []string {
	return []string{"ADDR", "CERT", "KEY", "CLIENT_CA", "PEER_PINS", "DELEGATE_PINS", "AUTHORITIES"}
}

func LoadDeployment(lookup func(string) string) (*DeploymentConfig, error) {
	present := false
	for _, name := range deploymentFields() {
		present = present || lookup("DEPLOY_GATEWAY_"+name) != ""
	}
	if !present {
		return nil, nil
	}
	value := func(name string) string { return lookup("DEPLOY_GATEWAY_" + name) }
	for _, name := range deploymentFields() {
		if value(name) == "" {
			return nil, fmt.Errorf("incomplete deployment gateway configuration: DEPLOY_GATEWAY_%s", name)
		}
	}
	host, port, err := net.SplitHostPort(value("ADDR"))
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || number < 1 || number > 65535 || (host != "" && net.ParseIP(host) == nil) {
		return nil, errors.New("invalid deployment gateway listener address")
	}
	cacheURL, err := url.Parse(lookup("CACHE_URL"))
	if err != nil || cacheURL.Host == "" || (cacheURL.Scheme != "redis" && cacheURL.Scheme != "rediss" && cacheURL.Scheme != "valkey" && cacheURL.Scheme != "valkeys") {
		return nil, errors.New("deployment gateway requires a shared Valkey/Redis CACHE_URL; memory cache is not permitted")
	}
	decode := func(name string) ([]byte, error) {
		raw, err := base64.StdEncoding.DecodeString(value(name))
		if err != nil || len(raw) == 0 {
			return nil, fmt.Errorf("invalid deployment gateway PEM encoding: DEPLOY_GATEWAY_%s", name)
		}
		return raw, nil
	}
	certPEM, err := decode("CERT")
	if err != nil {
		return nil, err
	}
	keyPEM, err := decode("KEY")
	if err != nil {
		return nil, err
	}
	caPEM, err := decode("CLIENT_CA")
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, errors.New("invalid deployment gateway certificate and key")
	}
	clients := x509.NewCertPool()
	if !clients.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("invalid deployment gateway client CA")
	}
	serverTLS, err := gateway.ServerTLS(cert, clients)
	if err != nil {
		return nil, errors.New("invalid deployment gateway TLS configuration")
	}
	authorities := map[string]bool{}
	for _, authority := range strings.Split(value("AUTHORITIES"), ",") {
		// The deployment contract supplies canonical hostnames, never URLs or ports.
		if authority == "" || len(authority) > 253 || strings.ToLower(authority) != authority || strings.ContainsAny(authority, "/:@?#%\\ \t\r\n") || strings.HasSuffix(authority, ".") {
			return nil, errors.New("invalid deployment gateway authority")
		}
		for _, label := range strings.Split(authority, ".") {
			if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return nil, errors.New("invalid deployment gateway authority")
			}
			for _, char := range label {
				if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
					return nil, errors.New("invalid deployment gateway authority")
				}
			}
		}
		authorities[authority] = true
	}
	policy := gateway.Policy{Peers: strings.Split(value("PEER_PINS"), ","), Delegates: strings.Split(value("DELEGATE_PINS"), ","), Claim: gateway.Claims(sharedCounter{}, "thura:gateway:v1:"), Authorize: func(_ context.Context, authority string) error {
		// An explicit default HTTPS port is equivalent to the hostname; other ports,
		// suffix matches and attacker-controlled forwarded hosts are not accepted.
		authority = strings.TrimSuffix(authority, ":443")
		if !authorities[authority] {
			return errors.New("gateway authority not configured for this installation")
		}
		return nil
	}}
	if _, err := Protect(policy); err != nil {
		return nil, errors.New("invalid deployment gateway peer or delegate pins")
	}
	return &DeploymentConfig{Address: value("ADDR"), TLS: serverTLS, Policy: policy}, nil
}

type sharedCounter struct{}

func (sharedCounter) Incr(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error) {
	return cache.From(ctx).Incr(ctx, key, delta, ttl)
}
