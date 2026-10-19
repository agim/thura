package collabora

import (
	"context"
	"encoding/xml"
	"errors"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/golang-jwt/jwt/v5"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"thura/internal/platform/servicehttp"
	"time"
)

const Prefix = "/api/v1/collabora"

type Config struct {
	ServerURL string `env:"COLLABORA_SERVER_URL"`
	AppURL    string `env:"COLLABORA_APP_URL"`
	Secret    string `env:"COLLABORA_TOKEN_SECRET"`
	CAFile    string `env:"COLLABORA_CA_FILE"`
}

func Load() (Config, error) {
	var c Config
	if err := env.Load(".", &c); err != nil {
		return c, err
	}
	c.ServerURL = strings.TrimRight(c.ServerURL, "/")
	c.AppURL = strings.TrimRight(c.AppURL, "/")
	if len(c.Secret) < 32 {
		return c, router.Errorf(503, "Collabora editing is not configured")
	}
	for _, raw := range []string{c.ServerURL, c.AppURL} {
		u, e := url.Parse(raw)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && u.Scheme != "http") {
			return c, router.Errorf(503, "Collabora service URLs must be HTTP origins")
		}
	}
	return c, nil
}
func (c Config) Sign(claims jwt.MapClaims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(c.Secret))
}
func (c Config) Verify(token string) (jwt.MapClaims, error) {
	p, e := jwt.Parse(token, func(t *jwt.Token) (any, error) { return []byte(c.Secret), nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithAudience("thura-collabora"), jwt.WithExpirationRequired())
	if e != nil || !p.Valid {
		return nil, errors.New("invalid document token")
	}
	claims, ok := p.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid document token")
	}
	return claims, nil
}

type action struct {
	Name string `xml:"name,attr"`
	Ext  string `xml:"ext,attr"`
	URL  string `xml:"urlsrc,attr"`
}
type discovery struct {
	Zones []struct {
		Apps []struct {
			Name    string   `xml:"name,attr"`
			Actions []action `xml:"action"`
		} `xml:"app"`
	} `xml:"net-zone"`
}

func (c Config) Editor(ctx context.Context, ext, contentType string, readOnly bool) (string, bool, error) {
	d, e := c.discover(ctx)
	if e != nil {
		return "", false, e
	}
	// Prefer an exact extension's read/edit contract over a generic MIME action.
	for _, mimeFallback := range []bool{false, true} {
		var view string
		for _, z := range d.Zones {
			for _, app := range z.Apps {
				for _, a := range app.Actions {
					matches := ext != "" && a.Ext == ext && !mimeFallback
					if mimeFallback {
						matches = contentType != "" && a.Ext == "" && app.Name == contentType
					}
					if !matches {
						continue
					}
					if a.Name == "view" || a.Name == "view_comment" {
						view = a.URL
					}
					if a.Name == "edit" && !readOnly {
						return editorPath(a.URL, false)
					}
				}
			}
		}
		if view != "" {
			return editorPath(view, true)
		}
	}

	return "", false, router.Errorf(422, "Collabora does not support this file format")
}

func (c Config) Formats(ctx context.Context) ([]string, []string, error) {
	d, err := c.discover(ctx)
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	mimes := map[string]bool{}
	for _, z := range d.Zones {
		for _, app := range z.Apps {
			for _, a := range app.Actions {
				if a.Name == "edit" || a.Name == "view" || a.Name == "view_comment" {
					if a.Ext != "" {
						seen[a.Ext] = true
					} else if strings.Contains(app.Name, "/") {
						mimes[app.Name] = true
					}
				}
			}
		}
	}
	result := make([]string, 0, len(seen))
	for ext := range seen {
		result = append(result, ext)
	}
	sort.Strings(result)
	contentTypes := make([]string, 0, len(mimes))
	for mime := range mimes {
		contentTypes = append(contentTypes, mime)
	}
	sort.Strings(contentTypes)
	return result, contentTypes, nil
}

func (c Config) discover(ctx context.Context) (discovery, error) {
	var d discovery
	client, e := servicehttp.Client(ctx, c.CAFile)
	if e != nil {
		return d, e
	}
	call, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(call, "GET", c.ServerURL+Prefix+"/hosting/discovery", nil)
	if e != nil {
		return d, e
	}
	res, e := client.Do(req)
	if e != nil {
		return d, router.Errorf(502, "Collabora could not be reached")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return d, router.Errorf(502, "Collabora discovery is unavailable")
	}
	if e = xml.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&d); e != nil {
		return d, router.Errorf(502, "Collabora returned invalid discovery data")
	}
	return d, nil
}
func editorPath(raw string, readOnly bool) (string, bool, error) {
	u, e := url.Parse(raw)
	if e != nil || !strings.HasPrefix(path.Clean(u.Path), Prefix+"/browser/") {
		return "", false, router.Errorf(502, "Collabora returned an unsupported editor path")
	}
	return u.Path, readOnly, nil
}
