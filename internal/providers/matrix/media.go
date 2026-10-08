package matrix

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/router"
	"io"
	"net/http"
	"net/url"
	"time"
)

func (c Config) Media(ctx context.Context, user, method, path string, data []byte, kind string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	u, err := url.Parse(c.URL + path)
	if err != nil {
		return nil, err
	}
	v := u.Query()
	v.Set("user_id", user)
	u.RawQuery = v.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", kind)
	client := *lidza.HTTPClient(ctx)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return nil, router.Errorf(503, "Matrix media service unavailable")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (10<<20)+1))
	if err != nil || len(raw) > 10<<20 {
		return nil, router.Errorf(422, "attachment exceeds 10 MB")
	}
	if res.StatusCode != 200 {
		return nil, router.Errorf(502, "Matrix media request failed")
	}
	return raw, nil
}
func (c Config) Upload(ctx context.Context, user, name string, data []byte, kind string) (string, error) {
	raw, err := c.Media(ctx, user, "POST", "/_matrix/media/v3/upload?filename="+url.QueryEscape(name), data, kind)
	if err != nil {
		return "", err
	}
	var v struct {
		URI string `json:"content_uri"`
	}
	if json.Unmarshal(raw, &v) != nil || v.URI == "" {
		return "", router.Errorf(502, "invalid media response")
	}
	return v.URI, nil
}
