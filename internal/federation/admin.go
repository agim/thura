package federation

import (
	"errors"
	"net/http"
	"strings"

	"github.com/agim/lidza/packs/admin"
	"github.com/agim/lidza/pkg/env"
)

// PeersPage is the server administrator's SMIP peers page: this server's
// public identity to share, the paired peers, and the fetch → compare
// fingerprints → confirm flow. No seed or private key reaches the view.
type PeersPage struct {
	Enabled      bool
	Domain       string
	Origin       string
	IdentityPath string
	Keys         []PeerKey
	Peers        []PeerView
	Pending      []PeerView
	MaxPeers     int
}

func Page() admin.Page {
	return admin.Page{Name: "SMIP peers", Path: "smip-peers", Icon: "shield-check", Template: "smip-peers.html", Data: func(r *http.Request) (any, error) {
		g := From(r.Context())
		data := PeersPage{Enabled: g.Enabled(), Domain: g.domain, IdentityPath: IdentityPath, MaxPeers: MaxPeers}
		if !g.Enabled() {
			return data, nil
		}
		var cfg struct {
			URL string `env:"APP_URL"`
		}
		if err := env.Load(".", &cfg); err != nil {
			return nil, err
		}
		data.Origin = strings.TrimRight(cfg.URL, "/")
		if data.Origin == "" {
			data.Origin = "https://" + g.domain
		}
		data.Keys = g.OwnKeys()
		peers, err := g.Peers(r.Context())
		if err != nil {
			return nil, err
		}
		for _, peer := range peers {
			if peer.State == PeerPending {
				data.Pending = append(data.Pending, peer)
			} else {
				data.Peers = append(data.Peers, peer)
			}
		}
		return data, nil
	}, Actions: map[string]admin.Action{
		"pair": func(r *http.Request) (string, error) {
			view, err := From(r.Context()).Propose(r.Context(), r.Form.Get("domain"), r.Form.Get("origin"))
			if err != nil {
				return "", err
			}
			return "Fetched the identity of " + view.Domain + ". Read its fingerprint to the peer's operator, compare it with what they read out, then confirm below.", nil
		},
		"confirm": func(r *http.Request) (string, error) {
			domain := r.Form.Get("domain")
			if err := From(r.Context()).Confirm(r.Context(), domain, r.Form["key"], r.Form.Get("verified") == "on"); err != nil {
				return "", err
			}
			return "Paired " + strings.ToLower(strings.TrimSpace(domain)) + ". Packets signed with its confirmed keys are accepted from now on; the other operator must pair this server too.", nil
		},
		"discard": func(r *http.Request) (string, error) {
			if err := From(r.Context()).Discard(r.Context(), r.Form.Get("domain")); err != nil {
				return "", err
			}
			return "Proposal discarded; nothing was trusted.", nil
		},
		"revoke": func(r *http.Request) (string, error) {
			if r.Form.Get("confirm") != "revoke" {
				return "", errors.New("type revoke to confirm")
			}
			if err := From(r.Context()).Revoke(r.Context(), r.Form.Get("domain")); err != nil {
				return "", err
			}
			return "Revoked. Its keys are refused on every node from the next packet; queued sends to it block before any network attempt.", nil
		},
	}}
}
