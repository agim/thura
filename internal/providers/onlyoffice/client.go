package onlyoffice

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/golang-jwt/jwt/v5"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

type Config struct {
	ServerURL string `env:"OFFICE_SERVER_URL"`
	AppURL    string `env:"OFFICE_APP_URL"`
	Secret    string `env:"OFFICE_JWT_SECRET"`
	CAFile    string `env:"OFFICE_CA_FILE"`
}

func Load() (Config, error) {
	var c Config
	if err := env.Load(".", &c); err != nil {
		return c, err
	}
	c.ServerURL = strings.TrimRight(c.ServerURL, "/")
	c.AppURL = strings.TrimRight(c.AppURL, "/")
	if len(c.Secret) < 32 {
		return c, router.Errorf(503, "Document editing is not configured")
	}
	for _, s := range []string{c.ServerURL, c.AppURL} {
		u, e := url.Parse(s)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" {
			return c, router.Errorf(503, "Document service URLs must be HTTP origins")
		}
	}
	return c, nil
}
func (c Config) Sign(claims jwt.MapClaims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(c.Secret))
}
func (c Config) Verify(token string) (jwt.MapClaims, error) {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) { return []byte(c.Secret), nil }, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !parsed.Valid {
		return nil, router.Errorf(401, "invalid document signature")
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, router.Errorf(401, "invalid document signature")
	}
	return claims, nil
}

// The signed callback's URL is still constrained to the operator's fixed
// document-server origin. Redirects are refused to prevent a cache URL from
// being used as a proxy to unrelated hosts.
func (c Config) Download(ctx context.Context, address string) ([]byte, error) {
	u, err := url.Parse(address)
	base, baseErr := url.Parse(c.ServerURL)
	if baseErr != nil {
		return nil, baseErr
	}
	// The authenticated browser proxy makes ONLYOFFICE generate cache links
	// on Thura's origin. Map only its cache subtree back to the fixed service;
	// never fetch the callback's supplied host or follow a redirect.
	const prefix = "/api/v1/office/editor/cache/"
	if err == nil && u.User == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.HasPrefix(path.Clean(u.Path), prefix) {
		u.Scheme, u.Host = base.Scheme, base.Host
		u.Path = "/cache/" + strings.TrimPrefix(path.Clean(u.Path), prefix)
		u.RawPath = ""
	}
	if err != nil || u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) || u.User != nil {
		return nil, router.Errorf(400, "document URL is outside the configured service")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	client, err := c.client(ctx)
	if err != nil {
		return nil, err
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("document service returned %d", response.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, (10<<20)+1))
	if len(b) == 0 || len(b) > 10<<20 {
		return nil, errors.New("saved document size is outside the upload limit")
	}
	return b, err
}

func (c Config) client(ctx context.Context) (*http.Client, error) {
	client := *lidza.HTTPClient(ctx)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if c.CAFile != "" {
		pem, err := os.ReadFile(c.CAFile)
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

type Conversion struct {
	End     bool   `json:"endConvert"`
	URL     string `json:"fileUrl"`
	Percent int    `json:"percent"`
	Error   int    `json:"error"`
}

// Convert sends only the signed source ticket to the operator's fixed service.
func (c Config) Convert(ctx context.Context, input, target, key, source string) (Conversion, error) {
	payload := jwt.MapClaims{"async": true, "filetype": input, "outputtype": target, "key": key, "url": source}
	token, err := c.Sign(payload)
	if err != nil {
		return Conversion{}, err
	}
	payload["token"] = token
	raw, err := json.Marshal(payload)
	if err != nil {
		return Conversion{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.ServerURL+"/converter", bytes.NewReader(raw))
	if err != nil {
		return Conversion{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	client, err := c.client(ctx)
	if err != nil {
		return Conversion{}, err
	}
	res, err := client.Do(req)
	if err != nil {
		return Conversion{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Conversion{}, fmt.Errorf("document conversion returned %d", res.StatusCode)
	}
	var out Conversion
	if err = json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&out); err != nil {
		return out, err
	}
	if out.Error != 0 {
		return out, fmt.Errorf("document conversion failed (%d)", out.Error)
	}
	return out, nil
}
