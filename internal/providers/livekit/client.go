package livekit

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/livekit/protocol/auth"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Config struct {
	URL       string `env:"LIVEKIT_SERVER_URL"`
	PublicURL string `env:"LIVEKIT_PUBLIC_URL"`
	Key       string `env:"LIVEKIT_API_KEY"`
	Secret    string `env:"LIVEKIT_API_SECRET"`
}

func Load() (Config, error) {
	var c Config
	if err := env.Load(".", &c); err != nil {
		return c, err
	}
	c.URL = strings.TrimRight(c.URL, "/")
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	if c.Key == "" || len(c.Secret) < 32 {
		return c, router.Errorf(503, "LIVEKIT_API_KEY and a LIVEKIT_API_SECRET of at least 32 characters are required")
	}
	api, err := url.Parse(c.URL)
	if err != nil || (api.Scheme != "http" && api.Scheme != "https") || api.Host == "" || api.User != nil || api.Path != "" || api.RawQuery != "" || api.Fragment != "" {
		return c, router.Errorf(503, "LIVEKIT_SERVER_URL must be an http(s) origin without a path")
	}
	// The public URL may carry a path: a proxy that forwards /rtc to LiveKit
	// and strips it serves wss://<domain>/rtc (deploy/lidza.stack.json).
	public, err := url.Parse(c.PublicURL)
	if err != nil || (public.Scheme != "ws" && public.Scheme != "wss") || public.Host == "" || public.User != nil || public.RawQuery != "" || public.Fragment != "" {
		return c, router.Errorf(503, "LIVEKIT_PUBLIC_URL must be a ws(s) URL without credentials, query or fragment")
	}

	return c, nil
}

// PublicOrigins are the WebSocket origin of PublicURL and its HTTP twin, the
// CSP connect-src entries the browser needs; an origin covers every path.
func (c Config) PublicOrigins() []string {
	u, err := url.Parse(c.PublicURL)
	if err != nil {
		return nil
	}
	scheme := map[string]string{"wss": "https", "ws": "http"}[u.Scheme]
	return []string{u.Scheme + "://" + u.Host, scheme + "://" + u.Host}
}
func (c Config) Token(subject, name, room string) (string, error) {
	yes, no := true, false
	return auth.NewAccessToken(c.Key, c.Secret).SetIdentity(subject).SetName(name).SetValidFor(2 * time.Minute).SetVideoGrant(&auth.VideoGrant{RoomJoin: true, Room: room, CanPublish: &yes, CanSubscribe: &yes, CanPublishData: &yes, CanUpdateOwnMetadata: &no, CanPublishSources: []string{"camera", "microphone", "screen_share", "screen_share_audio"}}).ToJWT()
}
func (c Config) Call(ctx context.Context, method, room string, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	token, err := auth.NewAccessToken(c.Key, c.Secret).SetValidFor(time.Minute).SetVideoGrant(&auth.VideoGrant{RoomAdmin: true, RoomCreate: method == "CreateRoom" || method == "DeleteRoom", Room: room}).ToJWT()
	if err != nil {
		return err
	}
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.URL+"/twirp/livekit.RoomService/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := *lidza.HTTPClient(ctx)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return router.Errorf(503, "Meet service unavailable")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return router.Errorf(502, "invalid meeting service response")
	}
	if res.StatusCode == 404 && (method == "RemoveParticipant" || method == "DeleteRoom") {
		return nil
	}
	if res.StatusCode != 200 {
		return router.Errorf(502, "meeting service request failed")
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return router.Errorf(502, "invalid meeting response")
	}
	return nil
}
