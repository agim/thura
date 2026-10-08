package onlyoffice

import (
	"context"
	"errors"
	"fmt"
	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/golang-jwt/jwt/v5"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	ServerURL string `env:"OFFICE_SERVER_URL"`
	AppURL    string `env:"OFFICE_APP_URL"`
	Secret    string `env:"OFFICE_JWT_SECRET"`
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
	if err != nil || u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) || u.User != nil {
		return nil, router.Errorf(400, "document URL is outside the configured service")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	client := *lidza.HTTPClient(ctx)
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
