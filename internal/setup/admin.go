package setup

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/admin"
	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"log/slog"
	"net/http"
	q "thura/db/queries/gen"
	"time"
)

type View struct {
	MailIdentity                                MailIdentityView
	Revision, PublishedRevision, ActiveRevision int32
	Step                                        Step
	Steps                                       []Step
	Validation, Message                         string
	Secrets                                     []string
	Probes                                      []ProbeView
	MailCheckID, StorageCheckID                 string
	IntegrationChecks                           []IntegrationCheckView
	ReviewChecks                                []ReviewCheckView
	ConfigurationError                          string
}

//go:embed admin-controls.js
var adminControls string

func Page() admin.Page {
	return admin.Page{Name: "Server setup", Path: "setup", Icon: "settings", Template: "setup.html", Downloads: map[string]http.HandlerFunc{"controls.js": func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if _, err := io.WriteString(w, adminControls); err != nil {
			slog.WarnContext(r.Context(), "serve setup controls", "error", err)
		}
	}}, Data: func(r *http.Request) (any, error) {
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
		view := View{MailIdentity: mailIdentityView(v), MailCheckID: uuid.NewString(), StorageCheckID: uuid.NewString(), ActiveRevision: state.Revision, Revision: row.Revision, PublishedRevision: row.PublishedRevision, Step: step, Steps: Catalog()}
		for _, kind := range integrationKinds() {
			view.IntegrationChecks = append(view.IntegrationChecks, IntegrationCheckView{Kind: kind, ID: uuid.NewString()})
		}
		for i, f := range step.Fields {
			view.Step.Fields[i].Saved = v[f.Name] != ""
			if f.Kind == "choice" && v[f.Name] != "" {
				found := false
				for _, option := range f.Options {
					found = found || option == v[f.Name]
				}
				if !found {
					view.Step.Fields[i].Options = append(append([]string{}, f.Options...), v[f.Name])
				}
			}
			view.Step.Fields[i].Managed = managedField(f.Name)
			if managedField(f.Name) {
				view.Step.Fields[i].ManagedTitle = managedTitle(f.Name)
			}
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
				view.ConfigurationError = e.Error()
			} else if e := validateCoreFeatures(v); e != nil {
				view.Validation = e.Error()
				view.ConfigurationError = e.Error()
			} else if e := requireProviderChecks(r.Context(), q.New(db.From(r.Context())), v); e != nil {
				view.Validation = e.Error()
			}
			view.ReviewChecks, e = reviewChecks(r.Context(), v)
			if e != nil {
				return nil, e
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
		"auto-check": func(r *http.Request) (string, error) {
			kind := r.Form.Get("kind")
			if kind != "mail" && kind != "storage" && len(checkNames(kind)) == 0 {
				return "", fmt.Errorf("unknown integration")
			}
			return runProbe(r.Context(), kind, r.Form.Get("revision"), r.Form.Get("check_id"), true)
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
		"check-integration": func(r *http.Request) (string, error) {
			if r.Form.Get("confirm") != "check" {
				return "", fmt.Errorf("confirm the live integration check; AI uses one small provider request")
			}
			kind := r.Form.Get("kind")
			if len(checkNames(kind)) == 0 {
				return "", fmt.Errorf("unknown integration")
			}
			return Probe(r.Context(), kind, r.Form.Get("revision"), r.Form.Get("check_id"))
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

type IntegrationCheckView struct{ Kind, ID string }

// Review uses the latest matching attempt, including uncertain failures. Opening
// another tab or reloading never implicitly retries a provider operation.
type ReviewCheckView struct {
	Kind, Title, ID, State, Message, Action, Confirm, AttemptID string
	Automatic                                                   bool
}

func reviewChecks(ctx context.Context, v map[string]string) ([]ReviewCheckView, error) {
	titles := map[string]string{"mail": "Email", "storage": "Storage", "matrix": "Chat", "collabora": "Documents", "livekit": "Meetings", "ai": "AI", "smip": "Secure delivery"}
	var out []ReviewCheckView
	for _, kind := range append([]string{"mail", "storage"}, integrationKinds()...) {
		c := ReviewCheckView{Kind: kind, Title: titles[kind], ID: uuid.NewString(), State: "pending", Automatic: true, Action: "check-integration", Confirm: "check"}
		if kind == "mail" {
			c.Action = "check-mail"
			c.Confirm = "send"
		}
		if kind == "storage" {
			c.Action = "check-storage"
		}
		fingerprint, err := probeFingerprint(v, kind)
		if err != nil {
			return nil, err
		}
		prior, err := q.New(db.From(ctx)).LatestMatchingSetupProbe(ctx, q.LatestMatchingSetupProbeParams{Kind: kind, Fingerprint: fingerprint})
		if err == nil && (prior.State != "succeeded" || prior.CreatedAt.After(lidza.Now(ctx).Add(-24*time.Hour))) {
			c.AttemptID = prior.ID
			c.State = prior.State
			c.Message = probeResult(kind, prior.State, prior.Diagnostic, prior.ID)
			c.Automatic = false
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
