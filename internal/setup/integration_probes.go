package setup

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/llm"
	"github.com/google/uuid"
	"io"
	"net/http"
	"strconv"
	"strings"
	"thura/internal/platform/ingress"
	"thura/internal/providers/collabora"
	"thura/internal/providers/livekit"
	"thura/internal/providers/matrix"
	"time"
)

func integrationKinds() []string { return []string{"matrix", "collabora", "livekit", "ai", "smip"} }

func checkNames(kind string) []string {
	switch kind {
	case "matrix":
		return []string{"MATRIX_SERVER_URL", "MATRIX_SERVER_NAME", "MATRIX_AS_TOKEN"}
	case "collabora":
		return []string{"COLLABORA_SERVER_URL", "COLLABORA_APP_URL", "COLLABORA_TOKEN_SECRET"}
	case "livekit":
		return []string{"LIVEKIT_SERVER_URL", "LIVEKIT_PUBLIC_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"}
	case "ai":
		return []string{"THURA_AI_ENABLED", "LLM_PROVIDER", "LLM_MODEL", "LLM_BASE_URL", "LLM_API_KEY", "LLM_MAX_TOKENS"}
	case "smip":
		return []string{"APP_URL", "SMIP_ENABLED", "SMIP_CONFIG", "SMIP_MAIL_ENABLED", "SMIP_MAIL_CONFIG"}
	}
	return nil
}

// Each check exercises the saved draft, never temporarily swaps process-wide
// credentials. No user content is sent to an AI provider by a connection test.
func checkIntegration(ctx context.Context, kind string, v map[string]string, id string) error {
	switch kind {
	case "matrix":
		c := matrix.Config{URL: strings.TrimRight(v["MATRIX_SERVER_URL"], "/"), ServerName: v["MATRIX_SERVER_NAME"], Token: v["MATRIX_AS_TOKEN"]}
		if err := c.EnsureUser(ctx, c.Bot()); err != nil {
			return err
		}
		var who struct {
			UserID string `json:"user_id"`
		}
		if err := c.Call(ctx, c.Bot(), "GET", "/_matrix/client/v3/account/whoami", nil, &who); err != nil {
			return err
		}
		if who.UserID != c.Bot() {
			return errors.New("matrix returned another application-service identity")
		}
		return nil
	case "collabora":
		c := collabora.Config{ServerURL: v["COLLABORA_SERVER_URL"], AppURL: v["COLLABORA_APP_URL"], Secret: v["COLLABORA_TOKEN_SECRET"]}
		for _, ext := range []string{"docx", "xlsx"} {
			_, readOnly, err := c.Editor(ctx, ext, "", false)
			if err != nil {
				return err
			}
			if readOnly {
				return errors.New("office editing is unavailable")
			}
		}
		return nil
	case "livekit":
		c := livekit.Config{URL: v["LIVEKIT_SERVER_URL"], PublicURL: v["LIVEKIT_PUBLIC_URL"], Key: v["LIVEKIT_API_KEY"], Secret: v["LIVEKIT_API_SECRET"]}
		room := "thura-setup-" + id
		// Always attempt deletion after an uncertain create; never leave a room that
		// another user can accidentally join, and never retry a submitted create.
		err := c.Call(ctx, "CreateRoom", room, map[string]any{"name": room, "empty_timeout": 1, "max_participants": 1}, nil)
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		removed := c.Call(cleanup, "DeleteRoom", room, map[string]string{"room": room}, nil)
		return errors.Join(err, removed)
	case "ai":
		if err := ensureLocalModel(ctx, v); err != nil {
			return err
		}
		if err := validateModel(v); err != nil {
			return err
		}
		tokens, err := strconv.Atoi(v["LLM_MAX_TOKENS"])
		if err != nil {
			return err
		}
		service, err := llm.New(llm.Config{Provider: v["LLM_PROVIDER"], Model: v["LLM_MODEL"], BaseURL: v["LLM_BASE_URL"], APIKey: v["LLM_API_KEY"], EmbedProvider: "none", MaxTokens: tokens, Timeout: 45 * time.Second, MaxAttempts: 1})
		if err != nil {
			return err
		}
		result, err := service.Chat(ctx, llm.Request{System: "Reply with OK only.", Messages: []llm.Message{{Role: llm.User, Content: "Thura connection test. Reply OK."}}, MaxTokens: 128, Label: "thura.setup-connection-test"})
		if err != nil {
			return err
		}
		if strings.TrimSpace(result.Text) == "" {
			return errors.New("model returned no response")
		}
		return nil
	case "smip":
		// Local identity validation is supplemented by a real request through the
		// public route. A forwarded scheme header cannot satisfy direct TLS 1.3.
		if err := validate(v); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(v["APP_URL"], "/")+"/api/v1/setup/transport?nonce="+id, nil)
		if err != nil {
			return err
		}
		client := *lidza.HTTPClient(ctx)
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 || res.TLS == nil || res.TLS.Version < tls.VersionTLS13 {
			return errors.New("public SMIP route does not prove TLS 1.3")
		}
		var reply struct {
			Nonce     string `json:"nonce"`
			Transport string `json:"transport"`
		}
		if err = json.NewDecoder(io.LimitReader(res.Body, 1024)).Decode(&reply); err != nil {
			return err
		}
		if reply.Nonce != id || reply.Transport != "tls1.3" {
			return errors.New("SMIP transport challenge failed")
		}
		return nil
	}
	return errors.New("unknown integration check")
}

// TransportCheck exposes no settings or credentials. A fresh challenge prevents
// a cached success from disguising a broken deployment transport.
func TransportCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !ingress.TLS13(r) {
		http.Error(w, "Direct TLS 1.3 is required for SMIP", 400)
		return
	}
	nonce, err := uuid.Parse(r.URL.Query().Get("nonce"))
	if err != nil || nonce == uuid.Nil {
		http.Error(w, "Invalid challenge", 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"nonce": nonce.String(), "transport": "tls1.3"}); err != nil {
		return
	}
}
