package setup

import (
	"fmt"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/admin"
	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"net/http"
	q "thura/db/queries/gen"
)

type View struct {
	Revision, PublishedRevision, ActiveRevision int32
	Step                                        Step
	Steps                                       []Step
	Validation, Message                         string
	Secrets                                     []string
	Probes                                      []ProbeView
	MailCheckID, StorageCheckID                 string
}

func Page() admin.Page {
	return admin.Page{Name: "Server setup", Path: "setup", Icon: "settings", Template: "setup.html", Data: func(r *http.Request) (any, error) {
		row, e := q.New(db.From(r.Context())).GetServerSetup(r.Context())
		if e != nil {
			return nil, e
		}
		v, e := unseal(row.Draft)
		if e != nil {
			return nil, e
		}
		id := r.URL.Query().Get("step")
		if id == "" {
			id = row.Step
		}
		step, ok := stepByID(id)
		if !ok {
			step, _ = stepByID("server")
		}
		state, _ := lidza.Optional[runtimeState](r.Context())
		view := View{MailCheckID: uuid.NewString(), StorageCheckID: uuid.NewString(), ActiveRevision: state.Revision, Revision: row.Revision, PublishedRevision: row.PublishedRevision, Step: step, Steps: Catalog()}
		for i, f := range step.Fields {
			view.Step.Fields[i].Saved = v[f.Name] != ""
			if f.Kind != "secret" {
				view.Step.Fields[i].Value = v[f.Name]
			}
		}
		if step.ID == "review" {
			view.Probes, e = probeViews(r.Context(), v)
			if e != nil {
				return nil, e
			}
			if e := validate(v); e != nil {
				view.Validation = e.Error()
			} else if e := requireProviderChecks(r.Context(), q.New(db.From(r.Context())), v); e != nil {
				view.Validation = e.Error()
			}
			for _, s := range Catalog() {
				for _, f := range s.Fields {
					if f.Kind == "secret" && v[f.Name] != "" {
						view.Secrets = append(view.Secrets, f.Label)
					}
				}
			}
			view.Message = fmt.Sprintf("Draft revision %d; published revision %d", row.Revision, row.PublishedRevision)
		}
		return view, nil
	}, Actions: map[string]admin.Action{
		"save": func(r *http.Request) (string, error) {
			e := Save(r.Context(), r.Form, false)
			return "Draft saved. You can leave and resume later.", e
		},
		"restart": func(r *http.Request) (string, error) {
			if r.Form.Get("confirm") != "restart" {
				return "", fmt.Errorf("type restart to reset the draft")
			}
			e := Save(r.Context(), r.Form, true)
			return "Draft restarted; published settings were preserved.", e
		},
		"check-mail": func(r *http.Request) (string, error) {
			if r.Form.Get("confirm") != "send" {
				return "", fmt.Errorf("confirm sending a test email to your administrator inbox")
			}
			return Probe(r.Context(), "mail", r.Form.Get("revision"), r.Form.Get("check_id"))
		},
		"check-storage": func(r *http.Request) (string, error) {
			if r.Form.Get("confirm") != "check" {
				return "", fmt.Errorf("confirm creating and deleting a small storage test object")
			}
			return Probe(r.Context(), "storage", r.Form.Get("revision"), r.Form.Get("check_id"))
		},
		"publish": func(r *http.Request) (string, error) {
			if r.Form.Get("confirm") != "publish" {
				return "", fmt.Errorf("type publish to confirm this configuration")
			}
			e := Publish(r.Context(), r.Form.Get("revision"))
			return "Configuration published. Restart all server nodes to activate this revision. Verify providers, DNS/TLS and backups before inviting users.", e
		},
	}}
}
