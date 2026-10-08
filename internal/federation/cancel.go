package federation

import (
	"context"

	"github.com/agim/lidza/packs/audit"
	"github.com/agim/lidza/pkg/router"
	q "thura/db/queries/gen"
	"thura/internal/smip"
	"thura/schema"
)

// Cancelled is a local terminal state. It never claims to recall a packet
// already attempted, and does not change the SMIP wire protocol.
const Cancelled = "cancelled"

func Cancel(ctx context.Context, w, id string) (schema.SmipOutbound, error) {
	tx, queries, row, err := lockManagedOutbound(ctx, w, id)
	if err != nil {
		return schema.SmipOutbound{}, err
	}
	defer tx.Rollback(ctx)
	if row.State == Cancelled {
		return outboundView(row), nil
	}
	// Dispatch takes these same workspace/row locks and commits Attempts before
	// HTTP. Whichever transaction wins defines whether cancellation is safe.
	if row.Attempts != 0 || (row.State != smip.Pending && row.State != smip.Blocked) {
		return schema.SmipOutbound{}, router.Errorf(409, "only unattempted pending or blocked packets can be cancelled")
	}
	row.State = Cancelled
	row.Reason = "cancelled_unsent"
	if err = saveOutbound(ctx, queries, row); err != nil {
		return schema.SmipOutbound{}, err
	}
	if err = audit.From(ctx).RecordTx(ctx, tx, audit.Event{Action: "smip.message.cancel", Scope: w, Resource: "smip-outbox/" + row.ID}); err != nil {
		return schema.SmipOutbound{}, err
	}
	// Read the persisted timestamps so an idempotent retry returns exactly the
	// same view, including Postgres timestamp precision.
	row, err = queries.GetSmipOutbound(ctx, q.GetSmipOutboundParams{WorkspaceID: w, ID: id})
	if err != nil {
		return schema.SmipOutbound{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return schema.SmipOutbound{}, err
	}
	return outboundView(row), nil
}
