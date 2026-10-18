// Package documenttext extracts bounded plain text without executing document code.
package documenttext

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"golang.org/x/net/html"
	"io"
	"mime"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

const Limit = 2 << 20

var ErrUnsupported = errors.New("document format is not searchable")
var ErrLimit = errors.New("document exceeds text extraction limit")

func Extract(ctx context.Context, data []byte, contentType string) (string, error) {
	kind, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", ErrUnsupported
	}
	switch kind {
	case "text/plain", "text/markdown", "text/csv", "text/tab-separated-values", "application/json", "application/xml", "text/xml":
		if len(data) > Limit {
			return "", ErrLimit
		}
		if !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
			return "", ErrUnsupported
		}
		return string(data), nil
	case "text/html":
		if len(data) > Limit {
			return "", ErrLimit
		}
		z := html.NewTokenizer(bytes.NewReader(data))
		var out strings.Builder
		skip := false
		for {
			switch z.Next() {
			case html.ErrorToken:
				if z.Err() != io.EOF {
					return "", z.Err()
				}
				return out.String(), nil
			case html.StartTagToken:
				n, _ := z.TagName()
				if string(n) == "script" || string(n) == "style" {
					skip = true
				}
			case html.EndTagToken:
				n, _ := z.TagName()
				if string(n) == "script" || string(n) == "style" {
					skip = false
				}
				out.WriteByte(' ')
			case html.TextToken:
				if !skip {
					out.Write(z.Text())
				}
			}
		}
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "application/vnd.openxmlformats-officedocument.presentationml.presentation", "application/vnd.oasis.opendocument.text", "application/vnd.oasis.opendocument.spreadsheet", "application/vnd.oasis.opendocument.presentation":
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", err
		}
		if len(archive.File) > 4096 {
			return "", ErrLimit
		}
		var out strings.Builder
		remaining := int64(Limit)
		found := false
		for _, f := range archive.File {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			name := f.Name
			wanted := name == "content.xml" || name == "word/document.xml" || strings.HasPrefix(name, "word/header") || strings.HasPrefix(name, "word/footer") || name == "word/footnotes.xml" || name == "word/endnotes.xml" || name == "xl/sharedStrings.xml" || strings.HasPrefix(name, "xl/worksheets/") || strings.HasPrefix(name, "ppt/slides/slide") || strings.HasPrefix(name, "ppt/notesSlides/notesSlide")
			if !wanted || !strings.HasSuffix(name, ".xml") {
				continue
			}
			found = true
			if f.UncompressedSize64 > uint64(remaining) {
				return "", ErrLimit
			}
			r, err := f.Open()
			if err != nil {
				return "", err
			}
			raw, err := io.ReadAll(io.LimitReader(r, remaining+1))
			closeErr := r.Close()
			if err != nil {
				return "", err
			}
			if closeErr != nil {
				return "", closeErr
			}
			remaining -= int64(len(raw))
			if remaining < 0 {
				return "", ErrLimit
			}
			d := xml.NewDecoder(bytes.NewReader(raw))
			for {
				token, err := d.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					return "", err
				}
				switch t := token.(type) {
				case xml.CharData:
					out.Write(t)
				case xml.EndElement:
					switch t.Name.Local {
					case "p", "row", "c", "si", "table-cell":
						out.WriteByte('\n')
					}
				}
			}
			out.WriteByte('\n')
		}
		if !found {
			return "", ErrUnsupported
		}
		return out.String(), nil
	case "application/pdf":
		// Poppler is optional. Execute a fixed binary, never a document-supplied command.
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "pdftotext", "-enc", "UTF-8", "-", "-")
		cmd.Stdin = bytes.NewReader(data)
		out := &boundedWriter{}
		cmd.Stdout = out
		if err := cmd.Run(); err != nil {
			return "", errors.New("PDF text extraction unavailable or failed")
		}
		return out.String(), nil
	default:
		return "", ErrUnsupported
	}
}

type boundedWriter struct{ strings.Builder }

func (b *boundedWriter) Write(p []byte) (int, error) {
	if b.Len()+len(p) > Limit {
		return 0, ErrLimit
	}
	return b.Builder.Write(p)
}
