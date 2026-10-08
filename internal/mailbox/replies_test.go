package mailbox

import (
	"fmt"
	"strings"
	"testing"
	q "thura/db/queries/gen"
	"thura/schema"
)

func TestReplyRecipientsExcludeSelfDeduplicateAndNeverCopyBcc(t *testing.T) {
	source := q.MailItem{FromAddress: `"Sender, One" <sender@example.com>`, ToAddress: "team@example.com, other@example.com, SENDER@example.com", Cc: "other@example.com, third@example.com", Bcc: "secret@example.com", Status: "received"}
	to, cc := replyRecipients("team@example.com", source, true)
	if to != `"Sender, One" <sender@example.com>` || cc != "<other@example.com>, <third@example.com>" {
		t.Fatalf("unexpected reply recipients: %s / %s", to, cc)
	}
	if strings.Contains(to+cc, "secret") || strings.Contains(to+cc, "team@") {
		t.Fatal("reply leaked hidden or own address")
	}
	source.Status = "sent"
	source.Folder = q.MailFolderArchive
	to, cc = replyRecipients("team@example.com", source, false)
	if !strings.Contains(to, "other@example.com") || strings.Contains(to, "team@") || cc != "" {
		t.Fatal("archived outgoing message used the sender as recipient")
	}
}
func TestReplyDefaultsKeepThreadAndPlainQuote(t *testing.T) {
	source := q.MailItem{ID: "source", ThreadID: "root", Subject: "Re: Hello", HTMLBody: "<p>Hello &amp; welcome</p><script>secret script</script><style>hidden</style>", Status: "received", FromAddress: "sender@example.com"}
	id := source.ID
	in := sourceDraft(q.Mailbox{Address: "team@example.com", Signature: "Team"}, source, schema.DraftInput{ReplyID: &id})
	if Text(in.Subject) != "Re: Hello" || Text(in.ThreadID) != "root" || !strings.Contains(Text(in.Text), "Team") || !strings.Contains(Text(in.Text), "Hello & welcome") || strings.Contains(Text(in.Text), "secret script") || strings.Contains(Text(in.Text), "hidden") {
		t.Fatal("reply defaults lost thread/signature or retained active HTML")
	}
	if got := cleanReferences("<a@example.com>\r\n<bad> <a@example.com> <b@example.com>"); got != "<a@example.com> <b@example.com>" {
		t.Fatalf("unsafe references: %q", got)
	}
	if got := firstMessageID("\r\nInjected: anything"); got != "" {
		t.Fatal("accepted invalid message ID")
	}
}

func TestLongReferencesKeepRootAndNewestParent(t *testing.T) {
	var chain strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&chain, "<%d@example.com> ", i)
	}
	cleaned := cleanReferences(chain.String())
	if !strings.HasPrefix(cleaned, "<0@example.com>") || !strings.HasSuffix(cleaned, "<59@example.com>") || len(cleaned) > 2000 || len(strings.Fields(cleaned)) > 20 {
		t.Fatal("bounded reference chain lost root or newest parent")
	}
}
