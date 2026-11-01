package tests

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/packs/db"
	"github.com/google/uuid"
	"thura/internal/mailbox"
	"thura/internal/platform/providererr"
	"thura/schema"
)

// Members use the workspace; owners and admins run it. A member is refused
// the shared mailbox's identity and label deletion, and reads no storage
// keys, provider text or setting names; an owner and the server
// administrator keep what they act on.
func TestMemberSurfaces(t *testing.T) {
	n := newMailNode(t, "members.example")
	ctx := n.server.Context()
	const password = "member lantern copper meadow 6612"
	const ownerPassword = "mail meadow river lantern 87344"
	member, err := auth.From(ctx).CreateUser(ctx, "member@members.example", "Plain member", password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.From(ctx).Exec(ctx, "INSERT INTO auth_member(subject,scope,role) VALUES($1,$2,'member')", member.Subject, n.workspace.ID); err != nil {
		t.Fatal(err)
	}
	private, err := mailbox.Provision(ctx, schema.ProvisionMailboxInput{WorkspaceID: n.workspace.ID, Name: "Mine", Address: "member@members.example"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.From(ctx).Exec(ctx, "UPDATE mailbox SET private=true, owner_subject=$2 WHERE id=$1", private.ID, member.Subject); err != nil {
		t.Fatal(err)
	}
	// A sent message the provider refused, with every internal key set.
	var blocked string
	if err = db.From(ctx).QueryRow(ctx, `INSERT INTO mail_item(mailbox_id,author_id,folder,from_address,to_address,subject,status,provider_id,delivery_reason,raw_key,external_id)
		VALUES($1,$2,'sent','team@members.example','someone@example.net','Refused','delivery_blocked','<provider-id@mta>','Provider refused the message: 550 5.1.1 mailbox unknown','raw/secret-key','relay-delivery-1') RETURNING id`, n.box.ID, n.owner.Subject).Scan(&blocked); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/workspaces/" + n.workspace.ID
	shared := base + "/mailboxes/" + n.box.ID
	login := func(email, pw string) {
		t.Helper()
		n.request(t, "POST", "/api/v1/auth/login", auth.Credentials{Email: email, Password: pw}, nil, 200)
	}
	// raw reads a route and returns its JSON text, to look for keys by name.
	raw := func(path string) string {
		t.Helper()
		r := n.server.JSON(t, "GET", path, nil, nil)
		if r.StatusCode != 200 {
			t.Fatalf("GET %s = %d", path, r.StatusCode)
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	hidden := func(who, body string) {
		t.Helper()
		for _, leak := range []string{"configPrefix", "<provider-id@mta>", "raw/secret-key", "relay-delivery-1"} {
			if strings.Contains(body, leak) {
				t.Fatalf("%s reads %q: %s", who, leak, body)
			}
		}
	}
	name, signature := "Shared desk", "Regards"

	login(member.Email, password)
	// The shared mailbox's identity is the managers'; the private one is the member's.
	n.request(t, "PUT", shared+"/signature", schema.MailSignatureInput{Signature: &signature, SenderName: &name}, nil, 403)
	n.request(t, "PUT", base+"/mailboxes/"+private.ID+"/signature", schema.MailSignatureInput{Signature: &signature, SenderName: &name}, nil, 200)
	// Any member creates and applies a label; deleting one is the managers'.
	var label schema.MailLabel
	n.request(t, "POST", base+"/labels", schema.MailLabelInput{Name: "Member made"}, &label, 201)
	n.request(t, "PATCH", shared+"/messages/"+blocked+"/labels", schema.MailLabelChange{LabelID: label.ID, Applied: true}, nil, 204)
	n.request(t, "PATCH", shared+"/messages/"+blocked+"/labels", schema.MailLabelChange{LabelID: label.ID, Applied: false}, nil, 204)
	n.request(t, "DELETE", base+"/labels/"+label.ID, nil, nil, 403)
	n.request(t, "DELETE", shared+"/labels/"+label.ID, nil, nil, 403)
	// The member's mail views carry no internal keys and no provider text.
	hidden("member", raw(base+"/mailboxes"))
	hidden("member", raw(shared+"/messages?folder=sent"))
	detailBody := raw(shared + "/messages/" + blocked)
	hidden("member", detailBody)
	var detail schema.MailDetail
	if err = json.Unmarshal([]byte(detailBody), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.CanManage || detail.Item.DeliveryReason != "Not delivered. Try again or contact your administrator." {
		t.Fatalf("member delivery reason %q (canManage %v)", detail.Item.DeliveryReason, detail.CanManage)
	}
	// Meet names no setting, and providers answer in one plain sentence.
	var meetings schema.MeetingList
	n.request(t, "GET", base+"/meet", nil, &meetings, 200)
	if meetings.Configured || meetings.Unavailable != "" {
		t.Fatalf("member meeting list: %+v", meetings)
	}
	generic := func(method, path string, in any, want string) {
		t.Helper()
		var reply struct {
			Error string `json:"error"`
		}
		r := n.server.JSON(t, method, path, in, nil)
		if r.StatusCode != 503 {
			t.Fatalf("%s %s = %d want 503", method, path, r.StatusCode)
		}
		if err := json.NewDecoder(r.Body).Decode(&reply); err != nil || reply.Error != want {
			t.Fatalf("%s %s answered %q want %q", method, path, reply.Error, want)
		}
	}
	generic("POST", base+"/meet", schema.MeetingInput{Name: "Standup", RequestID: uuid.NewString()}, providererr.Meet)
	generic("POST", base+"/chat", schema.ChatRoomInput{Name: "Room", RequestID: uuid.NewString()}, providererr.Chat)
	generic("GET", base+"/drive/office-capabilities", nil, providererr.Editor)
	var ai schema.AISettings
	n.request(t, "GET", base+"/ai/settings", nil, &ai, 200)
	if ai.CanConfigure || ai.Provider != "" || ai.Model != "" || ai.BaseURL != "" {
		t.Fatalf("member AI settings name the service: %+v", ai)
	}

	login(n.owner.Email, ownerPassword)
	// The owner changes the shared identity, deletes labels and reads the provider's reason.
	n.request(t, "PUT", shared+"/signature", schema.MailSignatureInput{Signature: &signature, SenderName: &name}, nil, 200)
	detailBody = raw(shared + "/messages/" + blocked)
	hidden("owner", detailBody)
	detail = schema.MailDetail{}
	if err = json.Unmarshal([]byte(detailBody), &detail); err != nil {
		t.Fatal(err)
	}
	if !detail.CanManage || !strings.Contains(detail.Item.DeliveryReason, "550 5.1.1") {
		t.Fatalf("owner delivery reason %q (canManage %v)", detail.Item.DeliveryReason, detail.CanManage)
	}
	n.request(t, "DELETE", base+"/labels/"+label.ID, nil, nil, 204)
	// A workspace owner is not the server administrator: no setting names.
	n.request(t, "GET", base+"/meet", nil, &meetings, 200)
	if meetings.Unavailable != "" {
		t.Fatalf("owner reads the Meet setting: %q", meetings.Unavailable)
	}
	// The server administrator reads which setting is missing.
	t.Setenv("ADMIN_USERS", n.owner.Subject)
	n.request(t, "GET", base+"/meet", nil, &meetings, 200)
	if meetings.Configured || meetings.Unavailable == "" {
		t.Fatalf("administrator meeting list: %+v", meetings)
	}
}
