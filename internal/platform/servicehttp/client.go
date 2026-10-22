package servicehttp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/agim/lidza"
	"net/http"
	"os"
	"sync"
)

// transports keeps one Transport per CA file so its connections are reused;
// a fresh Transport per call never closed its idle connections. A changed
// file (size or modification time) replaces the entry and closes the old one.
var transports struct {
	sync.Mutex
	file string
	size int64
	mod  int64
	tr   *http.Transport
}

func Client(ctx context.Context, caFile string) (*http.Client, error) {
	client := *lidza.HTTPClient(ctx)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if caFile != "" {
		transport, err := caTransport(client.Transport, caFile)
		if err != nil {
			return nil, err
		}
		client.Transport = transport
	}
	return &client, nil
}

func caTransport(base http.RoundTripper, caFile string) (*http.Transport, error) {
	info, err := os.Stat(caFile)
	if err != nil {
		return nil, err
	}
	transports.Lock()
	defer transports.Unlock()
	if transports.tr != nil && transports.file == caFile && transports.size == info.Size() && transports.mod == info.ModTime().UnixNano() {
		return transports.tr, nil
	}
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
	if existing, ok := base.(*http.Transport); ok {
		transport = existing.Clone()
	}
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	if transports.tr != nil {
		transports.tr.CloseIdleConnections()
	}
	transports.file, transports.size, transports.mod, transports.tr = caFile, info.Size(), info.ModTime().UnixNano(), transport
	return transport, nil
}
