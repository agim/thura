package setup

import (
	"context"

	"github.com/agim/lidza/packs/db"
	q "thura/db/queries/gen"
)

// Preserve model changes made in Settings across restarts of wizard-configured
// installations. Merge only AI fields so an unfinished wizard draft stays intact.
func RememberModelSettings(ctx context.Context, values map[string]string) error {
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
	if row.Published == "" {
		return nil
	}
	published, err := unseal(row.Published)
	if err != nil {
		return err
	}
	if published["LLM_PROVIDER"] == "" {
		return nil // older installations keep their existing Settings ownership
	}
	draft, err := unseal(row.Draft)
	if err != nil {
		return err
	}
	step, _ := stepByID("ai")
	for _, field := range step.Fields {
		if value, present := values[field.Name]; present {
			published[field.Name], draft[field.Name] = value, value
		}
	}
	sealedDraft, err := seal(draft)
	if err != nil {
		return err
	}
	sealedPublished, err := seal(published)
	if err != nil {
		return err
	}
	if err = queries.UpdateSetupModelSettings(ctx, q.UpdateSetupModelSettingsParams{Draft: sealedDraft, Published: sealedPublished}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
