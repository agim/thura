package tests

import (
	"testing"

	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"thura/schema"
)

// SMIP streams are operator plumbing: listing, authorizing and disabling them
// is for workspace owners and admins, and a member is refused every one.
func TestSmipStreamRoutesRefuseMembers(t *testing.T) {
	a := newSmipApp(t, "streams-local.example", "streams-peer.example")
	ctx := a.server.Context()
	if _, err := db.From(ctx).Exec(ctx, "INSERT INTO mailbox(workspace_id,name,address,config_prefix,private,owner_subject) VALUES($1,'Team mail','team@team.example','TEAM',false,NULL),($1,'Private','own@team.example','OWN',true,$2)", a.workspace.ID, a.owner.Subject); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.From(ctx).Exec(ctx, "DELETE FROM mailbox WHERE workspace_id=$1", a.workspace.ID); err != nil {
			t.Error(err)
		}
	})
	binding := a.binding(t, "streams-peer.example", "team-chat")

	// The owner's list carries the choices the form offers: the paired peer and
	// shared mailboxes only, as addresses at the local SMIP domain.
	var list schema.SmipBindingList
	a.request(t, "GET", a.base()+"/bindings", nil, &list, 200)
	if !list.Configured || list.Domain != "streams-local.example" || len(list.Peers) != 1 || list.Peers[0] != "streams-peer.example" {
		t.Fatalf("operator choices missing: %+v", list)
	}
	if len(list.Recipients) != 1 || list.Recipients[0].Address != "team@streams-local.example" || list.Recipients[0].Mailbox != "Team mail" {
		t.Fatalf("recipient choices wrong: %+v", list.Recipients)
	}

	a.login(t, a.member)
	in := schema.SmipBindingInput{RequestID: uuid.NewString(), Peer: "streams-peer.example", Stream: "other", Sender: "team@streams-peer.example", Recipient: "team@streams-local.example"}
	a.request(t, "POST", a.base()+"/bindings", in, nil, 403)
	a.request(t, "GET", a.base()+"/bindings", nil, nil, 403)
	a.request(t, "DELETE", a.base()+"/bindings/"+binding.ID, nil, nil, 403)
	var count int
	if err := db.From(ctx).QueryRow(ctx, "SELECT COUNT(*) FROM smip_binding WHERE workspace_id=$1 AND enabled", a.workspace.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("a member changed SMIP streams", count, err)
	}
}
