package documenttext

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestExtractOfficeRunsAndLimits(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("word/document.xml")
	w.Write([]byte(`<document><p><r><t>fiber </t></r><r><t>renewal</t></r></p><p>Second paragraph</p></document>`))
	z.Close()
	text, err := Extract(context.Background(), b.Bytes(), "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	if err != nil || !strings.Contains(text, "fiber renewal\nSecond paragraph") {
		t.Fatalf("%q %v", text, err)
	}
	b.Reset()
	z = zip.NewWriter(&b)
	w, _ = z.Create("word/document.xml")
	w.Write(bytes.Repeat([]byte("a"), Limit+1))
	z.Close()
	if _, err := Extract(context.Background(), b.Bytes(), "application/vnd.openxmlformats-officedocument.wordprocessingml.document"); !errors.Is(err, ErrLimit) {
		t.Fatalf("zip expansion: %v", err)
	}
	for _, in := range []struct {
		mime string
		data []byte
	}{{"image/png", []byte("secret")}, {"text/plain", []byte{0xff, 0}}, {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", []byte("broken")}} {
		if _, err := Extract(context.Background(), in.data, in.mime); err == nil {
			t.Fatal("accepted invalid or unsupported document")
		}
	}
	html, err := Extract(context.Background(), []byte(`<p>Visible &amp; safe</p><script>secret</script><style>hidden</style>`), "text/html")
	if err != nil || strings.Contains(html, "secret") || strings.Contains(html, "hidden") || !strings.Contains(html, "Visible & safe") {
		t.Fatalf("HTML: %q %v", html, err)
	}
}
