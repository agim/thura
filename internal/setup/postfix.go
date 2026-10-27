package setup

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/mail"
	"os"
	"regexp"
	"strings"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/env"
	q "thura/db/queries/gen"
	"thura/internal/tenancy"
)

var mailHostname = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
var mailLocalpart = regexp.MustCompile(`^[a-z0-9][a-z0-9._+\-]{0,63}$`)

func inboxAddress(v map[string]string) string {
	if v["THURA_MAIL_ADDRESS"] != "" {
		return v["THURA_MAIL_ADDRESS"]
	}
	return v["MAIL_FROM"]
}
func managedPostfix(v map[string]string) bool {
	return v["THURA_MAIL_INBOUND"] == "postfix" || v["THURA_MAIL_OUTBOUND"] == "postfix"
}
func validateMailRouting(v map[string]string) error {
	if address := v["THURA_MAIL_ADDRESS"]; address != "" {
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address {
			return errors.New("inbox address must be a plain email address")
		}
	}
	if !managedPostfix(v) {
		return nil
	}
	host := v["THURA_POSTFIX_HOSTNAME"]
	if len(host) > 253 || !mailHostname.MatchString(host) {
		return errors.New("postfix requires a lowercase mail server DNS hostname")
	}
	if len(os.Getenv("THURA_POSTFIX_TOKEN")) < 32 {
		return errors.New("start the included Postfix Compose service with THURA_POSTFIX_TOKEN before choosing managed Postfix")
	}
	if v["THURA_MAIL_OUTBOUND"] == "postfix" && (v["MAIL_PROVIDER"] != "smtp" || v["MAIL_SMTP_HOST"] != "127.0.0.1" || v["MAIL_SMTP_PORT"] != "2525" || v["MAIL_SMTP_SECURITY"] != "none" || v["MAIL_SMTP_USERNAME"] != "" || v["MAIL_SMTP_PASSWORD"] != "") {
		return errors.New("save the email step to configure local Postfix sending")
	}
	if v["THURA_MAIL_INBOUND"] == "postfix" && !postfixAddress(inboxAddress(v)) {
		return errors.New("postfix inbox must be a lowercase email address on a DNS domain")
	}
	return nil
}
func postfixAddress(address string) bool {
	parsed, err := mail.ParseAddress(address)
	local, domain, ok := strings.Cut(address, "@")
	return err == nil && parsed.Address == address && ok && len(domain) <= 253 && mailHostname.MatchString(domain) && mailLocalpart.MatchString(local)
}

// This private sidecar protocol deliberately excludes outgoing API credentials.
// Both processes share a network namespace: HTTP and submission remain loopback.
type postfixMailbox struct {
	ID     string `json:"id"`
	Secret string `json:"secret"`
}
type postfixRelay struct {
	ReportDomain string                    `json:"reportDomain,omitempty"`
	BaseURL      string                    `json:"baseUrl"`
	Mailboxes    map[string]postfixMailbox `json:"mailboxes"`
}
type postfixConfiguration struct {
	Enabled  bool         `json:"enabled"`
	Hostname string       `json:"hostname"`
	Listen   string       `json:"listen"`
	Revision int32        `json:"revision"`
	Config   postfixRelay `json:"config"`
}

func PostfixConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !PrivatePostfixRequest(r) {
		http.Error(w, "Postfix authentication required", http.StatusForbidden)
		return
	}
	queries := q.New(db.From(r.Context()))
	row, err := queries.GetServerSetup(r.Context())
	if err != nil {
		http.Error(w, "Mail configuration unavailable", http.StatusServiceUnavailable)
		return
	}
	state, _ := lidza.Optional[runtimeState](r.Context())
	result := postfixConfiguration{Enabled: true, Hostname: "mail.invalid", Listen: "127.0.0.1:2525", Config: postfixRelay{BaseURL: "http://127.0.0.1:3000", Mailboxes: map[string]postfixMailbox{}}}
	// Before the first publication only the private outbound listener exists.
	// Draft edits never change an installation's active receiving configuration.
	if state.Revision == 0 {
		if row.PublishedRevision > 0 {
			http.Error(w, "Restart Thura to activate mail configuration", http.StatusServiceUnavailable)
			return
		}
		v, err := unseal(row.Draft)
		if err != nil {
			http.Error(w, "Mail configuration unavailable", http.StatusServiceUnavailable)
			return
		}
		if h := v["THURA_POSTFIX_HOSTNAME"]; len(h) <= 253 && mailHostname.MatchString(h) {
			result.Hostname = h
		}
	} else {
		v := state.MailSettings
		result.Enabled = managedPostfix(v)
		result.Revision = state.Revision
		if result.Enabled {
			result.Hostname = v["THURA_POSTFIX_HOSTNAME"]
		}
		if v["THURA_MAIL_INBOUND"] == "postfix" || len(os.Getenv("THURA_POSTFIX_TOKEN")) >= 32 {
			// Use the node's activated settings, including mailbox-specific prefixes.
			values, err := env.Values(".")
			if err != nil {
				http.Error(w, "Mail configuration unavailable", http.StatusServiceUnavailable)
				return
			}
			mailboxes, err := queries.ListPostfixMailboxes(r.Context())
			if err != nil || len(mailboxes) > 1000 {
				http.Error(w, "Mail configuration unavailable", http.StatusServiceUnavailable)
				return
			}
			for _, mailbox := range mailboxes {
				secret := values[mailbox.ConfigPrefix+"MAIL_INBOUND_SECRET"]
				if mailbox.ConfigPrefix == "" {
					secret = v["MAIL_INBOUND_SECRET"]
				}
				teamSecret, managed, teamErr := tenancy.PostfixSecret(r.Context(), mailbox)
				if managed {
					if teamErr != nil {
						continue
					}
					secret = teamSecret
				} else if v["THURA_MAIL_INBOUND"] != "postfix" {
					continue
				}
				if !postfixAddress(mailbox.Address) || len(secret) < 32 || len(secret) > 4096 {
					http.Error(w, "A mailbox requires a canonical address and inbound signing secret", http.StatusServiceUnavailable)
					return
				}
				if _, duplicate := result.Config.Mailboxes[mailbox.Address]; duplicate {
					http.Error(w, "Duplicate receiving mailbox address", http.StatusServiceUnavailable)
					return
				}
				result.Config.Mailboxes[mailbox.Address] = postfixMailbox{ID: mailbox.ID, Secret: secret}
			}
			aliases, err := queries.ListPostfixAliases(r.Context())
			if err != nil || len(aliases) > 10000 {
				http.Error(w, "Mail alias configuration unavailable", http.StatusServiceUnavailable)
				return
			}
			targets := map[string]postfixMailbox{}
			for _, route := range result.Config.Mailboxes {
				targets[route.ID] = route
			}
			for _, alias := range aliases {
				target, managed := targets[alias.MailboxID]
				if !managed {
					continue
				}
				if old, exists := result.Config.Mailboxes[alias.Address]; exists && old.ID != target.ID {
					http.Error(w, "Conflicting receiving alias", http.StatusServiceUnavailable)
					return
				}
				result.Config.Mailboxes[alias.Address] = target
			}
			if len(result.Config.Mailboxes) > 0 {
				_, domain, _ := strings.Cut(inboxAddress(v), "@")
				for address := range result.Config.Mailboxes {
					if strings.HasSuffix(address, "@"+domain) {
						result.Config.ReportDomain = domain
						break
					}
				}
				result.Listen = "25"
				result.Enabled = true
				if !mailHostname.MatchString(v["THURA_POSTFIX_HOSTNAME"]) {
					http.Error(w, "Configure the deployment Postfix hostname before team receiving", http.StatusServiceUnavailable)
					return
				}
				result.Hostname = v["THURA_POSTFIX_HOSTNAME"]
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		return
	}
}

// PrivatePostfixRequest authenticates the mail sidecar without trusting proxy headers.
func PrivatePostfixRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	token := os.Getenv("THURA_POSTFIX_TOKEN")
	expected := sha256.Sum256([]byte("Bearer " + token))
	actual := sha256.Sum256([]byte(r.Header.Get("Authorization")))
	return err == nil && ip != nil && ip.IsLoopback() && len(token) >= 32 && subtle.ConstantTimeCompare(expected[:], actual[:]) == 1
}
