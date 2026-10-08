package mailbox

import (
	xhtml "golang.org/x/net/html"
	"strings"
	"testing"
)

func TestReaderHTMLAllowsOnlyFormattingAndCIDImages(t *testing.T) {
	for _, source := range []string{
		`<p style="background:url(https://tracking.invalid)" onclick="alert(1)">Safe body</p><script>alert(1)</script><style>@import 'https://tracking.invalid'</style><img src="https://tracking.invalid"><a href="javascript:alert(1)">Visible link text</a><img src="cid:logo%40local" alt="Logo" onerror="alert(1)">`,
		`<meta http-equiv="refresh" content="0;url=https://tracking.invalid"><base href="https://tracking.invalid"><svg><a xlink:href="javascript:alert(1)">SVG content</a></svg><math><mtext><img src="https://tracking.invalid"></mtext></math><p>Safe body</p><img src="CID:logo@local">`,
		`<iframe srcdoc="<script>alert(1)</script>"></iframe><form action="https://tracking.invalid"><input name="secret"></form><p>Safe body</p><img src="data:image/svg+xml,evil"><img src="cid:logo@local"><template><script>alert(1)</script></template>`,
	} {
		clean := ReaderHTML(source)
		if !strings.Contains(clean, "Safe body") || !strings.Contains(clean, `src="cid:logo@local"`) {
			t.Fatalf("lost safe content: %s", clean)
		}
		doc, err := xhtml.Parse(strings.NewReader(clean))
		if err != nil {
			t.Fatal(err)
		}
		var check func(*xhtml.Node)
		check = func(n *xhtml.Node) {
			if n.Type == xhtml.ElementNode {
				if !readerTag(n.Data) && n.Data != "img" && n.Data != "html" && n.Data != "head" && n.Data != "body" {
					t.Fatalf("unsafe tag %s", n.Data)
				}
				for _, a := range n.Attr {
					if n.Data != "img" || a.Key != "src" && a.Key != "alt" || a.Key == "src" && a.Val != "cid:logo@local" {
						t.Fatalf("unsafe attribute %s=%s", a.Key, a.Val)
					}
				}
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				check(child)
			}
		}
		check(doc)
		if strings.Contains(clean, "alert(1)") || strings.Contains(clean, "tracking.invalid") {
			t.Fatalf("retained active source: %s", clean)
		}
	}
	for _, cid := range []string{"", "bad cid", "a\n@b", "a\"onload=evil", strings.Repeat("a", 201)} {
		if ContentID(cid) != "" {
			t.Fatalf("accepted invalid CID %q", cid)
		}
	}
}
