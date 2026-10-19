// Package documentprocess runs bounded local document recognition. Source files
// are never executed, and no document-controlled path or shell command is used.
package documentprocess

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const TextLimit = 64 << 10

type bounded struct {
	bytes.Buffer
	max int
}

func (b *bounded) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, errors.New("document output exceeds limit")
	}
	return b.Buffer.Write(p)
}
func command(ctx context.Context, max int, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out := &bounded{max: max}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("local %s processing failed: %w", name, err)
	}
	return out.Bytes(), nil
}

func OCR(ctx context.Context, pdf []byte) (string, error) {
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || len(pdf) > 10<<20 {
		return "", errors.New("invalid or oversized PDF")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	// lidza:ignore L009 ephemeral worker scratch, removed before return; durable inputs and outputs use the storage pack
	dir, err := os.MkdirTemp("", "thura-pdf-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "source.pdf")
	// lidza:ignore L009 private temporary input for Poppler, never persistent document storage
	if err = os.WriteFile(input, pdf, 0600); err != nil {
		return "", err
	}
	info, err := command(ctx, 16<<10, "pdfinfo", input)
	if err != nil {
		return "", err
	}
	count := regexp.MustCompile(`(?m)^Pages:\s+(\d+)`).FindSubmatch(info)
	if len(count) != 2 {
		return "", errors.New("PDF page count unavailable")
	}
	pages, err := strconv.Atoi(string(count[1]))
	if err != nil {
		return "", errors.New("invalid PDF page count")
	}
	if pages < 1 || pages > 12 {
		return "", errors.New("scanned PDF recognition supports up to 12 pages; split this file first")
	}
	if _, err = command(ctx, 1024, "pdftoppm", "-scale-to", "1800", "-png", input, filepath.Join(dir, "page")); err != nil {
		return "", err
	}
	var out strings.Builder
	characters := 0
	for n := 1; n <= pages; n++ {
		// Poppler pads the page number to the number of digits in the page count.
		name := filepath.Join(dir, fmt.Sprintf("page-%0*d.png", len(strconv.Itoa(pages)), n))
		text, err := command(ctx, TextLimit-out.Len(), "tesseract", name, "stdout", "-l", "eng", "--psm", "3")
		if err != nil {
			return "", err
		}
		characters += len(strings.TrimSpace(string(text)))
		if out.Len()+len(text)+32 > TextLimit {
			return "", errors.New("recognized text exceeds limit; split the PDF first")
		}
		fmt.Fprintf(&out, "\n--- Page %d ---\n%s", n, text)
	}
	if characters < 10 {
		return "", errors.New("no readable text found; try a sharper scan")
	}
	return out.String(), nil
}

// Word creates a genuine editable DOCX from reviewed text, not page images.
func Word(text string) ([]byte, error) {
	if strings.TrimSpace(text) == "" || len(text) > TextLimit {
		return nil, errors.New("Word text is empty or exceeds limit")
	}
	var body strings.Builder
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		body.WriteString(`<w:p><w:r><w:t xml:space="preserve">`)
		if err := xml.EscapeText(&body, []byte(line)); err != nil {
			return nil, err
		}
		body.WriteString(`</w:t></w:r></w:p>`)
	}
	files := map[string]string{
		"[Content_Types].xml": `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":         `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml":   `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() + `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body></w:document>`,
	}
	var out bytes.Buffer
	archive := zip.NewWriter(&out)
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml"} {
		entry, err := archive.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err = entry.Write([]byte(files[name])); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
