// Package smtpenvelope validates SMTP connection metadata supplied by the signed
// relay protocol. MIME headers are never a source of client IP or envelope data.
package smtpenvelope

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/mail"
	"strings"
)

type Envelope struct {
	IP   string `json:"ip"`
	From string `json:"from"`
	Helo string `json:"helo"`
}

func Decode(encoded string) (Envelope, error) {
	var out Envelope
	if len(encoded) == 0 || len(encoded) > 2048 {
		return out, errors.New("invalid SMTP envelope size")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return out, errors.New("invalid SMTP envelope encoding")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF {
		return out, errors.New("invalid SMTP envelope")
	}
	if net.ParseIP(out.IP) == nil || len(out.From) > 254 || len(out.Helo) == 0 || len(out.Helo) > 253 || strings.ContainsAny(out.From+out.Helo, "\r\n\x00") {
		return out, errors.New("invalid SMTP envelope fields")
	}
	if out.From != "" {
		a, e := mail.ParseAddress(out.From)
		if e != nil || a.Address != out.From || !strings.Contains(out.From, "@") {
			return out, errors.New("invalid SMTP sender")
		}
	}
	for _, r := range out.Helo {
		if r < 33 || r > 126 {
			return out, errors.New("invalid SMTP HELO")
		}
	}
	return out, nil
}
