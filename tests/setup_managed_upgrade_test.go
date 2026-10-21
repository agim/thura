package tests

import (
	"encoding/json"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/credentials"
	"strings"
	"testing"
	q "thura/db/queries/gen"
	"thura/internal/setup"
	"thura/internal/smipmail"
	"thura/internal/tenancy"
)

func TestUpgradeRepairsInitialTeamRegistrationsAndKeepsSavedProviderAndKeys(t *testing.T) {
	s := setupServer(t)
	claimSetup(t, s)
	completeSetup(t, s)
	ctx := s.Context()
	pool := db.From(ctx)
	queries := q.New(pool)
	row, err := queries.GetServerSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	w, err := queries.CreateWorkspace(ctx, "Retained operator team")
	if err != nil {
		t.Fatal(err)
	}
	if err = queries.GrantWorkspaceOwner(ctx, q.GrantWorkspaceOwnerParams{Scope: w.ID, Subject: row.Subject}); err != nil {
		t.Fatal(err)
	}
	box, err := queries.CreateMailbox(ctx, q.CreateMailboxParams{WorkspaceID: w.ID, Address: "team@setup.example", Name: "Old initial mailbox"})
	if err != nil {
		t.Fatal(err)
	}
	newMember, err := auth.From(ctx).CreateUser(ctx, "postmaster@external.example", "Existing teammate", "PUBLIC upgrade existing account 7834")
	if err != nil {
		t.Fatal(err)
	}
	if err = queries.GrantWorkspaceMember(ctx, q.GrantWorkspaceMemberParams{Scope: w.ID, Subject: newMember.Subject, Role: "member"}); err != nil {
		t.Fatal(err)
	}
	if _, err = queries.CreateMailboxAlias(ctx, q.CreateMailboxAliasParams{MailboxID: box.ID, Address: "postmaster@setup.example"}); err != nil {
		t.Fatal(err)
	}
	// This is a legacy published installation, not a new publication bypass.
	if _, err = pool.Exec(ctx, "UPDATE server_setup SET workspace_id=$1,published=draft,published_revision=revision", w.ID); err != nil {
		t.Fatal(err)
	}
	key, err := credentials.Key(".")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := credentials.Decrypt(key, row.Draft)
	if err != nil {
		t.Fatal(err)
	}
	saved := map[string]string{}
	if err = json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	var identity smipmail.Config
	if err = json.Unmarshal([]byte(saved["SMIP_MAIL_CONFIG"]), &identity); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THURA_MANAGED_SETTINGS", "MATRIX_SERVER_URL,MATRIX_AS_TOKEN,MAIL_PROVIDER,STORAGE_PREFIX")
	t.Setenv("THURA_SERVICE_MATRIX_SERVER_URL", "http://new-deployment-default:8008")
	if err = setup.Activate(ctx, s.Services); err != nil {
		t.Fatal(err)
	}
	d, err := queries.BoundTeamDomain(ctx, "setup.example")
	if err != nil || d.WorkspaceID != w.ID || !d.Mail || !d.Web || d.Verification != "administrator" {
		t.Fatal("initial operator domain was not registered", err)
	}
	config, secrets, err := tenancy.LoadMail(ctx, w.ID, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if config.Provider != saved["MAIL_PROVIDER"] || secrets.Seed != identity.Seed || secrets.InboundSecret != saved["MAIL_INBOUND_SECRET"] {
		t.Fatal("upgrade replaced a saved provider or signing/relay credential")
	}
	initial, err := queries.GetMailbox(ctx, box.ID)
	if err != nil || !initial.Private || initial.OwnerSubject == nil || *initial.OwnerSubject != row.Subject {
		t.Fatal("operator mailbox was not made private", err)
	}
	members, err := queries.ListWorkspaceMembers(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	assigned := ""
	for _, member := range members {
		if member.Subject == newMember.Subject {
			assigned = member.MailboxAddress
		}
	}
	if !strings.HasPrefix(assigned, "postmaster-") || !strings.HasSuffix(assigned, "@setup.example") {
		t.Fatal("existing member did not receive a collision-safe personal address", assigned)
	}
	after, err := queries.GetServerSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Repeated startup is idempotent; registration and encrypted keys persist.
	if err = setup.Activate(ctx, s.Services); err != nil {
		t.Fatal(err)
	}
	again, err := queries.GetServerSetup(ctx)
	if err != nil || again.Revision != after.Revision || again.Draft != after.Draft || again.Published != after.Published {
		t.Fatal("repeated deployment changed settings", err)
	}
	retained, err := queries.GetTeamMailConfig(ctx, q.GetTeamMailConfigParams{WorkspaceID: w.ID, DomainID: d.ID})
	if err != nil || retained.Sealed != config.Sealed {
		t.Fatal("repeated deployment rotated team credentials", err)
	}
	// An exact-address legacy migration may expose an ownership conflict. Fail
	// activation rather than silently transferring that person's retained mail.
	if _, err = pool.Exec(ctx, "UPDATE mailbox SET owner_subject=$1 WHERE id=$2", newMember.Subject, box.ID); err != nil {
		t.Fatal(err)
	}
	if err = setup.Activate(ctx, s.Services); err == nil {
		t.Fatal("administrator inbox ownership conflict was silently accepted")
	}
	retainedBox, err := queries.GetMailbox(ctx, box.ID)
	if err != nil || retainedBox.OwnerSubject == nil || *retainedBox.OwnerSubject != newMember.Subject {
		t.Fatal("conflicting inbox ownership was transferred", err)
	}
}
