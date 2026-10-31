package mailabuse

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
	"thura/internal/assistant"
	"thura/schema"
)

// aiEvidence is the JSON the screening model reads. It always fits within
// assistant.MailScreeningInputLimit: the text body comes first, the HTML part
// only as its visible text and link targets (never its markup) and only when it
// adds something, and long bodies keep their beginning and a short tail.
// Truncated with the original sizes tells the model the evidence is partial.
type aiEvidence struct {
	From, Subject        string
	Text                 string
	HTMLVisibleText      string   `json:",omitempty"`
	HTMLLinks            []string `json:",omitempty"`
	Truncated            bool
	OriginalTextBytes    int
	OriginalHTMLBytes    int
	SenderAuthentication senderEvidence
	Files                []string
	Findings             []schema.MailFinding
}

const (
	evidenceMaxLinks    = 100
	evidenceLinkBytes   = 1000
	evidenceLinksBudget = 8000
	evidenceTail        = 4
)

// buildAIEvidence returns the model's evidence JSON within the input limit.
func buildAIEvidence(from, subject, text, html string, auth senderEvidence, files []string, findings []schema.MailFinding) (string, error) {
	limit := assistant.MailScreeningInputLimit
	e := aiEvidence{From: clip(from, 1000), Subject: clip(subject, 2000), OriginalTextBytes: len(text), OriginalHTMLBytes: len(html), SenderAuthentication: auth, Files: []string{}, Findings: findings}
	if e.Findings == nil {
		e.Findings = []schema.MailFinding{}
	}
	for i, name := range files {
		if i == 100 {
			e.Truncated = true
			break
		}
		e.Files = append(e.Files, clip(name, 300))
	}
	visible, links := htmlEvidence(html)
	if addsText(text, visible) {
		e.HTMLVisibleText = visible
	}
	used := 0
	for _, link := range links {
		if strings.Contains(text, link) {
			continue
		}
		link = clip(link, evidenceLinkBytes)
		if len(e.HTMLLinks) == evidenceMaxLinks || used+len(link) > evidenceLinksBudget {
			e.Truncated = true
			break
		}
		e.HTMLLinks = append(e.HTMLLinks, link)
		used += len(link)
	}
	fullText, fullHTML := text, e.HTMLVisibleText
	e.Text, e.HTMLVisibleText = "", ""
	base, err := encodeEvidence(e)
	if err != nil {
		return "", err
	}
	// Room for the bodies, less a margin for JSON escaping and the truncation
	// marker; the loop below shrinks further if escaping still overflows.
	room := limit - len(base) - 256
	for attempt := 0; attempt < 8; attempt++ {
		textRoom, htmlRoom := split(room, len(fullText), len(fullHTML))
		e.Text, e.HTMLVisibleText = shorten(fullText, textRoom), shorten(fullHTML, htmlRoom)
		truncated := e.Truncated || len(e.Text) < len(fullText) || len(e.HTMLVisibleText) < len(fullHTML)
		candidate := e
		candidate.Truncated = truncated
		out, err := encodeEvidence(candidate)
		if err != nil {
			return "", err
		}
		if len(out) <= limit {
			return out, nil
		}
		room -= len(out) - limit + room/8
	}
	// Unreachable in practice; ScreenMail refuses an oversized input anyway.
	e.Text, e.HTMLVisibleText, e.Truncated = "", "", true
	return encodeEvidence(e)
}

// split shares the body room: the text body first, the HTML's visible text
// with what remains, but never less than a quarter when both are long.
func split(room, text, html int) (int, int) {
	if room < 0 {
		room = 0
	}
	if text+html <= room {
		return text, html
	}
	if html == 0 {
		return room, 0
	}
	htmlRoom := room / 4
	if room-text > htmlRoom {
		htmlRoom = room - text
	}
	if htmlRoom > html {
		htmlRoom = html
	}
	return room - htmlRoom, htmlRoom
}

// shorten keeps the beginning and a short tail of s within max bytes, marking
// what was left out; it never splits a UTF-8 sequence.
func shorten(s string, max int) string {
	if len(s) <= max {
		return s
	}
	marker := "\n[… " + strconv.Itoa(len(s)) + " bytes in full; middle omitted …]\n"
	if max <= len(marker)+16 {
		return clip(s, max)
	}
	keep := max - len(marker)
	tail := keep / evidenceTail
	head := clip(s, keep-tail)
	end := len(s) - tail
	for end < len(s) && !utf8.RuneStart(s[end]) {
		end++
	}
	return head + marker + s[end:]
}

// clip cuts s to at most max bytes at a rune boundary.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}

// addsText reports whether the HTML's visible text says something the text
// body does not: more than a tenth of its words are missing from the text.
func addsText(text, visible string) bool {
	known := map[string]bool{}
	for _, word := range strings.Fields(text) {
		known[normalized(word)] = true
	}
	words, missing := 0, 0
	for _, word := range strings.Fields(visible) {
		if word = normalized(word); word != "" {
			words++
			if !known[word] {
				missing++
			}
		}
	}
	return missing*10 > words
}

func compact(s string) string { return strings.Join(strings.Fields(s), " ") }

func encodeEvidence(e aiEvidence) (string, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	// Markup-like characters stay as written instead of six-byte escapes.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(e); err != nil {
		return "", err
	}
	return strings.TrimSuffix(out.String(), "\n"), nil
}

// htmlEvidence reduces an HTML part to its visible text and its distinct link
// targets; scripts, styles, comments and attributes are dropped.
func htmlEvidence(html string) (string, []string) {
	if strings.TrimSpace(html) == "" {
		return "", nil
	}
	doc, err := xhtml.Parse(strings.NewReader(html))
	if err != nil {
		return "", nil
	}
	var text strings.Builder
	links, seen := []string{}, map[string]bool{}
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			switch n.Data {
			case "script", "style", "head", "noscript", "template", "title":
				return
			case "a", "area":
				for _, a := range n.Attr {
					if href := strings.TrimSpace(a.Val); a.Key == "href" && href != "" && !seen[href] {
						seen[href] = true
						links = append(links, href)
					}
				}
			case "br", "p", "div", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6", "table", "blockquote":
				text.WriteString("\n")
			}
		}
		if n.Type == xhtml.TextNode {
			if words := compact(n.Data); words != "" {
				text.WriteString(words)
				text.WriteString(" ")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	lines := []string{}
	for _, line := range strings.Split(text.String(), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n"), links
}
