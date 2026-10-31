package livekit

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/credentials"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/livekit/protocol/auth"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	URL       string `env:"LIVEKIT_SERVER_URL"`
	PublicURL string `env:"LIVEKIT_PUBLIC_URL"`
	Key       string `env:"LIVEKIT_API_KEY"`
	Secret    string `env:"LIVEKIT_API_SECRET"`
}

// BundledURL is the API origin of the LiveKit that deploy/compact/app.py runs
// in the same container; app.py marks that case with THURA_BUNDLED_LIVEKIT.
const BundledURL = "http://127.0.0.1:7880"

// settingNames are the names Load reads; APP_URL gives the default public URL.
func settingNames() [5]string {
	return [5]string{"LIVEKIT_SERVER_URL", "LIVEKIT_PUBLIC_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET", "APP_URL"}
}

// Load reads the LiveKit settings from every layer pkg/env merges. An empty
// process variable (Compose passes LIVEKIT_PUBLIC_URL through as "") counts
// as unset, so a value saved in the app's settings still applies; Resolve's
// defaults fill what is left.
func Load() (Config, error) {
	values, err := env.Values(".")
	if err != nil {
		return Config{}, err
	}
	// pkg/env invents http://<LIDZA_ADDR> for APP_URL in development and
	// tests; that loopback address is no public origin to derive from.
	if m := env.Mode(); (m == "dev" || m == "test") && os.Getenv("APP_URL") == "" {
		addr := values["LIDZA_ADDR"]
		if addr == "" {
			addr = "127.0.0.1:3000"
		}
		if values["APP_URL"] == "http://"+addr {
			values["APP_URL"] = ""
		}
	}
	names := settingNames()
	v := make(map[string]string, len(names))
	var saved map[string]string
	for _, name := range names {
		v[name] = values[name]
		if v[name] != "" {
			continue
		}
		if saved == nil {
			// The credentials layer of pkg/env: under test the runtime values
			// alone, never the deployment's sealed file.
			if env.Mode() == "test" {
				saved = credentials.Overrides()
			} else {
				saved = credentials.Values(".")
			}
		}
		v[name] = saved[name]
	}
	return Resolve(v)
}

// Resolve builds the configuration from settings by name (a saved setup
// draft, or what Load read) and validates it. A blank LIVEKIT_PUBLIC_URL is
// wss://<APP_URL host>/rtc, the proxy route of deploy/lidza.stack.json; a
// blank LIVEKIT_SERVER_URL is BundledURL, but only beside a bundled LiveKit.
func Resolve(v map[string]string) (Config, error) {
	c := Config{URL: strings.TrimRight(v["LIVEKIT_SERVER_URL"], "/"), PublicURL: strings.TrimRight(v["LIVEKIT_PUBLIC_URL"], "/"), Key: v["LIVEKIT_API_KEY"], Secret: v["LIVEKIT_API_SECRET"]}
	if c.URL == "" && Bundled() {
		c.URL = BundledURL
	}
	if c.PublicURL == "" {
		c.PublicURL = DefaultPublicURL(v["APP_URL"])
	}
	return c, c.Validate()
}

// Bundled reports whether LiveKit runs in this container (deploy/compact).
func Bundled() bool { return os.Getenv("THURA_BUNDLED_LIVEKIT") == "true" }

// DefaultPublicURL is wss://<host>/rtc for an https app origin (ws:// for
// http), or "" when appURL is no origin.
func DefaultPublicURL(appURL string) string {
	u, err := url.Parse(strings.TrimSpace(appURL))
	if err != nil || u.Host == "" || u.User != nil {
		return ""
	}
	scheme := map[string]string{"https": "wss", "http": "ws"}[u.Scheme]
	if scheme == "" {
		return ""
	}
	return scheme + "://" + u.Host + "/rtc"
}

// Validate checks every setting. ValidateAPI and ValidatePublic check the
// server-side and the browser-side half, so a setup check can report each.
func (c Config) Validate() error {
	if err := c.ValidateAPI(); err != nil {
		return err
	}
	return c.ValidatePublic()
}

func (c Config) ValidateAPI() error {
	if c.Key == "" || len(c.Secret) < 32 {
		return router.Errorf(503, "LIVEKIT_API_KEY and a LIVEKIT_API_SECRET of at least 32 characters are required")
	}
	if c.URL == "" {
		return router.Errorf(503, "LIVEKIT_SERVER_URL is required: LiveKit does not run beside Thura here, so set its API origin")
	}
	api, err := url.Parse(c.URL)
	if err != nil || (api.Scheme != "http" && api.Scheme != "https") || api.Host == "" || api.User != nil || api.Path != "" || api.RawQuery != "" || api.Fragment != "" {
		return router.Errorf(503, "LIVEKIT_SERVER_URL must be an http(s) origin without a path")
	}
	return nil
}

func (c Config) ValidatePublic() error {
	if c.PublicURL == "" {
		return router.Errorf(503, "LIVEKIT_PUBLIC_URL is required while the public HTTPS address (APP_URL) is not set")
	}
	// The public URL may carry a path: a proxy that forwards /rtc to LiveKit
	// and strips it serves wss://<domain>/rtc (deploy/lidza.stack.json).
	public, err := url.Parse(c.PublicURL)
	if err != nil || (public.Scheme != "ws" && public.Scheme != "wss") || public.Host == "" || public.User != nil || public.RawQuery != "" || public.Fragment != "" {
		return router.Errorf(503, "LIVEKIT_PUBLIC_URL must be a ws(s) URL without credentials, query or fragment")
	}
	return nil
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
	token, err := auth.NewAccessToken(c.Key, c.Secret).SetValidFor(time.Minute).SetVideoGrant(&auth.VideoGrant{RoomAdmin: true, RoomCreate: method == "CreateRoom" || method == "DeleteRoom", RoomList: method == "ListRooms", Room: room}).ToJWT()
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
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return router.Errorf(502, "meeting service rejected the API key or secret")
	}
	if res.StatusCode != 200 {
		return router.Errorf(502, "meeting service request failed")
	}
	if out != nil && json.Unmarshal(raw, out) != nil {
		return router.Errorf(502, "invalid meeting response")
	}
	return nil
}
