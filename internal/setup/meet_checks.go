package setup

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/agim/lidza/packs/db"
	q "thura/db/queries/gen"
	"thura/internal/providers/livekit"
)

// MeetCheck is one named step of the Meetings check. State is passed,
// failed, skipped (an earlier step must pass first) or manual (a deployment
// task this server cannot verify).
type MeetCheck struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	State  string `json:"state"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
}

// meetFailure carries the failed steps of a Meetings probe, so the persisted
// diagnostic names each one with an app-owned message.
type meetFailure struct{ checks []MeetCheck }

func (m meetFailure) Error() string {
	var parts []string
	for _, c := range m.checks {
		if c.State == "failed" {
			parts = append(parts, c.Name+": "+c.Detail)
		}
	}
	return strings.Join(parts, "; ")
}

// Messages a Meetings diagnostic may persist; anything else is reported as
// "failed" so no provider text reaches the database.
func meetMessages() []string {
	return []string{
		"LIVEKIT_API_KEY and a LIVEKIT_API_SECRET of at least 32 characters are required",
		"LIVEKIT_SERVER_URL is required: LiveKit does not run beside Thura here, so set its API origin",
		"LIVEKIT_SERVER_URL must be an http(s) origin without a path",
		"LIVEKIT_PUBLIC_URL is required while the public HTTPS address (APP_URL) is not set",
		"LIVEKIT_PUBLIC_URL must be a ws(s) URL without credentials, query or fragment",
		"meeting service rejected the API key or secret",
		"Meet service unavailable",
		"meeting service request failed",
		"invalid meeting service response",
		livekit.ErrSignalNotForwarded.Error(),
		livekit.ErrSignalTimeout.Error(),
		livekit.ErrSignalUnreachable.Error(),
		livekit.ErrSignalRefusedHost.Error(),
		livekit.ErrSignalTLS.Error(),
	}
}

func meetMessage(err error) string {
	text := err.Error()
	for _, known := range meetMessages() {
		if strings.Contains(text, known) {
			return known
		}
	}
	return "failed"
}

func meetDiagnostic(err error) (string, bool) {
	var failure meetFailure
	if !errors.As(err, &failure) {
		return "", false
	}
	return failure.Error() + ".", true
}

func appHost(v map[string]string) string {
	if u, err := url.Parse(v["APP_URL"]); err == nil {
		return u.Hostname()
	}
	return ""
}

// meetChecks runs the named steps against settings by name (the saved
// draft). api exercises the authenticated API: the probe creates and deletes
// a room, the page's status lists rooms.
func meetChecks(ctx context.Context, v map[string]string, api func(context.Context, livekit.Config) error) []MeetCheck {
	settings := MeetCheck{ID: "settings", Name: "Settings present and valid", State: "passed"}
	reach := MeetCheck{ID: "api", Name: "LiveKit API reachable and authenticated", State: "skipped", Detail: "Waiting for valid settings."}
	signal := MeetCheck{ID: "signal", Name: "Public signalling reachable through the proxy", State: "skipped", Detail: "Waiting for a valid public URL."}
	media := MeetCheck{ID: "media", Name: "Media ports open", State: "manual", Detail: "Cannot be verified from inside the server.", Fix: "Open TCP 7881 and UDP 7882 to the internet. Behind NAT, set THURA_RTC_NODE_IP to the public address."}
	c, err := livekit.Resolve(v)
	if err != nil {
		settings.State, settings.Detail, settings.Fix = "failed", meetMessage(err), settingsFix(err)
	} else {
		settings.Detail = "Thura calls " + c.URL + "; browsers connect to " + c.PublicURL + "."
	}
	if c.ValidateAPI() == nil {
		if err := api(ctx, c); err != nil {
			reach.State, reach.Detail = "failed", meetMessage(err)
			reach.Fix = "Start LiveKit and check that this server reaches the LiveKit server URL."
			if strings.Contains(err.Error(), "rejected the API key") {
				reach.Fix = "Use the API key and secret LiveKit was started with (LIVEKIT_API_KEY and LIVEKIT_API_SECRET)."
			}
		} else {
			reach.State, reach.Detail = "passed", "LiveKit accepted an authenticated API request."
		}
	}
	if target, err := c.ValidateURL(); err == nil {
		if err := c.CheckSignal(ctx, appHost(v), nil); err != nil {
			signal.State, signal.Detail = "failed", meetMessage(err)
			switch {
			case errors.Is(err, livekit.ErrSignalNotForwarded):
				signal.Fix = "Add the proxy rule below so " + target.Scheme + "://" + target.Host + strings.TrimSuffix(target.Path, "/rtc/validate") + "/ reaches port 7880 with the prefix removed, then reload the proxy."
			case errors.Is(err, livekit.ErrSignalRefusedHost):
				signal.Fix = "Point LiveKit public WSS URL at a public address, or leave it blank to use this server's own domain."
			case errors.Is(err, livekit.ErrSignalTLS):
				signal.Fix = "Install a trusted certificate for " + target.Hostname() + " on the proxy."
			default:
				signal.Fix = "Check the DNS record of " + target.Hostname() + " and that the proxy accepts HTTPS from this server."
			}
		} else {
			signal.State, signal.Detail = "passed", "LiveKit answered at "+target.String()+"."
		}
	}
	return []MeetCheck{settings, reach, signal, media}
}

func settingsFix(err error) string {
	text := err.Error()
	switch {
	case strings.Contains(text, "LIVEKIT_API_KEY"):
		return "Enter the LiveKit API key and a secret of at least 32 characters on this step; scripts/init-compose.py generates both."
	case strings.Contains(text, "LIVEKIT_SERVER_URL is required"):
		return "Set LiveKit server URL to the API origin this server reaches, such as http://127.0.0.1:7880 when LiveKit runs on this host."
	case strings.Contains(text, "LIVEKIT_SERVER_URL"):
		return "Use scheme, host and port only, such as http://127.0.0.1:7880."
	case strings.Contains(text, "LIVEKIT_PUBLIC_URL is required"):
		return "Save the public HTTPS address in Server and workspace; Meet then uses wss://<domain>/rtc."
	case strings.Contains(text, "LIVEKIT_PUBLIC_URL"):
		return "Use wss://<domain>/rtc, or leave LiveKit public WSS URL blank to use that default."
	}
	return "Check the Meetings settings on this step."
}

func meetFailed(checks []MeetCheck) error {
	for _, c := range checks {
		if c.State == "failed" || c.State == "skipped" {
			return meetFailure{checks: checks}
		}
	}
	return nil
}

func createDeleteRoom(id string) func(context.Context, livekit.Config) error {
	return func(ctx context.Context, c livekit.Config) error {
		room := "thura-setup-" + id
		// Always attempt deletion after an uncertain create; never leave a room
		// that another user can accidentally join, and never retry a create.
		err := c.Call(ctx, "CreateRoom", room, map[string]any{"name": room, "empty_timeout": 1, "max_participants": 1}, nil)
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		removed := c.Call(cleanup, "DeleteRoom", room, map[string]string{"room": room}, nil)
		return errors.Join(err, removed)
	}
}

func listRooms(ctx context.Context, c livekit.Config) error {
	return c.Call(ctx, "ListRooms", "", map[string]any{}, nil)
}

// The page's status repeats at most every 15 seconds for the same settings.
var meetStatusCache struct {
	sync.Mutex
	key    string
	at     time.Time
	checks []MeetCheck
}

// meetStatusJSON reports the named Meetings checks for the saved draft. It
// only reads: the API step lists rooms, the signalling step makes one GET.
func meetStatusJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	row, err := q.New(db.From(r.Context())).GetServerSetup(r.Context())
	var v map[string]string
	if err == nil {
		v, err = unseal(row.Draft)
	}
	if err != nil {
		http.Error(w, `{"error":"saved setup could not be read"}`, http.StatusServiceUnavailable)
		return
	}
	key := strings.Join([]string{v["LIVEKIT_SERVER_URL"], v["LIVEKIT_PUBLIC_URL"], v["LIVEKIT_API_KEY"], v["LIVEKIT_API_SECRET"], v["APP_URL"]}, "\x00")
	meetStatusCache.Lock()
	defer meetStatusCache.Unlock()
	if meetStatusCache.key != key || time.Since(meetStatusCache.at) > 15*time.Second {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		meetStatusCache.checks = meetChecks(ctx, v, listRooms)
		cancel()
		meetStatusCache.key, meetStatusCache.at = key, time.Now()
	}
	if err := json.NewEncoder(w).Encode(map[string]any{"checks": meetStatusCache.checks}); err != nil {
		slog.WarnContext(r.Context(), "serve meetings status", "error", err)
	}
}

// MeetView is the Meetings part of the integrations step: the pending checks
// and the proxy rules for this server's domain. FromPage is set while no
// public address is saved and the rules use the address of the setup page.
type MeetView struct {
	Domain, PublicURL, Prefix, Nginx, Caddy string
	FromPage                                bool
	Checks                                  []MeetCheck
}

var pageHost = regexp.MustCompile(`^[A-Za-z0-9.-]{1,253}$`)

func meetView(v map[string]string, requestHost string) *MeetView {
	view := &MeetView{Checks: []MeetCheck{
		{ID: "settings", Name: "Settings present and valid", State: "pending"},
		{ID: "api", Name: "LiveKit API reachable and authenticated", State: "pending"},
		{ID: "signal", Name: "Public signalling reachable through the proxy", State: "pending"},
		{ID: "media", Name: "Media ports open", State: "manual", Detail: "Cannot be verified from inside the server.", Fix: "Open TCP 7881 and UDP 7882 to the internet. Behind NAT, set THURA_RTC_NODE_IP to the public address."},
	}}
	public := strings.TrimRight(v["LIVEKIT_PUBLIC_URL"], "/")
	if public == "" {
		public = livekit.DefaultPublicURL(v["APP_URL"])
	}
	if public == "" {
		// Before the public address is saved, the administrator is most
		// likely on it: offer the rules for this page's host.
		host := requestHost
		if h, _, err := net.SplitHostPort(requestHost); err == nil {
			host = h
		}
		if !pageHost.MatchString(host) {
			return view
		}
		view.FromPage = true
		public = "wss://" + host + "/rtc"
	}
	u, err := url.Parse(public)
	if err != nil || u.Hostname() == "" {
		return view
	}
	view.Domain, view.PublicURL = u.Hostname(), public
	view.Prefix = strings.TrimRight(u.Path, "/")
	view.Nginx, view.Caddy = proxySnippets(view.Domain, view.Prefix)
	return view
}

// proxySnippets forward <prefix>/ to LiveKit's HTTP port with the prefix
// stripped and WebSocket upgrades passed through, as deploy/lidza.stack.json
// routes it.
func proxySnippets(domain, prefix string) (nginx, caddy string) {
	if prefix == "" {
		nginx = "# Inside the server block for " + domain + " (LiveKit on its own host)\nlocation / {\n    proxy_pass http://127.0.0.1:7880;\n    proxy_http_version 1.1;\n    proxy_set_header Upgrade $http_upgrade;\n    proxy_set_header Connection \"upgrade\";\n    proxy_set_header Host $host;\n    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n    proxy_set_header X-Forwarded-Proto $scheme;\n    proxy_read_timeout 1h;\n    proxy_send_timeout 1h;\n}\n"
		caddy = domain + " {\n\treverse_proxy 127.0.0.1:7880\n}\n"
		return nginx, caddy
	}
	nginx = "# Inside the server block for " + domain + ", before the location that serves Thura\nlocation " + prefix + "/ {\n    proxy_pass http://127.0.0.1:7880/;\n    proxy_http_version 1.1;\n    proxy_set_header Upgrade $http_upgrade;\n    proxy_set_header Connection \"upgrade\";\n    proxy_set_header Host $host;\n    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n    proxy_set_header X-Forwarded-Proto $scheme;\n    proxy_read_timeout 1h;\n    proxy_send_timeout 1h;\n}\n"
	caddy = domain + " {\n\t# Before the rule that serves Thura\n\thandle_path " + prefix + "/* {\n\t\treverse_proxy 127.0.0.1:7880\n\t}\n}\n"
	return nginx, caddy
}
