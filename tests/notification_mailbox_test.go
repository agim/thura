package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/agim/lidza"
	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"net/http"
	"strings"
	"testing"
	"thura/app"
	q "thura/db/queries/gen"
	"thura/internal/mailbox"
	"thura/schema"
	"time"
)

// Team notifications are filed like composed mail. The sender copy belongs in
// the author's own inbox; a shared mailbox copy never carries the secret.
func TestTeamNotificationsPreferPrivateInboxAndRedactSharedCopies(t *testing.T) {
	t.Setenv("LIDZA_MASTER_KEY", strings.Repeat("ad", 32))
	t.Setenv("JOBS_WORKERS", "0")
	t.Setenv("AUTH_LOGIN_BURST", "100")
	t.Setenv("AUTH_SIGNIN_BURST", "100")
	t.Setenv("SMIP_MAIL_ENABLED", "false")
	recorder := &teamProviderRecorder{}
	application := app.New(nil)
	start := application.OnStart
	application.OnStart = func(ctx context.Context, s *lidza.Services) error {
		if e := start(ctx, s); e != nil {
			return e
		}
		lidza.Provide[http.RoundTripper](s, recorder)
		return nil
	}
	srv := startConfigured(t, application)
	ctx := srv.Context()
	pool := db.From(ctx)
	queries := q.New(pool)
	suffix := fmt.Sprint(time.Now().UnixNano())
	password := "notice meadow lantern cedar 51873"
	owner, e := auth.From(ctx).CreateUser(ctx, "notice-owner-"+suffix+"@teams.example", "Operator", password)
	if e != nil {
		t.Fatal(e)
	}
	member, e := auth.From(ctx).CreateUser(ctx, "notice-member-"+suffix+"@teams.example", "Member", password)
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("ADMIN_USERS", owner.Subject)
	request := func(method, path string, in, out any, want int) {
		t.Helper()
		res := srv.JSON(t, method, path, in, out)
		if res.StatusCode != want {
			t.Fatalf("%s %s got %d want %d", method, path, res.StatusCode, want)
		}
	}
	login := func(address string) {
		t.Helper()
		request("POST", "/api/v1/auth/login", auth.Credentials{Email: address, Password: password}, nil, 200)
	}
	logout := func() { request("POST", "/api/v1/auth/logout", nil, nil, 204) }
	login(owner.Email)
	var team schema.Workspace
	request("POST", "/api/v1/workspaces", schema.NewTeamInput{Name: "Notices"}, &team, 201)
	base := "/api/v1/workspaces/" + team.ID
	t.Cleanup(func() {
		for _, statement := range []string{"DELETE FROM invitation WHERE workspace_id=$1", "DELETE FROM mail_delivery WHERE transmission_id IN(SELECT id FROM mail_transmission WHERE workspace_id=$1)", "DELETE FROM mail_transmission WHERE workspace_id=$1", "DELETE FROM mail_screening WHERE workspace_id=$1", "DELETE FROM mail_intake_rate WHERE workspace_id=$1", "DELETE FROM mail_item WHERE mailbox_id IN(SELECT id FROM mailbox WHERE workspace_id=$1)", "DELETE FROM mailbox WHERE workspace_id=$1", "DELETE FROM team_mail_config WHERE workspace_id=$1", "DELETE FROM team_domain_binding WHERE workspace_id=$1", "DELETE FROM team_domain WHERE workspace_id=$1", "DELETE FROM team_setup WHERE workspace_id=$1", "DELETE FROM auth_member WHERE scope=$1::text", "DELETE FROM workspace WHERE id=$1"} {
			if _, err := pool.Exec(ctx, statement, team.ID); err != nil {
				t.Error(err)
			}
		}
		auth.From(ctx).DeleteUser(ctx, member.Subject)
		auth.From(ctx).DeleteUser(ctx, owner.Subject)
	})
	domain := "notices-" + suffix + ".example"
	var d schema.TeamDomainView
	request("POST", base+"/domains", schema.TeamDomainInput{Name: domain, Web: true, Mail: true}, &d, 201)
	key := "PUBLIC-fixture-notice-key"
	request("PUT", base+"/domains/"+d.ID+"/mail", schema.TeamMailInput{Provider: "mailgun", APIKey: &key, Region: "us", SmtpSecurity: teamString("starttls"), Enabled: true, Smip: false}, nil, 200)
	// "Accounts" sorts before every personal inbox of this team.
	var shared schema.Mailbox
	request("POST", base+"/mailboxes", schema.TeamMailboxInput{Name: "Accounts", Address: "accounts@" + domain}, &shared, 201)
	request("POST", base+"/team-setup/publish", schema.TeamPublishInput{PrimaryDomain: domain}, nil, 200)
	if e = queries.GrantWorkspaceMember(ctx, q.GrantWorkspaceMemberParams{Scope: team.ID, Subject: member.Subject, Role: "member"}); e != nil {
		t.Fatal(e)
	}
	invite := func(local string) q.MailItem {
		t.Helper()
		assigned := local + "@" + domain
		delivery := local + "-" + suffix + "@external.example"
		request("POST", base+"/invitations", schema.InviteInput{Email: assigned, MailboxAddress: &assigned, DeliveryEmail: &delivery, Role: schema.WorkspaceRoleMember}, nil, 201)
		var item q.MailItem
		if err := pool.QueryRow(ctx, "SELECT id,mailbox_id,text_body FROM mail_item WHERE to_address=$1", delivery).Scan(&item.ID, &item.MailboxID, &item.TextBody); err != nil {
			t.Fatal(err)
		}
		row, err := queries.GetMailTransmission(ctx, item.ID)
		if err != nil {
			t.Fatal(err)
		}
		var frozen struct {
			Payload struct {
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
			} `json:"payload"`
		}
		if err = json.Unmarshal([]byte(row.Payload), &frozen); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(frozen.Payload.Message.Text, "/invite?token=") {
			t.Fatalf("queued transmission lost the invitation link: %q", frozen.Payload.Message.Text)
		}
		payload, _ := json.Marshal(map[string]string{"id": item.ID})
		if err = mailbox.Deliver(ctx, payload); err != nil {
			t.Fatal(err)
		}
		var folder string
		if err = pool.QueryRow(ctx, "SELECT folder FROM mail_item WHERE id=$1", item.ID).Scan(&folder); err != nil || folder != "sent" {
			t.Fatalf("notification not filed as sent: %q %v", folder, err)
		}
		return item
	}
	// Publication backfilled the owner's personal inbox; while it is still an
	// unassigned reservation, the shared mailbox files a redacted copy.
	var private q.Mailbox
	if e = pool.QueryRow(ctx, "SELECT id,name FROM mailbox WHERE workspace_id=$1 AND private AND owner_subject=$2", team.ID, owner.Subject).Scan(&private.ID, &private.Name); e != nil {
		t.Fatal(e)
	}
	if private.Name < shared.Name {
		t.Fatalf("fixture inbox %q must sort after %q", private.Name, shared.Name)
	}
	if _, e = pool.Exec(ctx, "UPDATE mailbox SET owner_subject=NULL WHERE id=$1", private.ID); e != nil {
		t.Fatal(e)
	}
	first := invite("first")
	if first.MailboxID != shared.ID {
		t.Fatal("notification without a personal inbox left the shared mailbox")
	}
	if strings.Contains(first.TextBody, "token=") || !strings.Contains(first.TextBody, "[invitation link sent to first-"+suffix+"@external.example]") {
		t.Fatalf("shared mailbox copy exposes the invitation: %q", first.TextBody)
	}
	// The inviter's own inbox wins over the shared mailbox that sorts first.
	if _, e = queries.AssignPersonalMailbox(ctx, q.AssignPersonalMailboxParams{ID: private.ID, WorkspaceID: team.ID, Subject: &owner.Subject}); e != nil {
		t.Fatal(e)
	}
	second := invite("second")
	if second.MailboxID != private.ID {
		t.Fatal("notification skipped the inviter's personal inbox")
	}
	if !strings.Contains(second.TextBody, "/invite?token=") {
		t.Fatal("personal inbox copy lost the invitation link")
	}
	request("GET", base+"/mailboxes/"+private.ID+"/messages/"+second.ID, nil, nil, 200)
	logout()
	login(member.Email)
	var sent schema.MailItemList
	request("GET", base+"/mailboxes/"+shared.ID+"/messages?folder=sent", nil, &sent, 200)
	if len(sent.Items) != 1 || sent.Items[0].ID != first.ID || strings.Contains(sent.Items[0].TextBody, "token=") {
		t.Fatalf("another member sees private notifications or a token: %+v", sent.Items)
	}
	request("GET", base+"/mailboxes/"+private.ID+"/messages/"+second.ID, nil, nil, 404)
	request("GET", base+"/mailboxes/"+shared.ID+"/messages/"+second.ID, nil, nil, 404)
}
