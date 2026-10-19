// Package richtext applies a small authored-content policy independent of the
// stricter inbound email reader. It never keeps images, CSS or active content.
package richtext

import (
	xhtml "golang.org/x/net/html"
	"html"
	"net/url"
	"strings"
)

func Clean(source string) string {
	doc, err := xhtml.Parse(strings.NewReader(source))
	if err != nil {
		return ""
	}
	var out strings.Builder
	var render func(*xhtml.Node, int)
	render = func(n *xhtml.Node, depth int) {
		if depth > 100 {
			return
		}
		if n.Type == xhtml.TextNode {
			out.WriteString(html.EscapeString(n.Data))
			return
		}
		switch n.Data {
		case "script", "style", "iframe", "object", "svg", "math", "template", "form", "head":
			return
		}
		allowed := strings.Contains("|p|br|strong|b|em|i|u|s|ul|ol|li|a|blockquote|", "|"+n.Data+"|") && n.Type == xhtml.ElementNode
		if allowed {
			out.WriteString("<" + n.Data)
			if n.Data == "a" {
				for _, a := range n.Attr {
					if a.Key == "href" {
						target, err := url.Parse(strings.TrimSpace(a.Val))
						if err == nil && (target.Scheme == "https" || target.Scheme == "http" || target.Scheme == "mailto") {
							out.WriteString(` href="` + html.EscapeString(target.String()) + `" rel="noopener noreferrer"`)
						}
					}
				}
			}
			out.WriteString(">")
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			render(c, depth+1)
		}
		if allowed && n.Data != "br" {
			out.WriteString("</" + n.Data + ">")
		}
	}
	render(doc, 0)
	return out.String()
}
