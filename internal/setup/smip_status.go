package setup

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/db"
	q "thura/db/queries/gen"
	"thura/internal/smipmail"
)

// smipStatusReply is what the mail step's controls render after the page:
// the observed DNS/transport status of the draft's SMIP mail identity, or
// Error when the check could not run. It never carries a seed or a key.
type smipStatusReply struct {
	Available     bool     `json:"available"`
	Domain        string   `json:"domain,omitempty"`
	TXT           string   `json:"txt,omitempty"`
	Addresses     []string `json:"addresses,omitempty"`
	MatchesServer string   `json:"matchesServer,omitempty"`
	TLS           string   `json:"tls,omitempty"`
	Detail        string   `json:"detail,omitempty"`
	CheckedAt     string   `json:"checkedAt,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// smipStatusJSON serves the status behind the setup page's administrator
// gate. It is cached for a minute per domain and bounded to
// smipmail.StatusTimeout, like the team domains' status route.
func smipStatusJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	reply := smipStatusReply{}
	status := http.StatusOK
	row, err := q.New(db.From(r.Context())).GetServerSetup(r.Context())
	var v map[string]string
	if err == nil {
		v, err = unseal(row.Draft)
	}
	if err != nil {
		status, reply.Error = http.StatusServiceUnavailable, "The status check could not run: the setup draft is unavailable."
	} else if identity := mailIdentityView(v); identity.Saved && v["SMIP_MAIL_ENABLED"] == "true" {
		s := smipmail.From(r.Context()).DomainStatus(r.Context(), identity.Domain, v["APP_URL"], lidza.Now(r.Context()))
		reply = smipStatusReply{Available: true, Domain: s.Domain, TXT: s.TXT, Addresses: s.Addresses, MatchesServer: s.MatchesServer, TLS: s.TLS, Detail: s.Detail, CheckedAt: s.CheckedAt.Format("Jan 2, 2006 3:04 PM MST")}
	}
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(reply); err != nil {
		slog.WarnContext(r.Context(), "serve SMIP status", "error", err)
	}
}
