package smipmail

import (
	"net/mail"
	"strings"
	"testing"
)

// A peer's From may carry a display name, but only as one address whose
// decoded name has no control characters; the MIME copy re-encodes it.
func TestFromDisplayNameFromPeer(t *testing.T) {
	named := (&mail.Address{Name: "Ada Lovelace, Nörthwind", Address: "info@Northwind.example"}).String()
	sender, err := plainAddress(named)
	if err != nil || sender != "info@northwind.example" {
		t.Fatalf("named sender %q: %v", sender, err)
	}
	header, err := fromHeader(named)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := mail.ParseAddress(header)
	if err != nil || parsed.Name != "Ada Lovelace, Nörthwind" || parsed.Address != "info@northwind.example" || strings.ContainsAny(header, "\r\n") {
		t.Fatalf("MIME From %q: %+v %v", header, parsed, err)
	}
	if header, err = fromHeader("info@northwind.example"); err != nil || header != "info@northwind.example" {
		t.Fatalf("bare From became %q: %v", header, err)
	}
	for _, raw := range []string{
		"Info\r\nBcc: victim@example.com <info@northwind.example>",
		"=?utf-8?q?Info=0D=0ABcc:_victim@example.com?= <info@northwind.example>",
		"=?utf-8?q?Info=00?= <info@northwind.example>",
		"Info <info@northwind.example>, other@example.com",
	} {
		if _, err := plainAddress(raw); err == nil {
			t.Errorf("accepted From %q", raw)
		}
		if _, err := fromHeader(raw); err == nil {
			t.Errorf("MIME accepted From %q", raw)
		}
	}
}
