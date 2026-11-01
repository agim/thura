package tests

import (
	"testing"

	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"thura/internal/smip"
	"thura/schema"
)

// Queueing a SMIP send is for owners and admins, so a send queued by an
// admin who is demoted to member before the attempt is blocked with
// role_revoked and never reaches the network.
func TestSmipDispatchBlocksDemotedRequester(t *testing.T) {
	a := newSmipApp(t, "role-sender.example", "role-receiver.example")
	b := newSmipApp(t, "role-receiver.example", "role-sender.example")
	binding := a.binding(t, b.config.Domain, uuid.NewString())
	smipSendingGateway(t, a, b, smipTLS(t, b))
	ctx := a.server.Context()
	setRole := func(role string) {
		t.Helper()
		if _, err := db.From(ctx).Exec(ctx, "UPDATE auth_member SET role=$3 WHERE subject=$1 AND scope=$2", a.member.Subject, a.workspace.ID, role); err != nil {
			t.Fatal(err)
		}
	}
	setRole("admin")
	a.login(t, a.member)
	var queued schema.SmipOutbound
	a.request(t, "POST", a.base()+"/outbox", schema.SmipSendInput{TransactionID: uuid.NewString(), BindingID: binding.ID, Body: smipText("queued by an admin")}, &queued, 200)
	setRole("member")
	smipDispatch(t, a, queued.ID)

	var state, reason string
	var attempts int
	if err := db.From(ctx).QueryRow(ctx, "SELECT state, reason, attempts FROM smip_outbox WHERE id=$1", queued.ID).Scan(&state, &reason, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != smip.Blocked || reason != "role_revoked" || attempts != 0 {
		t.Fatalf("demoted requester's send was not blocked before any attempt: %s %s %d", state, reason, attempts)
	}
	a.login(t, a.owner)
	var list schema.SmipOutboxList
	a.request(t, "GET", a.base()+"/outbox", nil, &list, 200)
	if len(list.Items) != 1 || list.Items[0].ID != queued.ID || list.Items[0].Reason != "role_revoked" {
		t.Fatalf("outbox does not show the role_revoked block: %+v", list.Items)
	}
}
