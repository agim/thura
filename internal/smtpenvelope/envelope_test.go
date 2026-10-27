package smtpenvelope

import (
	"encoding/base64"
	"testing"
)

func TestSMTPMetadataMustBeCanonicalAndBounded(t *testing.T) {
	encode := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	valid := `{"ip":"203.0.113.5","from":"sender@example.com","helo":"mail.example.com"}`
	got, err := Decode(encode(valid))
	if err != nil || got.IP != "203.0.113.5" || got.From != "sender@example.com" {
		t.Fatalf("valid envelope: %+v %v", got, err)
	}
	for _, raw := range []string{`{}`, `{"ip":"hostname.example","from":"x@y.example","helo":"mail.example"}`, `{"ip":"1.2.3.4","from":"Name <x@y.example>","helo":"mail.example"}`, `{"ip":"1.2.3.4","from":"x@y.example","helo":"x\r\nUser: admin"}`, `{"ip":"1.2.3.4","from":"x@y.example","helo":"mail.example","authenticated":true}`, valid + ` {}`} {
		if _, err := Decode(encode(raw)); err == nil {
			t.Errorf("accepted invalid SMTP evidence %q", raw)
		}
	}
	if _, err := Decode("not-base64!"); err == nil {
		t.Fatal("accepted malformed base64")
	}
	if _, err := Decode(encode(`{"ip":"::1","from":"","helo":"[::1]"}`)); err != nil {
		t.Fatal("null reverse path and literal HELO should work", err)
	}
}
