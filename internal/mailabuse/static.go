package mailabuse

import (
	"archive/zip"
	"bytes"
	"net"
	"net/mail"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
	"thura/schema"
)

type Person struct{ Name, Email string }
type File struct {
	Name, ContentType string
	Data              []byte
}
type Evidence struct {
	Score    int
	Findings []schema.MailFinding
	Links    []string
}

func (e *Evidence) add(code, severity, detail string, score int) {
	for _, f := range e.Findings {
		if f.Code == code && f.Detail == detail {
			return
		}
	}
	if len(e.Findings) < 100 {
		e.Findings = append(e.Findings, schema.MailFinding{Code: code, Severity: severity, Detail: detail})
	}
	if score > e.Score {
		e.Score = score
	}
}
func address(raw string) (string, string, string) {
	a, err := mail.ParseAddress(raw)
	if err != nil {
		return "", "", ""
	}
	email := strings.ToLower(a.Address)
	_, domain, _ := strings.Cut(email, "@")
	ascii, err := idna.Lookup.ToASCII(domain)
	if err != nil {
		return "", "", ""
	}
	local, _, ok := strings.Cut(email, "@")
	if !ok {
		return "", "", ""
	}
	ascii = strings.TrimSuffix(ascii, ".")
	return local + "@" + ascii, ascii, a.Name
}
func identity(sender string, authenticated bool) string {
	if authenticated {
		return "smip:" + sender
	}
	return "unverified:" + sender
}
func normalized(raw string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, raw)
}
func domain(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return ""
	}
	return ascii
}
func registrable(host string) string {
	d, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return host
	}
	return d
}

var urlPattern = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)

func scanURL(e *Evidence, raw, display string) {
	raw = strings.TrimRight(strings.TrimSpace(raw), ".,;)")
	u, err := url.Parse(raw)
	if err != nil {
		e.add("invalid_link", "warning", "Malformed link", 40)
		return
	}
	if u.Scheme == "mailto" || raw == "" || strings.HasPrefix(raw, "#") {
		return
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		e.add("unsafe_link_scheme", "danger", "Link uses an unsafe or unsupported scheme", 90)
		return
	}
	host := domain(raw)
	if host == "" {
		e.add("invalid_link", "warning", "Link has no valid hostname", 60)
		return
	}
	if len(e.Links) < 100 {
		e.Links = append(e.Links, raw)
	} else {
		e.add("too_many_links", "danger", "More than 100 links require manual review", 80)
	}
	if u.User != nil {
		e.add("link_credentials", "danger", "Link disguises its destination with a username", 90)
	}
	if ip := net.ParseIP(host); ip != nil {
		risk := 60
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || !ip.IsGlobalUnicast() {
			risk = 90
		}
		e.add("ip_link", "warning", "Link points directly to an IP address", risk)
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		e.add("private_link", "danger", "Link targets a local or internal hostname", 90)
	}
	if strings.Contains(host, "xn--") {
		e.add("international_domain", "warning", "Internationalized link hostname: "+host, 40)
	}
	if u.Scheme == "http" {
		e.add("unencrypted_link", "warning", "Link uses unencrypted HTTP", 30)
	}
	for _, short := range []string{"bit.ly", "tinyurl.com", "t.co", "shorturl.at"} {
		if host == short {
			e.add("shortened_link", "warning", "Shortened link conceals its final destination", 40)
		}
	}
	shown := strings.TrimSpace(display)
	if strings.HasPrefix(shown, "www.") {
		shown = "https://" + shown
	}
	if matches := urlPattern.FindString(shown); matches != "" && registrable(domain(matches)) != registrable(host) {
		e.add("misleading_link", "danger", "Displayed link domain differs from its destination", 90)
	}
}
func textOf(n *xhtml.Node) string {
	var out strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			out.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out.String()
}
func Analyze(from, text, html string, people []Person, files []File) Evidence {
	e := Evidence{Findings: []schema.MailFinding{}, Links: []string{}}
	sender, senderDomain, name := address(from)
	if sender == "" {
		e.add("invalid_sender", "danger", "Sender address is invalid or ambiguous", 90)
	}
	for _, p := range people {
		known, knownDomain, _ := address(p.Email)
		if known == "" || known == sender {
			continue
		}
		if normalized(name) != "" && skeleton(normalized(name)) == skeleton(normalized(p.Name)) {
			e.add("name_impersonation", "danger", "Display name matches a workspace contact or member, but the address differs", 85)
		}
		a, b := registrable(senderDomain), registrable(knownDomain)
		if a != "" && a != b && (oneEdit(a, b) || skeleton(a) == skeleton(b)) {
			e.add("lookalike_sender", "danger", "Sender domain resembles a known workspace domain", 80)
		}
	}
	if strings.Contains(name, "@") {
		claimed, _, _ := address(name)
		if claimed != "" && claimed != sender {
			e.add("sender_display_address", "danger", "Display name presents a different email address", 90)
		}
	}
	for _, link := range urlPattern.FindAllString(text, 101) {
		scanURL(&e, link, "")
	}
	if doc, err := xhtml.Parse(strings.NewReader(html)); err == nil {
		var walk func(*xhtml.Node)
		walk = func(n *xhtml.Node) {
			if n.Type == xhtml.ElementNode && n.Data == "a" {
				for _, a := range n.Attr {
					if a.Key == "href" {
						scanURL(&e, a.Val, textOf(n))
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(doc)
	}
	for _, file := range files {
		scanFile(&e, file)
	}
	return e
}
func oneEdit(a, b string) bool {
	if a == b || len(a) < 4 || len(b) < 4 || len(a)-len(b) > 1 || len(b)-len(a) > 1 {
		return false
	}
	i, j, d := 0, 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			i++
			j++
			continue
		}
		d++
		if d > 1 {
			return false
		}
		if len(a) >= len(b) {
			i++
		}
		if len(b) >= len(a) {
			j++
		}
	}
	return d+(len(a)-i)+(len(b)-j) == 1
}
func dangerousName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".exe", ".scr", ".com", ".bat", ".cmd", ".ps1", ".js", ".jse", ".vbs", ".vbe", ".wsf", ".hta", ".lnk", ".jar", ".iso", ".img":
		return true
	}
	return false
}
func scanFile(e *Evidence, file File) {
	name := file.Name
	lower := strings.ToLower(name)
	data := file.Data
	if dangerousName(name) {
		e.add("dangerous_attachment", "danger", "Executable or script attachment: "+name, 100)
	}
	if bytes.HasPrefix(data, []byte("MZ")) || bytes.HasPrefix(data, []byte{0x7f, 'E', 'L', 'F'}) {
		e.add("executable_content", "danger", "Attachment contains executable content: "+name, 100)
	}
	if strings.ContainsAny(name, "\u202e\u2066\u2067\u2068\u2069") {
		e.add("filename_direction", "danger", "Attachment uses misleading text direction: "+name, 90)
	}
	if strings.HasSuffix(lower, ".docm") || strings.HasSuffix(lower, ".xlsm") || strings.HasSuffix(lower, ".pptm") {
		e.add("office_macros", "danger", "Macro-enabled Office attachment: "+name, 80)
	}
	if strings.HasSuffix(lower, ".pdf") && (bytes.Contains(data, []byte("/JavaScript")) || bytes.Contains(data, []byte("/Launch"))) {
		e.add("active_pdf", "danger", "PDF contains active JavaScript or launch instructions: "+name, 90)
	}
	if bytes.HasPrefix(data, []byte("PK")) {
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			e.add("unreadable_archive", "danger", "Archive cannot be inspected: "+name, 80)
			return
		}
		var total uint64
		for i, entry := range archive.File {
			total += entry.UncompressedSize64
			if i >= 512 || total > 50<<20 || entry.UncompressedSize64 > uint64(len(data)+1)*100 {
				e.add("archive_limits", "danger", "Archive exceeds safe inspection limits: "+name, 100)
				break
			}
			if entry.Flags&1 != 0 {
				e.add("encrypted_archive", "danger", "Encrypted archive cannot be fully scanned: "+name, 90)
			}
			if dangerousName(entry.Name) || strings.HasSuffix(strings.ToLower(entry.Name), "vbaproject.bin") {
				e.add("active_archive_content", "danger", "Archive contains scripts, executables or Office macros: "+name, 90)
			}
		}
	}
}

func skeleton(value string) string {
	decoded, err := idna.Lookup.ToUnicode(value)
	if err == nil {
		value = decoded
	}
	replacer := strings.NewReplacer("rn", "m", "vv", "w", "а", "a", "е", "e", "о", "o", "р", "p", "с", "c", "х", "x", "і", "i", "ј", "j", "ѕ", "s", "ο", "o", "ρ", "p")
	return replacer.Replace(value)
}
