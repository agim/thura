package mailbox

import (
	"io"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	queries "thura/db/queries/gen"
	"thura/schema"
)

var messageIDPattern = regexp.MustCompile(`<[^\s<>@]+@[^\s<>@]+>`)

func firstMessageID(value string) string {
	id := messageIDPattern.FindString(value)
	if len(id) > 500 {
		return ""
	}
	return id
}
func cleanReferences(value string) string {
	if len(value) > 8192 {
		value = value[:8192]
	}

	var ids []string
	seen := map[string]bool{}
	size := 0
	for _, id := range messageIDPattern.FindAllString(value, -1) {
		if len(id) > 500 || seen[id] {
			continue
		}
		ids = append(ids, id)
		seen[id] = true
		size += len(id) + 1
	}
	// Keep the root and newest parent when pruning a long reference chain.
	for (len(ids) > 20 || size > 2000) && len(ids) > 2 {
		size -= len(ids[1]) + 1
		ids = append(ids[:1], ids[2:]...)
	}

	return strings.Join(ids, " ")
}
func shortText(value string, max int) string {
	runes := []rune(value)
	if len(runes) > max {
		runes = runes[:max]
	}
	return string(runes)
}
func sourceDraft(box queries.Mailbox, source queries.MailItem, in schema.DraftInput) schema.DraftInput {
	reply := in.ReplyID != nil
	all := in.ReplyAll != nil && *in.ReplyAll
	if reply {
		to, cc := replyRecipients(box.Address, source, all)
		if in.To == nil {
			in.To = &to
		}
		if in.Cc == nil {
			in.Cc = &cc
		}
		thread := source.ThreadID
		if thread == "" {
			thread = source.ID
		}
		in.ThreadID = &thread
	}
	if in.Subject == nil {
		prefix := "Fwd: "
		if reply {
			prefix = "Re: "
		}
		subject := source.Subject
		if !strings.HasPrefix(strings.ToLower(subject), strings.ToLower(prefix)) {
			subject = prefix + subject
		}
		subject = shortText(subject, 500)
		in.Subject = &subject
	}
	if in.Text == nil {
		body := source.TextBody
		if body == "" {
			body = plainHTML(source.HTMLBody)
		}
		quote := "\n\nOn " + source.CreatedAt.Format(time.RFC3339) + ", " + source.FromAddress + " wrote:\n" + body
		if box.Signature != "" {
			quote = "\n\n" + box.Signature + quote
		}
		quote = shortText(quote, 500000)
		in.Text = &quote
	}
	return in
}
func replyRecipients(own string, source queries.MailItem, all bool) (string, string) {
	seen := map[string]bool{strings.ToLower(own): true}
	collect := func(value string) []string {
		parsed, err := mail.ParseAddressList(value)
		if err != nil {
			return nil
		}
		var out []string
		for _, address := range parsed {
			key := strings.ToLower(address.Address)
			if !seen[key] {
				seen[key] = true
				out = append(out, address.String())
			}
		}
		return out
	}
	outgoing := source.Status == "sent" || source.Status == "captured" || source.Status == "queued" || source.Status == "draft" || source.Status == "failed"
	primary := source.FromAddress
	if outgoing {
		primary = source.ToAddress
	}
	to := collect(primary)
	var cc []string
	if all {
		if !outgoing {
			cc = append(cc, collect(source.ToAddress)...)
		}
		cc = append(cc, collect(source.Cc)...)
	}
	return strings.Join(to, ", "), strings.Join(cc, ", ")
}

// plainHTML extracts text without executing HTML or fetching resources.
func plainHTML(value string) string {
	z := html.NewTokenizer(strings.NewReader(value))
	var out strings.Builder
	blocked := 0
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			if z.Err() == io.EOF {
				return out.String()
			}
			return out.String()
		}
		token := z.Token()
		switch kind {
		case html.StartTagToken:
			if token.Data == "script" || token.Data == "style" {
				blocked++
			}
			if blocked == 0 && (token.Data == "p" || token.Data == "div" || token.Data == "br") {
				out.WriteString("\n")
			}
		case html.EndTagToken:
			if (token.Data == "script" || token.Data == "style") && blocked > 0 {
				blocked--
			}
		case html.TextToken:
			if blocked == 0 {
				out.WriteString(token.Data)
			}
		}
	}
}
