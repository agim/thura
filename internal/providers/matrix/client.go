// Package matrix keeps the application-service token on the control plane.
// Virtual users cannot log in with a Thura session or use this credential.
package matrix

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	URL        string `env:"MATRIX_SERVER_URL"`
	ServerName string `env:"MATRIX_SERVER_NAME"`
	Token      string `env:"MATRIX_AS_TOKEN"`
}

func Load() (Config, error) {
	var c Config
	if err := env.Load(".", &c); err != nil {
		return c, err
	}
	c.URL = strings.TrimRight(c.URL, "/")
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || len(c.Token) < 32 || c.ServerName == "" || strings.ContainsAny(c.ServerName, "/?#@ \n\r") {
		return c, router.Errorf(503, "Matrix chat is not configured")
	}
	return c, nil
}
func (c Config) User(subject string) string {
	h := sha256.Sum256([]byte(subject))
	return "@thura_" + hex.EncodeToString(h[:16]) + ":" + c.ServerName
}
func (c Config) Bot() string { return "@thura_bot:" + c.ServerName }
func (c Config) Call(ctx context.Context, as, method, path string, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	u, err := url.Parse(c.URL + path)
	if err != nil {
		return err
	}
	query := u.Query()
	if as != "" {
		query.Set("user_id", as)
	}
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	client := *lidza.HTTPClient(ctx)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return router.Errorf(503, "Matrix service is unavailable")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024+1))
	if err != nil || len(raw) > 2*1024*1024 {
		return router.Errorf(502, "invalid Matrix response")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var e struct {
			Code string `json:"errcode"`
		}
		if err = json.Unmarshal(raw, &e); err != nil {
			return router.Errorf(502, "invalid Matrix error response")
		}
		return &Error{Status: res.StatusCode, Code: e.Code}
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return router.Errorf(502, "invalid Matrix response")
	}
	return nil
}

type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string {
	return fmt.Sprintf("Matrix request failed (%d, %s)", e.Status, e.Code)
}
func (c Config) EnsureUser(ctx context.Context, user string) error {
	local := strings.SplitN(strings.TrimPrefix(user, "@"), ":", 2)[0]
	err := c.Call(ctx, "", "POST", "/_matrix/client/v3/register", map[string]any{"type": "m.login.application_service", "username": local, "inhibit_login": true}, nil)
	if e, ok := err.(*Error); ok && e.Code == "M_USER_IN_USE" {
		return nil
	}
	return err
}
func (c Config) Join(ctx context.Context, room, user string) error {
	var state struct {
		Membership string `json:"membership"`
	}
	if c.Call(ctx, c.Bot(), "GET", "/_matrix/client/v3/rooms/"+url.PathEscape(room)+"/state/m.room.member/"+url.PathEscape(user), nil, &state) == nil && state.Membership == "join" {
		return nil
	}

	if err := c.EnsureUser(ctx, user); err != nil {
		return err
	}
	err := c.Call(ctx, c.Bot(), "POST", "/_matrix/client/v3/rooms/"+url.PathEscape(room)+"/invite", map[string]string{"user_id": user}, nil)
	if err != nil { // Inviting an already joined member is rejected; only accept a proven join.
		var member struct {
			Membership string `json:"membership"`
		}
		if c.Call(ctx, c.Bot(), "GET", "/_matrix/client/v3/rooms/"+url.PathEscape(room)+"/state/m.room.member/"+url.PathEscape(user), nil, &member) != nil || member.Membership != "join" {
			return err
		}
	}
	return c.Call(ctx, user, "POST", "/_matrix/client/v3/join/"+url.PathEscape(room), map[string]any{}, nil)
}
