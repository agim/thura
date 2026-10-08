package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"thura/internal/federation"
	"time"
)

func httpsOrigin(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
}
func bounded(v map[string]string, name string, lo, hi int64) error {
	n, e := strconv.ParseInt(v[name], 10, 64)
	if e != nil || n < lo || n > hi {
		return fmt.Errorf("%s must be between %d and %d", name, lo, hi)
	}
	return nil
}
func validate(v map[string]string) error {
	for _, s := range Catalog() {
		for _, f := range s.Fields {
			value := v[f.Name]
			if len(value) > 32768 {
				return errors.New("a setup field exceeds its size limit")
			}
			if len(f.Options) > 0 {
				ok := false
				for _, choice := range f.Options {
					ok = ok || value == choice
				}
				if !ok {
					return fmt.Errorf("choose a valid %s", f.Label)
				}
			}
		}
	}
	if !httpsOrigin(v["APP_URL"]) {
		return errors.New("a public HTTPS origin is required")
	}
	if strings.TrimSpace(v["THURA_WORKSPACE_NAME"]) == "" || len(v["THURA_WORKSPACE_NAME"]) > 120 {
		return errors.New("workspace name must contain 1–120 bytes")
	}
	for _, key := range []string{"THURA_OPERATOR_EMAIL", "MAIL_FROM"} {
		a, e := mail.ParseAddress(v[key])
		if e != nil || a.Address != v[key] {
			return fmt.Errorf("%s must be a plain email address", key)
		}
	}
	if _, e := time.LoadLocation(v["THURA_TIMEZONE"]); e != nil {
		return errors.New("choose a valid IANA timezone")
	}
	if strings.TrimSpace(v["THURA_BACKUP_PLAN"]) == "" {
		return errors.New("record a backup and retention plan")
	}
	for _, b := range []struct {
		name   string
		lo, hi int64
	}{{"DRIVE_QUOTA_BYTES", 1 << 20, 1 << 40}, {"THURA_MAX_MEMBERS", 1, 1000}, {"MAIL_MAX_RECIPIENTS", 1, 50}, {"MAIL_SMTP_PORT", 1, 65535}} {
		if e := bounded(v, b.name, b.lo, b.hi); e != nil {
			return e
		}
	}
	if len(v["MAIL_INBOUND_SECRET"]) < 32 {
		return errors.New("inbound signing secret must have at least 32 characters")
	}
	if v["MAIL_PROVIDER"] == "smtp" {
		host := v["MAIL_SMTP_HOST"]
		if host == "" || (net.ParseIP(host) == nil && strings.ContainsAny(host, "/: \r\n")) {
			return errors.New("smtp hostname required")
		}
		if v["MAIL_SMTP_SECURITY"] == "none" {
			ip := net.ParseIP(host)
			if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
				return errors.New("plain SMTP is restricted to loopback")
			}
		}
		if (v["MAIL_SMTP_USERNAME"] == "") != (v["MAIL_SMTP_PASSWORD"] == "") {
			return errors.New("smtp username and password must both be supplied or both empty")
		}
	} else {
		if v["MAIL_API_KEY"] == "" {
			return errors.New("email API key required")
		}
		if v["MAIL_PROVIDER"] == "mailgun" && v["MAIL_DOMAIN"] == "" {
			return errors.New("mailgun sending domain required")
		}
	}
	for _, part := range strings.Split(v["STORAGE_PREFIX"], "/") {
		if part == "." || part == ".." {
			return errors.New("storage prefix cannot contain traversal segments")
		}
	}
	if v["STORAGE_PROVIDER"] == "local" {
		if strings.TrimSpace(v["STORAGE_DIR"]) == "" {
			return errors.New("local storage directory required")
		}
	} else {
		if !httpsOrigin(v["STORAGE_ENDPOINT"]) {
			return errors.New("s3 HTTPS origin required")
		}
		for _, n := range []string{"STORAGE_BUCKET", "STORAGE_REGION", "STORAGE_ACCESS_KEY", "STORAGE_SECRET_KEY"} {
			if v[n] == "" {
				return errors.New("s3 bucket, region and credentials required")
			}
		}
	}
	for _, group := range [][]string{{"MATRIX_SERVER_URL", "MATRIX_SERVER_NAME", "MATRIX_AS_TOKEN"}, {"OFFICE_SERVER_URL", "OFFICE_APP_URL", "OFFICE_JWT_SECRET"}, {"LIVEKIT_SERVER_URL", "LIVEKIT_PUBLIC_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"}} {
		if v[group[0]] == "" {
			continue
		}
		if !httpsOrigin(v[group[0]]) {
			return fmt.Errorf("%s requires an HTTPS origin", group[0])
		}
		for _, n := range group[1:] {
			if v[n] == "" {
				return fmt.Errorf("%s required for the enabled integration", n)
			}
		}
	}
	if v["OFFICE_SERVER_URL"] != "" && !httpsOrigin(v["OFFICE_APP_URL"]) {
		return errors.New("office requires an HTTPS app origin")
	}
	if v["LIVEKIT_SERVER_URL"] != "" {
		u, e := url.Parse(v["LIVEKIT_PUBLIC_URL"])
		if e != nil || u.Scheme != "wss" || u.Hostname() == "" || u.User != nil {
			return errors.New("livekit requires a secure WSS endpoint")
		}
	}
	if v["SMIP_ENABLED"] == "true" {
		var cfg federation.Config
		d := json.NewDecoder(strings.NewReader(v["SMIP_CONFIG"]))
		d.DisallowUnknownFields()
		if d.Decode(&cfg) != nil || d.Decode(&struct{}{}) != io.EOF {
			return errors.New("invalid SMIP pairing JSON")
		}
		if _, e := federation.NewGateway(cfg); e != nil {
			return errors.New("invalid SMIP pairing configuration")
		}
	}
	return nil
}
