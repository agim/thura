package setup

import (
	"context"
	"errors"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/packs/db"
	"os"
	"strings"
	q "thura/db/queries/gen"
)

// Only infrastructure owned by deployment may be reconciled. Provider choices,
// domains, storage locations, policies and signing identities are never defaults
// that a redeployment is allowed to replace.
func managedNames() [14]string {
	return [14]string{
		"MATRIX_SERVER_URL", "MATRIX_SERVER_NAME", "MATRIX_AS_TOKEN",
		"COLLABORA_SERVER_URL", "COLLABORA_APP_URL", "COLLABORA_TOKEN_SECRET",
		"LIVEKIT_SERVER_URL", "LIVEKIT_PUBLIC_URL", "LIVEKIT_API_KEY", "LIVEKIT_API_SECRET",
		"MAIL_RSPAMD_URL", "MAIL_RSPAMD_CONTROLLER_URL", "MAIL_RSPAMD_PASSWORD", "MAIL_CLAMD_ADDRESS",
	}
}

func managedField(name string) bool {
	known := false
	for _, allowed := range managedNames() {
		if allowed == name {
			known = true
			break
		}
	}
	if !known {
		return false
	}
	for _, candidate := range strings.Split(os.Getenv("THURA_MANAGED_SETTINGS"), ",") {
		if strings.TrimSpace(candidate) == name {
			return true
		}
	}
	return false
}

func fillManaged(v map[string]string) bool {
	changed := false
	for _, name := range managedNames() {
		if !managedField(name) || v[name] != "" {
			continue
		}
		value := os.Getenv("THURA_SERVICE_" + name)
		if value == "" {
			value = os.Getenv(name)
		}
		if value != "" {
			v[name] = value
			changed = true
		}
	}
	return localModelDefaults(v) || changed
}

// ReconcileManaged repairs missing deployment-owned settings on upgrade in one
// locked transaction. It never rotates a saved key, overwrites an operator
// choice, publishes an unfinished draft or marks an integration as verified.
func ReconcileManaged(ctx context.Context) error {
	if os.Getenv("THURA_MANAGED_SETTINGS") == "" && os.Getenv("THURA_MANAGED_OLLAMA") != "true" {
		return nil
	}
	tx, err := db.From(ctx).Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries := q.New(tx)
	row, err := queries.LockServerSetup(ctx)
	if err != nil {
		return err
	}
	if row.Draft == "" {
		return nil
	}
	draft, err := unseal(row.Draft)
	if err != nil {
		return err
	}
	changed := fillManaged(draft)
	published := row.Published
	publishedChanged := false
	if published != "" {
		active, err := unseal(published)
		if err != nil {
			return err
		}
		publishedChanged = fillManaged(active)
		identityChanged, err := configureFederationIdentity(active, lidza.Now(ctx))
		if err != nil {
			return err
		}
		publishedChanged = identityChanged || publishedChanged
		if draft["SMIP_CONFIG"] == "" && active["SMIP_CONFIG"] != "" {
			draft["SMIP_CONFIG"] = active["SMIP_CONFIG"]
			changed = true
		}
		if publishedChanged {
			published, err = seal(active)
			if err != nil {
				return err
			}
		}
	}
	identityChanged, err := configureFederationIdentity(draft, lidza.Now(ctx))
	if err != nil {
		return err
	}
	changed = identityChanged || changed
	if !changed && !publishedChanged {
		return nil
	}
	sealed, err := seal(draft)
	if err != nil {
		return err
	}
	if err = queries.ReconcileServerSetup(ctx, q.ReconcileServerSetupParams{Draft: sealed, Published: published, RepairPublished: publishedChanged}); err != nil {
		return err
	}
	if err = audit.From(ctx).RecordTx(audit.System(ctx, "deployment-reconciliation"), tx, audit.Event{Action: "server.setup.reconcile", Resource: "server-setup", Meta: map[string]string{"policy": "fill-missing-managed-v1"}}); err != nil {
		return errors.New("could not audit managed configuration repair")
	}
	return tx.Commit(ctx)
}

func managedTitle(name string) string {
	switch name {
	case "MATRIX_SERVER_URL":
		return "Chat"
	case "COLLABORA_SERVER_URL":
		return "Document editing"
	case "LIVEKIT_SERVER_URL":
		return "Meetings"
	case "MAIL_RSPAMD_URL":
		return "Spam filtering"
	case "MAIL_CLAMD_ADDRESS":
		return "Attachment antivirus"
	}
	return ""
}
