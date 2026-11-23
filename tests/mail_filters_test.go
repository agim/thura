package tests

import (
	"testing"

	"github.com/agim/lidza/packs/db"
	"thura/schema"
)

func ptr[T any](v T) *T { return &v }

// Mail filters file new mail as it reaches the Inbox, as in Gmail: a label of
// its own, out of the Inbox, read, starred or to Trash. Filtered mail does
// not notify, mail already in the Inbox is filed on request, and a filter is
// the mailbox's own.
func TestMailFiltersFileArrivingMail(t *testing.T) {
	n := pushNode(t, "mail-filters.example")
	ctx := n.server.Context()
	pool := db.From(ctx)
	w := n.workspace.ID
	filters := "/api/v1/workspaces/" + w + "/mailboxes/" + n.box.ID + "/filters"
	notifications := "/api/v1/workspaces/" + w + "/notifications"
	service := newPushService(t)
	n.subscribePush(t, service.server.URL+"/owner", 200)
	n.request(t, "PUT", notifications+"/preferences", schema.NotificationPreferences{Mail: true, MailPeopleOnly: false, Chat: true, ChatRequests: true, Calendar: true, Meet: true}, nil, 200)
	folderOf := func(item schema.MailItem) schema.MailFolder { return n.detail(t, item).Item.Folder }
	labelsOf := func(item schema.MailItem) []string {
		out := []string{}
		for _, l := range n.detail(t, item).Labels {
			out = append(out, l.Name)
		}
		return out
	}

	// A filter needs something to match and something to do.
	n.request(t, "POST", filters, schema.MailFilterInput{SkipInbox: ptr(true)}, nil, 422)
	n.request(t, "POST", filters, schema.MailFilterInput{FromContains: ptr(" , ")}, nil, 422)
	n.request(t, "POST", filters, schema.MailFilterInput{FromContains: ptr("news@outside.example")}, nil, 422)
	n.request(t, "POST", filters, schema.MailFilterInput{FromContains: ptr("news@outside.example"), LabelID: ptr("00000000-0000-4000-8000-000000000000")}, nil, 422)

	// Mail already in the Inbox is filed when the filter asks for it.
	before := receiveAbuse(t, n, "filter-before", "Weekly News <news@outside.example>", "Older issue", "", 200)
	other := receiveAbuse(t, n, "filter-other", "Ada <ada@outside.example>", "Lunch?", "", 200)
	clearPushJobs(t, n)
	var created schema.MailFilterResult
	n.request(t, "POST", filters, schema.MailFilterInput{FromContains: ptr("NEWS@outside.example, digest@outside.example"), LabelName: ptr("Newsletters"), SkipInbox: ptr(true), MarkRead: ptr(true), ApplyExisting: ptr(true)}, &created, 201)
	if created.Applied != 1 || created.Filter.LabelID == nil || !created.Filter.Enabled || !created.Filter.SkipInbox {
		t.Fatalf("created filter: %+v", created)
	}
	if folderOf(before) != schema.MailFolderArchive || len(labelsOf(before)) != 1 || labelsOf(before)[0] != "Newsletters" || n.detail(t, before).Item.Unread {
		t.Fatalf("existing mail not filed: %s %v", folderOf(before), labelsOf(before))
	}
	if folderOf(other) != schema.MailFolderInbox || len(labelsOf(other)) != 0 {
		t.Fatal("a message the filter does not match was filed")
	}
	// The label is the workspace's, reused by name.
	var again schema.MailFilterResult
	n.request(t, "POST", filters, schema.MailFilterInput{SubjectContains: ptr("no such subject"), LabelName: ptr("newsletters")}, &again, 201)
	if again.Filter.LabelID == nil || *again.Filter.LabelID != *created.Filter.LabelID || again.Applied != 0 {
		t.Fatalf("label not reused: %+v", again)
	}

	// New mail: a match skips the Inbox with its label and does not notify;
	// the member's log says a filter moved it. Other mail arrives as before.
	filed := receiveAbuse(t, n, "filter-new", "Weekly News <news@outside.example>", "This week", "", 200)
	if folderOf(filed) != schema.MailFolderArchive || len(labelsOf(filed)) != 1 || n.detail(t, filed).Item.Unread {
		t.Fatalf("new mail not filed: %s %v", folderOf(filed), labelsOf(filed))
	}
	if jobs := pushJobs(t, n); len(jobs) != 0 {
		t.Fatalf("filtered mail notified: %d", len(jobs))
	}
	var settings schema.NotificationSettings
	n.request(t, "GET", notifications, nil, &settings, 200)
	if len(settings.Recent) == 0 || settings.Recent[0].Reason != "filtered" || settings.Recent[0].Status != "skipped" {
		t.Fatalf("recent after a filtered message: %+v", settings.Recent)
	}
	plain := receiveAbuse(t, n, "filter-plain", "Ada <ada@outside.example>", "Lunch again?", "", 200)
	if folderOf(plain) != schema.MailFolderInbox {
		t.Fatalf("unmatched mail in %s", folderOf(plain))
	}
	if jobs := pushJobs(t, n); len(jobs) != 1 {
		t.Fatalf("unmatched mail: %d notifications", len(jobs))
	}
	clearPushJobs(t, n)
	// The label view lists the filed mail though it left the Inbox.
	var list schema.MailItemList
	n.request(t, "GET", n.base()+"?labelId="+*created.Filter.LabelID, nil, &list, 200)
	if len(list.Items) != 2 {
		t.Fatalf("label view lists %d", len(list.Items))
	}
	n.request(t, "GET", n.base()+"?folder=inbox", nil, &list, 200)
	for _, item := range list.Items {
		if item.ID == filed.ID || item.ID == before.ID {
			t.Fatal("filed mail still in the Inbox")
		}
	}

	// A filter that is off does nothing; turned on again it files.
	var toggled schema.MailFilterResult
	n.request(t, "PUT", filters+"/"+created.Filter.ID, schema.MailFilterInput{Enabled: ptr(false)}, &toggled, 200)
	if toggled.Filter.Enabled || toggled.Filter.FromContains != created.Filter.FromContains || !toggled.Filter.SkipInbox {
		t.Fatalf("toggle changed the filter: %+v", toggled.Filter)
	}
	if off := receiveAbuse(t, n, "filter-off", "Weekly News <news@outside.example>", "Off", "", 200); folderOf(off) != schema.MailFolderInbox {
		t.Fatal("a filter that is off filed mail")
	}
	n.request(t, "PUT", filters+"/"+created.Filter.ID, schema.MailFilterInput{Enabled: ptr(true)}, nil, 200)

	// Star keeps it in the Inbox; Trash wins over everything else. Words
	// match the body, alternatives any one.
	n.request(t, "POST", filters, schema.MailFilterInput{HasWords: ptr("quarterly report"), Star: ptr(true)}, nil, 201)
	n.request(t, "POST", filters, schema.MailFilterInput{FromContains: ptr("@junk.example"), Trash: ptr(true)}, nil, 201)
	starred := receiveAbuse(t, n, "filter-star", "Bo <bo@outside.example>", "The Quarterly Report is attached", "", 200)
	if d := n.detail(t, starred); d.Item.Folder != schema.MailFolderInbox || !d.Item.Starred || !d.Item.Unread {
		t.Fatalf("starred mail: %+v", d.Item)
	}
	if junk := receiveAbuse(t, n, "filter-junk", "Shop <offers@junk.example>", "quarterly report sale", "", 200); folderOf(junk) != schema.MailFolderTrash {
		t.Fatalf("trash filter left mail in %s", folderOf(junk))
	}
	clearPushJobs(t, n)
	// An edit keeps what it does not name, and an empty label id clears the label.
	var edited schema.MailFilterResult
	n.request(t, "PUT", filters+"/"+again.Filter.ID, schema.MailFilterInput{Star: ptr(true)}, &edited, 200)
	if edited.Filter.LabelID == nil || !edited.Filter.Star || edited.Filter.SubjectContains != "no such subject" {
		t.Fatalf("partial edit: %+v", edited.Filter)
	}
	n.request(t, "PUT", filters+"/"+again.Filter.ID, schema.MailFilterInput{LabelID: ptr("")}, &edited, 200)
	if edited.Filter.LabelID != nil || !edited.Filter.Star {
		t.Fatalf("label not cleared: %+v", edited.Filter)
	}
	// Without its label and its star it would do nothing.
	n.request(t, "PUT", filters+"/"+again.Filter.ID, schema.MailFilterInput{Star: ptr(false)}, nil, 422)

	var all schema.MailFilterList
	n.request(t, "GET", filters, nil, &all, 200)
	if len(all.Items) != 4 || all.Items[0].ID != created.Filter.ID {
		t.Fatalf("filters listed: %d", len(all.Items))
	}

	// A shared mailbox's filters are its admins': a member reads them and
	// cannot change them. Another workspace's mailbox is not found.
	member := n.addMember(t, "member@mail-filters.example")
	n.login(t, member.Email)
	n.request(t, "GET", filters, nil, &all, 200)
	n.request(t, "POST", filters, schema.MailFilterInput{FromContains: ptr("x@y.example"), Trash: ptr(true)}, nil, 403)
	n.request(t, "PUT", filters+"/"+created.Filter.ID, schema.MailFilterInput{Enabled: ptr(false)}, nil, 403)
	n.request(t, "DELETE", filters+"/"+created.Filter.ID, nil, nil, 403)
	// A private mailbox's filters are its owner's alone.
	var private string
	if err := pool.QueryRow(ctx, "INSERT INTO mailbox(workspace_id,name,address,config_prefix,private,owner_subject) VALUES($1,'Member','member@mail-filters.example','',true,$2) RETURNING id", w, member.Subject).Scan(&private); err != nil {
		t.Fatal(err)
	}
	own := "/api/v1/workspaces/" + w + "/mailboxes/" + private + "/filters"
	var mine schema.MailFilterResult
	n.request(t, "POST", own, schema.MailFilterInput{SubjectContains: ptr("receipt"), LabelName: ptr("Receipts")}, &mine, 201)
	// A filter id of another mailbox is not found there.
	n.request(t, "PUT", own+"/"+created.Filter.ID, schema.MailFilterInput{Enabled: ptr(false)}, nil, 404)
	n.login(t, n.owner.Email)
	n.request(t, "GET", own, nil, nil, 404)
	n.request(t, "PUT", own+"/"+mine.Filter.ID, schema.MailFilterInput{Enabled: ptr(false)}, nil, 404)
	n.request(t, "DELETE", own+"/"+mine.Filter.ID, nil, nil, 404)

	// Deleting a label takes its filters along; deleting a filter leaves
	// the mail it filed where it is.
	n.request(t, "DELETE", "/api/v1/workspaces/"+w+"/labels/"+*created.Filter.LabelID, nil, nil, 204)
	n.request(t, "GET", filters, nil, &all, 200)
	if len(all.Items) != 3 {
		t.Fatalf("filters after deleting their label: %d", len(all.Items))
	}
	if folderOf(filed) != schema.MailFolderArchive {
		t.Fatal("deleting the label moved filed mail")
	}
	n.request(t, "DELETE", filters+"/"+all.Items[0].ID, nil, nil, 204)
	n.request(t, "DELETE", filters+"/"+all.Items[0].ID, nil, nil, 404)
	n.request(t, "DELETE", filters+"/not-an-id", nil, nil, 404)
}
