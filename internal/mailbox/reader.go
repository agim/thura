package mailbox

import (
	"context"
	"encoding/base64"
	"errors"
	"html"
	"io"
	"mime"
	"net/url"
	"strings"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/packs/storage"
	"github.com/agim/lidza/pkg/router"
	"github.com/jackc/pgx/v5"
	xhtml "golang.org/x/net/html"
	q "thura/db/queries/gen"
	"thura/internal/platform/safeimage"
	"thura/internal/workspace"
	"thura/schema"
)

func readerTag(name string) bool {
	switch name {
	case "p", "br", "div", "span", "strong", "em", "b", "i", "u", "s", "blockquote", "ul", "ol", "li", "pre", "code", "h1", "h2", "h3", "h4", "table", "thead", "tbody", "tfoot", "tr", "td", "th":
		return true
	default:
		return false
	}
}
func readerBlocked(name string) bool {
	switch name {
	case "head", "script", "style", "iframe", "object", "embed", "svg", "math", "template", "form", "input", "button", "textarea", "select", "video", "audio":
		return true
	default:
		return false
	}
}

func ContentID(value string) string {
	value = strings.Trim(strings.TrimSpace(value), "<>")
	if value == "" || len(value) > 200 {
		return ""
	}
	for _, r := range value {
		if r <= ' ' || r >= 127 || strings.ContainsRune("<>\"'\\&", r) {
			return ""
		}
	}
	return value
}

// ReaderHTML is the authoritative API reader policy, shared by list/detail/
// conversation responses. Raw MIME and stored HTML remain unchanged. No
// original URL, CSS, event handler or navigation attribute reaches the reader.
func ReaderHTML(source string) string {
	doc, err := xhtml.Parse(strings.NewReader(source))
	if err != nil {
		return ""
	}
	var out strings.Builder
	var render func(*xhtml.Node, int)
	render = func(n *xhtml.Node, depth int) {
		if depth > 128 || n.Type == xhtml.ElementNode && readerBlocked(n.Data) {
			return
		}
		if n.Type == xhtml.TextNode {
			out.WriteString(html.EscapeString(n.Data))
			return
		}
		if n.Type == xhtml.ElementNode && n.Data == "img" {
			var cid, alt string
			for _, a := range n.Attr {
				if a.Key == "src" && strings.HasPrefix(strings.ToLower(a.Val), "cid:") {
					if decoded, err := url.PathUnescape(a.Val[4:]); err == nil {
						cid = ContentID(decoded)
					}
				}
				if a.Key == "alt" && len(a.Val) <= 200 {
					alt = a.Val
				}
			}
			if cid != "" {
				out.WriteString(`<img src="cid:` + html.EscapeString(cid) + `" alt="` + html.EscapeString(alt) + `">`)
			}
			return
		}
		allowed := n.Type == xhtml.ElementNode && readerTag(n.Data)
		if allowed {
			out.WriteString("<" + n.Data + ">")
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			render(child, depth+1)
		}
		if allowed && n.Data != "br" {
			out.WriteString("</" + n.Data + ">")
		}
	}
	render(doc, 0)
	return out.String()
}

// InlineImage grants only an attachment of this authorized message. The
// original remains downloadable; unsupported images cannot become active HTML.
func InlineImage(ctx context.Context, w, box, item, attachment string) (schema.FileContent, error) {
	if _, err := ItemAccess(ctx, w, box, item); err != nil {
		return schema.FileContent{}, err
	}
	if !workspace.ValidID(attachment) {
		return schema.FileContent{}, router.Errorf(404, "attachment not found")
	}
	a, err := q.New(db.From(ctx)).GetMailAttachment(ctx, attachment)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && a.ItemID != item {
		return schema.FileContent{}, router.Errorf(404, "attachment not found")
	}
	if err != nil {
		return schema.FileContent{}, err
	}
	if ContentID(a.ContentID) == "" {
		return schema.FileContent{}, router.Errorf(422, "attachment is not an embedded image")
	}
	contentType, _, err := mime.ParseMediaType(a.ContentType)
	if err != nil || contentType != "image/png" && contentType != "image/jpeg" && contentType != "image/gif" {
		return schema.FileContent{}, router.Errorf(422, "embedded image format is unsupported")
	}
	r, _, err := storage.From(ctx).Get(ctx, a.ObjectKey)
	if err != nil {
		return schema.FileContent{}, err
	}
	b, readErr := io.ReadAll(io.LimitReader(r, (10<<20)+1))
	if err = errors.Join(readErr, r.Close()); err != nil {
		return schema.FileContent{}, err
	}
	if len(b) > 10<<20 || len(b) != int(a.Size) {
		return schema.FileContent{}, router.Errorf(422, "embedded image size is invalid")
	}
	image, supported := safeimage.Convert(b, contentType)
	if !supported {
		return schema.FileContent{}, router.Errorf(422, "embedded image cannot be safely displayed")
	}
	return schema.FileContent{Name: a.Name, ContentType: "image/png", Data: base64.StdEncoding.EncodeToString(image.Data)}, nil
}
