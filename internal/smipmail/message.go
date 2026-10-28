package smipmail

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	mailpack "github.com/agim/lidza/packs/mail"
	"thura/internal/smip"
)

type Payload struct {
	MessageID string           `json:"messageId"`
	Created   int64            `json:"created"`
	Message   mailpack.Message `json:"message"`
}

func plainAddress(raw string) (string, error) {
	address, err := mail.ParseAddress(raw)
	// A display name is checked after decoding too: an encoded word can carry
	// a CR or LF that the raw text does not show.
	if err != nil || strings.ContainsAny(raw, "\r\n\x00") || strings.ContainsFunc(address.Name, unicode.IsControl) {
		return "", errors.New("invalid email address")
	}
	local, domain, ok := strings.Cut(address.Address, "@")
	if !ok || !smip.ValidDomain(strings.ToLower(domain)) {
		return "", errors.New("invalid email domain")
	}
	return local + "@" + strings.ToLower(domain), nil
}

// fromHeader re-encodes a validated From for the MIME copy: the plain
// address alone, or the decoded display name quoted or encoded by net/mail,
// so a peer's raw spelling never reaches the header as written.
func fromHeader(raw string) (string, error) {
	sender, err := plainAddress(raw)
	if err != nil {
		return "", err
	}
	parsed, _ := mail.ParseAddress(raw)
	name := strings.TrimSpace(parsed.Name)
	if name == "" {
		return sender, nil
	}
	return (&mail.Address{Name: name, Address: sender}).String(), nil
}
func recipients(msg mailpack.Message) ([]string, error) {
	groups := []string{msg.To, strings.Join(msg.Cc, ","), strings.Join(msg.Bcc, ",")}
	result := []string{}
	seen := map[string]bool{}
	for _, group := range groups {
		if strings.TrimSpace(group) == "" {
			continue
		}
		addresses, err := mail.ParseAddressList(group)
		if err != nil {
			return nil, err
		}
		for _, address := range addresses {
			value, err := plainAddress(address.Address)
			if err != nil {
				return nil, err
			}
			key := strings.ToLower(value)
			if !seen[key] {
				seen[key] = true
				result = append(result, value)
			}
		}
	}
	if len(result) < 1 || len(result) > 50 {
		return nil, errors.New("mail requires one to fifty recipients")
	}
	return result, nil
}

// Check applies the content rules Prepare freezes to a composed message
// without a transaction, so Queue refuses what delivery would hold: address
// syntax and count, subject and body limits, headers, and a recipient listed
// twice across To, Cc and Bcc, which the provider path rejects at send time.
func Check(msg mailpack.Message) error {
	unique, err := recipients(msg)
	if err != nil {
		return err
	}
	listed := 0
	for _, group := range []string{msg.To, strings.Join(msg.Cc, ","), strings.Join(msg.Bcc, ",")} {
		if strings.TrimSpace(group) == "" {
			continue
		}
		addresses, err := mail.ParseAddressList(group)
		if err != nil {
			return err
		}
		listed += len(addresses)
	}
	if listed != len(unique) {
		return errors.New("the same recipient is listed more than once")
	}
	p := Payload{MessageID: "<check@queue.invalid>", Created: 1, Message: msg}
	p.Message.Bcc = nil
	return p.Validate()
}
func (p Payload) Validate() error {
	m := p.Message
	if p.Created <= 0 || p.MessageID == "" || len(p.MessageID) > 254 || !strings.HasPrefix(p.MessageID, "<") || !strings.HasSuffix(p.MessageID, ">") || strings.ContainsAny(p.MessageID, "\r\n\x00") {
		return errors.New("invalid mail message identity")
	}
	if _, err := plainAddress(m.From); err != nil {
		return err
	}
	if len(m.Subject) > 1000 || strings.ContainsAny(m.Subject, "\r\n\x00") || len(m.Text) > 500000 || len(m.HTML) > 500000 || !utf8.ValidString(m.Text) || !utf8.ValidString(m.HTML) || m.Template != "" || m.Data != nil || m.Lang != "" || len(m.Bcc) > 0 || len(m.Headers) > 2 {
		return errors.New("unsupported or oversized SMIP mail content")
	}
	for _, group := range []string{m.To, strings.Join(m.Cc, ","), m.ReplyTo} {
		if group != "" {
			if strings.ContainsAny(group, "\r\n\x00") {
				return errors.New("invalid address header")
			}
			list, err := mail.ParseAddressList(group)
			if err != nil || len(list) > 50 {
				return errors.New("invalid address header")
			}
		}
	}
	for key, value := range m.Headers {
		if (key != "In-Reply-To" && key != "References") || len(value) > 8192 || strings.ContainsAny(value, "\r\n\x00") {
			return errors.New("unsupported SMIP mail header")
		}
	}
	if len(m.Attachments) > 10 {
		return errors.New("too many attachments")
	}
	total := 0
	for _, file := range m.Attachments {
		total += len(file.Data)
		if total > 10<<20 || file.Name == "" || len(file.Name) > 255 || strings.ContainsAny(file.Name+file.ContentID, "\r\n\x00") {
			return errors.New("invalid or oversized attachment")
		}
		if _, _, err := mime.ParseMediaType(file.ContentType); err != nil {
			return errors.New("invalid attachment media type")
		}
	}
	return nil
}
func decodePayload(raw []byte) (Payload, error) {
	var p Payload
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, err
	}
	canonical, err := json.Marshal(p)
	if err != nil || !bytes.Equal(raw, canonical) {
		return p, errors.New("noncanonical mail payload")
	}
	return p, p.Validate()
}

// MIME creates a readable private .eml copy. The signed payload retains exact
// immutable fields; Bcc addresses never enter either payload headers or MIME.
func (p Payload) MIME() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	var body bytes.Buffer
	mixed := multipart.NewWriter(&body)
	var alternatives bytes.Buffer
	alternative := multipart.NewWriter(&alternatives)
	for _, part := range []struct{ kind, text string }{{"text/plain", p.Message.Text}, {"text/html", p.Message.HTML}} {
		if part.text == "" {
			continue
		}
		writer, err := alternative.CreatePart(textproto.MIMEHeader{"Content-Type": {part.kind + "; charset=utf-8"}, "Content-Transfer-Encoding": {"base64"}})
		if err != nil {
			return nil, err
		}
		if _, err = writer.Write(mimeBase64([]byte(part.text))); err != nil {
			return nil, err
		}
	}
	if err := alternative.Close(); err != nil {
		return nil, err
	}
	writer, err := mixed.CreatePart(textproto.MIMEHeader{"Content-Type": {mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": alternative.Boundary()})}})
	if err != nil {
		return nil, err
	}
	if _, err = writer.Write(alternatives.Bytes()); err != nil {
		return nil, err
	}
	for _, file := range p.Message.Attachments {
		disposition := "attachment"
		if file.ContentID != "" {
			disposition = "inline"
		}
		header := textproto.MIMEHeader{"Content-Type": {file.ContentType}, "Content-Disposition": {mime.FormatMediaType(disposition, map[string]string{"filename": file.Name})}, "Content-Transfer-Encoding": {"base64"}}
		if file.ContentID != "" {
			header.Set("Content-ID", "<"+file.ContentID+">")
		}
		part, err := mixed.CreatePart(header)
		if err != nil {
			return nil, err
		}
		if _, err = part.Write(mimeBase64(file.Data)); err != nil {
			return nil, err
		}
	}
	if err := mixed.Close(); err != nil {
		return nil, err
	}
	from, err := fromHeader(p.Message.From)
	if err != nil {
		return nil, err
	}
	header := textproto.MIMEHeader{"From": {from}, "Subject": {mime.QEncoding.Encode("utf-8", p.Message.Subject)}, "Date": {time.Unix(p.Created, 0).UTC().Format(time.RFC1123Z)}, "Message-ID": {p.MessageID}, "MIME-Version": {"1.0"}, "Content-Type": {mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": mixed.Boundary()})}}
	if p.Message.To != "" {
		header.Set("To", p.Message.To)
	}
	if len(p.Message.Cc) > 0 {
		header.Set("Cc", strings.Join(p.Message.Cc, ", "))
	}
	if p.Message.ReplyTo != "" {
		header.Set("Reply-To", p.Message.ReplyTo)
	}
	for key, value := range p.Message.Headers {
		header.Set(key, value)
	}
	var output bytes.Buffer
	keys := make([]string, 0, len(header))
	for key := range header {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err = fmt.Fprintf(&output, "%s: %s\r\n", key, header.Get(key)); err != nil {
			return nil, err
		}
	}
	output.WriteString("\r\n")
	output.Write(body.Bytes())
	return output.Bytes(), nil
}

// RFC 2045 line wrapping also keeps large attachments readable by MIME parsers.
func mimeBase64(data []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(data)
	var output bytes.Buffer
	for len(encoded) > 76 {
		output.WriteString(encoded[:76])
		output.WriteString("\r\n")
		encoded = encoded[76:]
	}
	output.WriteString(encoded)
	return output.Bytes()
}
