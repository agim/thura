package documentprocess

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestReviewedWordContainsEditableEscapedText(t *testing.T) {
	text := "Delivery note\nQuantity: 12\nTotal: $1,500.00\nA < B & \"quoted\""
	data, err := Word(text)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var body []byte
	for _, f := range archive.File {
		if f.Name == "word/document.xml" {
			r, e := f.Open()
			if e != nil {
				t.Fatal(e)
			}
			body, e = io.ReadAll(r)
			if e != nil {
				t.Fatal(e)
			}
			if e = r.Close(); e != nil {
				t.Fatal(e)
			}
		}
	}
	if len(body) == 0 || !bytes.Contains(body, []byte("$1,500.00")) || bytes.Contains(body, []byte("<w:drawing")) {
		t.Fatal("Word output lost editable text or became an image")
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	var actual strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if c, ok := token.(xml.CharData); ok {
			actual.Write(c)
		}
	}
	if !strings.Contains(actual.String(), `A < B & "quoted"`) {
		t.Fatal("text escaping changed source content")
	}
	if _, err = Word(strings.Repeat("a", TextLimit+1)); err == nil {
		t.Fatal("oversized text accepted")
	}
	if _, err = Word("  "); err == nil {
		t.Fatal("empty document accepted")
	}
}
func TestOCRRejectsInvalidInputsBeforeRunningTools(t *testing.T) {
	if _, err := OCR(context.Background(), []byte("<script>not a PDF</script>")); err == nil {
		t.Fatal("invalid PDF accepted")
	}
	if _, err := OCR(context.Background(), append([]byte("%PDF-"), make([]byte, 10<<20)...)); err == nil {
		t.Fatal("oversized PDF accepted")
	}
}
