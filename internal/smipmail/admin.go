package smipmail

import (
	"net/http"

	"github.com/agim/lidza/packs/admin"
	"github.com/agim/lidza/packs/db"
	q "thura/db/queries/gen"
)

// Page uses the existing server-administrator gate. No keys, message bodies,
// provider credentials or private signing configuration enter the view.
func Page() admin.Page {
	return admin.Page{Name: "SMIP email domains", Path: "smip-mail", Icon: "mail", Template: "smip-mail.html", Data: func(r *http.Request) (any, error) {
		g := From(r.Context())
		rows, err := q.New(db.From(r.Context())).ListSmipMailDomains(r.Context(), g.domain)
		return struct {
			Enabled bool
			Domain  string
			Items   []q.SmipMailDomain
		}{g.Enabled(), g.domain, rows}, err
	}}
}
