package tests

import (
	"fmt"
	"testing"

	"github.com/agim/lidza/packs/db"
	"thura/internal/workspace"
	"thura/schema"
)

// A label is a view, not a filter: ?labelId= without ?folder= lists the
// label's messages from every folder of the mailbox but Spam and Trash, with
// the same paging and search as a folder; ?includeSpamTrash=true adds them.
func TestMailLabelViewListsEveryFolder(t *testing.T) {
	n := newMailNode(t, "label-view.example")
	ctx := n.server.Context()
	pool := db.From(ctx)
	w := n.workspace.ID
	base := "/api/v1/workspaces/" + w
	var label schema.MailLabel
	n.request(t, "POST", base+"/labels", schema.MailLabelInput{Name: "Stick it"}, &label, 201)
	ids := map[string]string{}
	// Newest first: inbox is the newest message, trash the oldest.
	for i, folder := range []string{"inbox", "sent", "archive", "drafts", "spam", "trash"} {
		var id string
		if err := pool.QueryRow(ctx, "INSERT INTO mail_item(mailbox_id,author_id,folder,subject,text_body,status,updated_at) VALUES($1,$2,$3::mail_folder,$4,$5,'received',now()-($6::int*interval '1 minute')) RETURNING id", n.box.ID, n.owner.Subject, folder, "Labeled "+folder, "body of "+folder, i).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[folder] = id
		n.request(t, "PATCH", n.base()+"/"+id+"/labels", schema.MailLabelChange{LabelID: label.ID, Applied: true}, nil, 204)
	}
	var unlabeled string
	if err := pool.QueryRow(ctx, "INSERT INTO mail_item(mailbox_id,author_id,folder,subject,status) VALUES($1,$2,'sent','Not labeled','sent') RETURNING id", n.box.ID, n.owner.Subject).Scan(&unlabeled); err != nil {
		t.Fatal(err)
	}
	folders := func(list schema.MailItemList) string {
		out := ""
		for _, item := range list.Items {
			out += string(item.Folder) + " "
		}
		return out
	}
	var list schema.MailItemList
	n.request(t, "GET", n.base()+"?labelId="+label.ID, nil, &list, 200)
	if got := folders(list); got != "inbox sent archive drafts " {
		t.Fatalf("label view folders %q", got)
	}
	// Paging: two at a time, the cursor carries on across folders.
	n.request(t, "GET", n.base()+"?labelId="+label.ID+"&limit=2", nil, &list, 200)
	if folders(list) != "inbox sent " || list.NextCursor == "" {
		t.Fatalf("first page %q cursor %q", folders(list), list.NextCursor)
	}
	n.request(t, "GET", n.base()+"?labelId="+label.ID+"&limit=2&cursor="+list.NextCursor, nil, &list, 200)
	if folders(list) != "archive drafts " || list.NextCursor != "" {
		t.Fatalf("second page %q cursor %q", folders(list), list.NextCursor)
	}
	// Search stays inside the label.
	n.request(t, "GET", n.base()+"?labelId="+label.ID+"&search=body+of+sent", nil, &list, 200)
	if len(list.Items) != 1 || list.Items[0].ID != ids["sent"] {
		t.Fatalf("search in label view %q", folders(list))
	}
	n.request(t, "GET", n.base()+"?labelId="+label.ID+"&search=Not+labeled", nil, &list, 200)
	if len(list.Items) != 0 {
		t.Fatal("label view search found an unlabeled message")
	}
	n.request(t, "GET", n.base()+"?labelId="+label.ID+"&includeSpamTrash=true", nil, &list, 200)
	if got := folders(list); got != "inbox sent archive drafts spam trash " {
		t.Fatalf("label view with Spam and Trash %q", got)
	}
	n.request(t, "GET", n.base()+"?labelId="+label.ID+"&includeSpamTrash=maybe", nil, nil, 422)
	// A folder with a label still narrows that folder, and folders alone are unchanged.
	n.request(t, "GET", n.base()+"?folder=sent&labelId="+label.ID, nil, &list, 200)
	if len(list.Items) != 1 || list.Items[0].ID != ids["sent"] {
		t.Fatalf("folder and label %q", folders(list))
	}
	n.request(t, "GET", n.base()+"?folder=sent", nil, &list, 200)
	if len(list.Items) != 2 {
		t.Fatalf("Sent folder lists %d", len(list.Items))
	}
	// Removing the label takes the message out of the view.
	n.request(t, "PATCH", n.base()+"/"+ids["archive"]+"/labels", schema.MailLabelChange{LabelID: label.ID, Applied: false}, nil, 204)
	n.request(t, "GET", n.base()+"?labelId="+label.ID, nil, &list, 200)
	if got := folders(list); got != "inbox sent drafts " {
		t.Fatalf("after removing the label %q", got)
	}

	// A label of another workspace is not found, and lists nothing of its own.
	other, err := workspace.Create(ctx, schema.CreateWorkspaceInput{Name: "Elsewhere", OwnerEmail: n.owner.Email})
	if err != nil {
		t.Fatal(err)
	}
	var foreign string
	if err := pool.QueryRow(ctx, "INSERT INTO mail_label(workspace_id,name) VALUES($1,'Stick it') RETURNING id", other.ID).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO mail_tag(item_id,label_id) VALUES($1,$2)", ids["inbox"], foreign); err != nil {
		t.Fatal(err)
	}
	n.request(t, "GET", n.base()+"?labelId="+foreign, nil, nil, 404)
	n.request(t, "GET", n.base()+"?labelId=not-a-label", nil, nil, 404)
}

// Drive's label view is every file carrying the label in any folder; the
// listing gives each file its folder so the rows can name it.
func TestDriveLabelViewSpansFolders(t *testing.T) {
	n := newMailNode(t, "drive-label-view.example")
	ctx := n.server.Context()
	pool := db.From(ctx)
	w := n.workspace.ID
	base := "/api/v1/workspaces/" + w
	var label schema.MailLabel
	n.request(t, "POST", base+"/labels", schema.MailLabelInput{Name: "Quarter"}, &label, 201)
	folderIDs := []string{}
	for _, name := range []string{"Plans", "Reports"} {
		var folder schema.DriveFolder
		n.request(t, "POST", base+"/drive/folders", schema.FolderInput{Name: name}, &folder, 200)
		folderIDs = append(folderIDs, folder.ID)
	}
	file := func(name string, folder any, trashed bool) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, "INSERT INTO drive_file(workspace_id,folder_id,name,content_type,size,trashed) VALUES($1,$2,$3,'text/plain',4,$4) RETURNING id", w, folder, name, trashed).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	plans, reports, root, binned := file("plan.txt", folderIDs[0], false), file("report.txt", folderIDs[1], false), file("root.txt", nil, false), file("old.txt", folderIDs[0], true)
	file("other.txt", folderIDs[1], false)
	for _, id := range []string{plans, reports, root, binned} {
		n.request(t, "PATCH", base+"/resources/document/"+id+"/labels", schema.ResourceLabelInput{LabelID: label.ID, Applied: true}, nil, 204)
	}
	var listing schema.DriveListing
	n.request(t, "GET", base+"/drive", nil, &listing, 200)
	labeled := map[string]bool{}
	for _, l := range listing.Labels {
		if l.LabelID == label.ID {
			labeled[l.FileID] = true
		}
	}
	// What the label view shows: labeled files that are not in Trash, from any folder.
	view := map[string]string{}
	for _, f := range listing.Files {
		if labeled[f.ID] && !f.Trashed {
			folder := ""
			if f.FolderID != nil {
				folder = *f.FolderID
			}
			view[f.ID] = folder
		}
	}
	want := map[string]string{plans: folderIDs[0], reports: folderIDs[1], root: ""}
	if fmt.Sprint(view) != fmt.Sprint(want) {
		t.Fatalf("Drive label view %v want %v", view, want)
	}
	if !labeled[binned] {
		t.Fatal("a trashed file lost its label")
	}
}
