// Package rspamd provides bounded scanning and explicit team-scoped feedback.
package rspamd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"thura/internal/smtpenvelope"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/pkg/env"
	"github.com/google/uuid"
)

type Config struct {
	URL        string `env:"MAIL_RSPAMD_URL"`
	Controller string `env:"MAIL_RSPAMD_CONTROLLER_URL"`
	Password   string `env:"MAIL_RSPAMD_PASSWORD"`
}

// ErrNotLearnable means Rspamd deliberately skipped a sample (e.g. too few tokens).
// Save the human correction without claiming statistical learning or retrying forever.
var ErrNotLearnable = errors.New("local spam filter skipped this statistical learning sample")

func (c Config) Validate() error {
	for _, raw := range []string{c.URL, c.Controller} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return errors.New("rspamd requires an HTTP or HTTPS service origin")
		}
	}
	if strings.ContainsAny(c.Password, "\r\n") {
		return errors.New("invalid Rspamd controller password")
	}
	return nil
}

func Configuration() (Config, error) {
	var c Config
	if err := env.Load(".", &c); err != nil {
		return c, err
	}
	return c, c.Validate()
}

type Result struct {
	Score   float64                    `json:"score"`
	Action  string                     `json:"action"`
	Skipped bool                       `json:"is_skipped"`
	Error   string                     `json:"error"`
	Symbols map[string]json.RawMessage `json:"symbols"`
}

func request(ctx context.Context, c Config, origin, path, workspace string, raw []byte, learn bool, envelope *smtpenvelope.Envelope) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(workspace); err != nil {
		return nil, errors.New("invalid team scope")
	}
	if origin == "" || len(raw) == 0 || len(raw) > 20<<20 {
		return nil, errors.New("rspamd is unavailable or the message exceeds its limit")
	}
	call, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(call, http.MethodPost, strings.TrimRight(origin, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "message/rfc822")
	// HTTP metadata, never the spoofable MIME headers or an SMTP-auth User header.
	req.Header.Set("X-Thura-Workspace", workspace)
	req.Header.Set("Flags", "no_log")
	if envelope != nil {
		req.Header.Set("IP", envelope.IP)
		req.Header.Set("From", envelope.From)
		req.Header.Set("Helo", envelope.Helo)
	}
	if learn {
		if c.Password == "" {
			return nil, errors.New("rspamd learning password is not configured")
		}
		req.Header.Set("Password", c.Password)
	}
	client := *lidza.HTTPClient(ctx)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("rspamd request could not complete")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, (512<<10)+1))
	if err != nil || len(data) > 512<<10 {
		return nil, errors.New("invalid Rspamd response")
	}
	if response.StatusCode != http.StatusOK {
		if learn && response.StatusCode == http.StatusNoContent {
			return nil, ErrNotLearnable
		}
		if learn && response.StatusCode == http.StatusAlreadyReported {
			var duplicate struct {
				Error string `json:"error"`
			}
			class := "ham"
			if path == "/learnspam" {
				class = "spam"
			}
			if json.Unmarshal(data, &duplicate) == nil && strings.HasSuffix(duplicate.Error, " has been already learned as "+class+", ignore it") {
				return []byte(`{"success":true}`), nil
			}
		}
		return nil, errors.New("rspamd rejected the request")
	}
	return data, nil
}

func Scan(ctx context.Context, c Config, workspace string, raw []byte) (Result, error) {
	return ScanSMTP(ctx, c, workspace, raw, "")
}

func ScanSMTP(ctx context.Context, c Config, workspace string, raw []byte, encoded string) (Result, error) {
	var envelope *smtpenvelope.Envelope
	if encoded != "" {
		e, err := smtpenvelope.Decode(encoded)
		if err != nil {
			return Result{}, err
		}
		envelope = &e
	}
	var out Result
	data, err := request(ctx, c, c.URL, "/checkv2", workspace, raw, false, envelope)
	if err != nil {
		return out, err
	}
	var required struct {
		Score *float64 `json:"score"`
	}
	if json.Unmarshal(data, &out) != nil || json.Unmarshal(data, &required) != nil || required.Score == nil || math.IsNaN(out.Score) || math.IsInf(out.Score, 0) || out.Skipped || out.Error != "" || out.Action == "" {
		return out, errors.New("rspamd did not complete its scan")
	}
	return out, nil
}

func Learn(ctx context.Context, c Config, workspace string, raw []byte, classification string) error {
	path := "/learnham"
	if classification == "spam" {
		path = "/learnspam"
	} else if classification != "ham" {
		return errors.New("invalid feedback class")
	}
	data, err := request(ctx, c, c.Controller, path, workspace, raw, true, nil)
	if err != nil {
		return err
	}
	var out struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(data, &out) != nil || !out.Success || out.Error != "" {
		return errors.New("rspamd did not acknowledge feedback")
	}
	return nil
}
