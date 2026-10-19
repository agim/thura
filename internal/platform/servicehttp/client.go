package servicehttp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/agim/lidza"
	"net/http"
	"os"
)

func Client(ctx context.Context, caFile string) (*http.Client, error) {
	client := *lidza.HTTPClient(ctx)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("document service CA is invalid")
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if existing, ok := client.Transport.(*http.Transport); ok {
			transport = existing.Clone()
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
		client.Transport = transport
	}
	return &client, nil
}
