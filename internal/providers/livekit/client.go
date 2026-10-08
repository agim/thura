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
	if len(c.Secret) < 32 || c.Key == "" {
		return c, router.Errorf(503, "Meet service is not configured")
	}
	for i, s := range []string{c.URL, c.PublicURL} {
		u, err := url.Parse(s)
		if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return c, router.Errorf(503, "invalid meeting service origins")
		}
		if i == 0 && u.Scheme != "http" && u.Scheme != "https" {
			return c, router.Errorf(503, "invalid meeting API origin")
		}
		if i == 1 && u.Scheme != "ws" && u.Scheme != "wss" {
			return c, router.Errorf(503, "invalid meeting WebSocket origin")
		}
	}

	return c, nil
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
