package setup

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/credentials"
	"github.com/agim/lidza/pkg/env"
	"github.com/agim/lidza/pkg/router"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
	"thura/schema"
)

// runtimeState records only configuration loaded by this process. Publishing
// a shared database snapshot cannot mark another node as initialized.
type runtimeState struct{ Revision int32 }

const fixtureSubject = "thura-fixture:externally-configured"

type bootstrapConfig struct {
	Token string `env:"THURA_SETUP_TOKEN"`
}

func Status(ctx context.Context) (schema.SetupStatus, error) {
	row, e := q.New(db.From(ctx)).GetServerSetupRouting(ctx)
	if e != nil {
		return schema.SetupStatus{}, e
	}
	var cfg bootstrapConfig
	if e = env.Load(".", &cfg); e != nil {
		return schema.SetupStatus{}, e
	}
	state, _ := lidza.Optional[runtimeState](ctx)
	return schema.SetupStatus{Active: row.PublishedRevision > 0 && state.Revision > 0, RestartRequired: row.PublishedRevision > 0 && row.PublishedRevision != state.Revision, Open: row.Subject == "" && !row.HasAccounts && len(cfg.Token) >= 32, Claimed: row.HasAccounts, Published: row.PublishedRevision > 0, Administrator: Allow(ctx)}, nil
}
func Allow(ctx context.Context) bool {
	user := auth.CurrentUser(ctx)
	if user == nil {
		return false
	}
	profile, err := auth.From(ctx).Profile(ctx, user.ID)
	if err != nil {
		return false
	}
	row, e := q.New(db.From(ctx)).GetServerSetup(ctx)
	if e != nil {
		return false
	}
	if row.Subject != "" && row.Subject == user.ID {
		return true
	}
	// Existing deployments require an explicitly configured operator account.
	var cfg struct {
		Admins string `env:"ADMIN_USERS"`
	}
	if env.Load(".", &cfg) != nil {
		return false
	}
	for _, id := range strings.Split(cfg.Admins, ",") {
		id = strings.TrimSpace(id)
		if id != "" && (id == user.ID || strings.EqualFold(id, profile.Email)) {
			return true
		}
	}
	return false
}
func seal(v map[string]string) (string, error) {
	key, e := credentials.Key(".")
	if e != nil {
		return "", errors.New("server master key is unavailable")
	}
	b, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	return credentials.Encrypt(key, b)
}
func unseal(raw string) (map[string]string, error) {
	if raw == "" {
		return defaults(), nil
	}
	key, e := credentials.Key(".")
	if e != nil {
		return nil, errors.New("server master key is unavailable")
	}
	b, e := credentials.Decrypt(key, raw)
	if e != nil {
		return nil, errors.New("saved setup could not be decrypted")
	}
	var v map[string]string
	if json.Unmarshal(b, &v) != nil {
		return nil, errors.New("saved setup is invalid")
	}
	return v, nil
}
func Claim(ctx context.Context, in schema.SetupClaimInput) (schema.SetupStatus, error) {
	if e := in.Validate(); e != nil {
		return schema.SetupStatus{}, router.Errorf(422, "valid account fields and a strong password required")
	}
	var cfg bootstrapConfig
	if e := env.Load(".", &cfg); e != nil {
		return schema.SetupStatus{}, e
	}
	expected, actual := sha256.Sum256([]byte(cfg.Token)), sha256.Sum256([]byte(in.Token))
	if len(cfg.Token) < 32 || subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
		return schema.SetupStatus{}, router.Errorf(403, "setup token is invalid or setup is disabled")
	}
	if err := auth.From(ctx).ValidatePassword(in.Password, in.Email); err != nil {
		return schema.SetupStatus{}, router.Errorf(422, "password does not satisfy the account policy")
	}
	if strings.TrimSpace(in.Name) == "" {
		return schema.SetupStatus{}, router.Errorf(422, "administrator name required")
	}
	hash, e := auth.HashPassword(in.Password)
	if e != nil {
		return schema.SetupStatus{}, router.Errorf(422, "password does not satisfy the account policy")
	}
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return schema.SetupStatus{}, e
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	row, e := queries.LockServerSetup(ctx)
	if e != nil {
		return schema.SetupStatus{}, e
	}
	count, e := queries.CountSetupAccounts(ctx)
	if e != nil {
		return schema.SetupStatus{}, e
	}
	if row.Subject != "" || count != 0 {
		return schema.SetupStatus{}, router.Errorf(409, "the first administrator has already been created")
	}
	subject := uuid.NewString()
	if _, e = queries.CreateInvitedAccount(ctx, q.CreateInvitedAccountParams{Subject: subject, Email: stringPointer(auth.NormalizeEmail(in.Email)), Name: stringPointer(strings.TrimSpace(in.Name)), PasswordHash: &hash}); e != nil {
		return schema.SetupStatus{}, router.Errorf(409, "account could not be created")
	}
	v := defaults()
	v["THURA_OPERATOR_EMAIL"] = auth.NormalizeEmail(in.Email)
	sealed, e := seal(v)
	if e != nil {
		return schema.SetupStatus{}, e
	}
	if e = queries.ClaimServerSetup(ctx, q.ClaimServerSetupParams{Subject: subject, Draft: sealed}); e != nil {
		return schema.SetupStatus{}, e
	}
	if e = audit.From(ctx).RecordTx(audit.System(ctx, "server-bootstrap"), tx, audit.Event{Action: "server.setup.claim", Resource: "server-setup", Meta: map[string]string{"admin_subject": subject}}); e != nil {
		return schema.SetupStatus{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return schema.SetupStatus{}, e
	}
	return schema.SetupStatus{Claimed: true}, nil
}
func lock(ctx context.Context, revision string) (pgx.Tx, *q.Queries, q.ServerSetup, error) {
	if !Allow(ctx) {
		return nil, nil, q.ServerSetup{}, router.Errorf(403, "server administrator required")
	}
	tx, e := db.From(ctx).Begin(ctx)
	if e != nil {
		return nil, nil, q.ServerSetup{}, e
	}
	queries := q.New(tx)
	row, e := queries.LockServerSetup(ctx)
	if e == nil {
		n, parseErr := strconv.Atoi(revision)
		if parseErr != nil || n != int(row.Revision) {
			e = router.Errorf(409, "setup changed in another tab; reload before saving")
		}
	}
	if e != nil {
		return nil, nil, q.ServerSetup{}, errors.Join(e, tx.Rollback(ctx))
	}
	return tx, queries, row, nil
}
func Save(ctx context.Context, form url.Values, restart bool) error {
	tx, queries, row, e := lock(ctx, form.Get("revision"))
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	v, e := unseal(row.Draft)
	if e != nil {
		return e
	}
	step := form.Get("step")
	s, ok := stepByID(step)
	if !ok {
		return errors.New("unknown setup step")
	}
	action := "server.setup.save"
	if restart {
		v = defaults()
		step = "server"
		action = "server.setup.restart"
	} else {
		for _, f := range s.Fields {
			value := form.Get(f.Name)
			if len(value) > 32768 {
				return errors.New("setup field exceeds its size limit")
			}
			if f.Kind == "secret" {
				if form.Get("clear_"+f.Name) == "true" {
					v[f.Name] = ""
				} else if value != "" {
					v[f.Name] = value
				}
			} else {
				v[f.Name] = strings.TrimSpace(value)
			}
		}
	}
	sealed, e := seal(v)
	if e != nil {
		return e
	}
	if e = queries.SaveServerSetup(ctx, q.SaveServerSetupParams{Draft: sealed, Step: step}); e != nil {
		return e
	}
	if e = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: action, Resource: "server-setup"}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func Publish(ctx context.Context, revision string) error {
	tx, queries, row, e := lock(ctx, revision)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if row.PublishedRevision == row.Revision && row.PublishedRevision > 0 {
		return nil
	}
	v, e := unseal(row.Draft)
	if e != nil {
		return e
	}
	if e = validate(v); e != nil {
		return e
	}
	if e = requireProviderChecks(ctx, queries, v); e != nil {
		return e
	}
	for _, step := range Catalog() {
		for _, f := range step.Fields {
			if value, ok := os.LookupEnv(f.Name); ok && value != v[f.Name] {
				return fmt.Errorf("remove the conflicting %s process environment override before publishing", f.Name)
			}
		}
	}
	for _, name := range []string{"MAIL_SMTP_URL", "MAIL_BASE_URL", "STORAGE_PUBLIC_URL"} {
		if os.Getenv(name) != "" {
			return fmt.Errorf("remove the conflicting %s process environment override before publishing", name)
		}
	}

	// Publication cannot stop old nodes or serialize every future upload.
	// Freeze the location from the first published snapshot, even while empty.
	if row.PublishedRevision > 0 {
		if row.Published == "" {
			return errors.New("published storage configuration is missing; restore it before updating setup")
		}
		previous, err := unseal(row.Published)
		if err != nil {
			return err
		}
		for _, name := range []string{"STORAGE_PROVIDER", "STORAGE_DIR", "STORAGE_ENDPOINT", "STORAGE_BUCKET", "STORAGE_PREFIX", "STORAGE_REGION"} {
			if previous[name] != v[name] {
				return errors.New("published storage location cannot be changed in the wizard; plan an offline migration across all nodes")
			}
		}
	}
	populated, e := queries.SetupHasStoredContent(ctx)
	if e != nil {
		return e
	}
	if populated {
		current, e := env.Values(".")
		if e != nil {
			return e
		}
		for _, name := range []string{"STORAGE_PROVIDER", "STORAGE_DIR", "STORAGE_ENDPOINT", "STORAGE_BUCKET", "STORAGE_PREFIX", "STORAGE_REGION"} {
			old := current[name]
			if old == "" {
				old = defaults()[name]
			}
			if old != v[name] {
				return errors.New("storage already contains app data; migrate it before changing its backend or location")
			}
		}
	}
	w := row.WorkspaceID
	if w == nil {
		workspace, e := queries.CreateWorkspace(ctx, v["THURA_WORKSPACE_NAME"])
		if e != nil {
			return e
		}
		w = &workspace.ID
		subject := row.Subject
		if subject == "" {
			subject = auth.CurrentUser(ctx).ID
		}
		if e = queries.GrantWorkspaceOwner(ctx, q.GrantWorkspaceOwnerParams{Subject: subject, Scope: workspace.ID}); e != nil {
			return e
		}
		if _, e = queries.CreateMailbox(ctx, q.CreateMailboxParams{WorkspaceID: workspace.ID, Name: "Team mail", Address: v["MAIL_FROM"], ConfigPrefix: ""}); e != nil {
			return e
		}
	} else {
		if e = queries.UpdateSetupWorkspace(ctx, q.UpdateSetupWorkspaceParams{ID: *w, Name: v["THURA_WORKSPACE_NAME"]}); e != nil {
			return e
		}
	}
	if e = queries.PublishServerSetup(ctx, q.PublishServerSetupParams{PublishedAt: timePointer(lidza.Now(ctx)), WorkspaceID: w}); e != nil {
		return e
	}
	if e = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "server.setup.publish", Resource: "server-setup", Meta: map[string]string{"revision": fmt.Sprint(row.Revision)}}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// Activate runs on startup: one encrypted snapshot publishes every setting
// together. Drafts never affect runtime packs. Restart every deployment node.
func Activate(ctx context.Context, s *lidza.Services) error {
	lidza.Provide(s, runtimeState{})
	queries := q.New(db.From(ctx))
	if e := queries.EnsureServerSetup(ctx); e != nil {
		return e
	}
	row, e := queries.GetServerSetup(ctx)
	if e != nil {
		return e
	}
	if row.Published == "" {
		if row.PublishedRevision > 0 {
			// CLI browser fixtures deliberately use .env.test transports and
			// prohibit access to deployment master keys. Their explicit marker
			// represents already configured test services, never a deployment.
			if os.Getenv("LIDZA_MODE") != "test" || row.Subject != fixtureSubject || row.PublishedRevision != 1 {
				return errors.New("published server configuration is missing")
			}
			lidza.Provide(s, runtimeState{Revision: 1})
		}
		return nil
	}
	if row.PublishedRevision <= 0 {
		return errors.New("published server configuration has no valid revision")
	}
	v, e := unseal(row.Published)
	if e != nil {
		return e
	}
	if e = validate(v); e != nil {
		return errors.New("published server setup is invalid")
	}
	overrides := credentials.Overrides()
	for _, step := range Catalog() {
		for _, f := range step.Fields {
			overrides[f.Name] = v[f.Name]
		}
	}
	// Explicit empty values suppress superseded file credentials as well.
	overrides["MAIL_SMTP_URL"] = ""
	overrides["MAIL_BASE_URL"] = ""
	overrides["STORAGE_PUBLIC_URL"] = ""
	overrides["STORAGE_PATH_STYLE"] = ""
	credentials.SetOverrides(overrides)
	if e = lidza.Reconfigure(ctx, s); e != nil {
		return errors.New("published providers could not be initialized; check server settings")
	}
	lidza.Provide(s, runtimeState{Revision: row.PublishedRevision})
	return nil
}

func timePointer(t time.Time) *time.Time { return &t }

func stringPointer(s string) *string { return &s }
