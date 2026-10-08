// Package smip implements the experimental SMIP/0.1 chat/file wire profile.
// It is a reference transport, not an enabled Thura provider.
package smip

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"time"
	"unicode/utf8"
)

const Version = "smip/0.1"
const MaxPayload = 16 << 20
const MaxChat = 64 << 10
const MaxWire = ((MaxPayload + 2) / 3 * 4) + 4096
const MaxLifetime = 7 * 24 * time.Hour

var ErrConflict = errors.New("message ID already used with different content")
var domainPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)
var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// Envelope fields are all signed. Payload and Name use unpadded base64url.
// Name is a display name, never a filesystem path.
type Envelope struct {
	Version   string `json:"version"`
	ID        string `json:"id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Sender    string `json:"sender"`
	Recipient string `json:"recipient"`
	Stream    string `json:"stream"`
	Kind      string `json:"kind"`
	Created   int64  `json:"created"`
	Expires   int64  `json:"expires"`
	KeyID     string `json:"keyId"`
	Name      string `json:"name"`
	Payload   string `json:"payload"`
}
type Packet struct {
	Envelope  Envelope `json:"envelope"`
	Signature string   `json:"signature"`
}
type Key struct {
	Public    ed25519.PublicKey
	NotBefore int64
	NotAfter  int64
	Revoked   bool
}

func KeyID(public ed25519.PublicKey) string {
	sum := sha256.Sum256(public)
	return hex.EncodeToString(sum[:])
}
func validDomain(s string) bool {
	if net.ParseIP(s) != nil || !domainPattern.MatchString(s) || !bytes.ContainsRune([]byte(s), '.') {
		return false
	}
	for _, label := range bytes.Split([]byte(s), []byte(".")) {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
	}
	return true
}
func addressAt(address, domain string) bool {
	prefix := bytes.TrimSuffix([]byte(address), []byte("@"+domain))
	return len(prefix) < len(address) && tokenPattern.Match(prefix)
}
func decode(value string, max int) ([]byte, error) {
	if len(value) > ((max + 2) / 3 * 4) {
		return nil, errors.New("encoded value exceeds limit")
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(b) > max || base64.RawURLEncoding.EncodeToString(b) != value {
		return nil, errors.New("invalid canonical base64url")
	}
	return b, nil
}
func (e Envelope) Validate() error {
	if e.Version != Version || !tokenPattern.MatchString(e.ID) || !validDomain(e.From) || !validDomain(e.To) || !addressAt(e.Sender, e.From) || !addressAt(e.Recipient, e.To) || !tokenPattern.MatchString(e.Stream) {
		return errors.New("invalid envelope identity")
	}
	if e.Created <= 0 || e.Expires <= e.Created || e.Expires-e.Created > int64(MaxLifetime/time.Second) || e.Created >= 1<<53 || e.Expires >= 1<<53 {
		return errors.New("invalid envelope lifetime")
	}
	if len(e.KeyID) != 64 {
		return errors.New("invalid signing key ID")
	}
	if _, err := hex.DecodeString(e.KeyID); err != nil {
		return err
	}
	size := MaxPayload
	if e.Kind == "chat" {
		size = MaxChat
		if e.Name != "" {
			return errors.New("chat cannot carry file name")
		}
	} else if e.Kind != "file" {
		return errors.New("unsupported message kind")
	}
	payload, err := decode(e.Payload, size)
	if err != nil {
		return err
	}
	if e.Kind == "chat" && !utf8.Valid(payload) {
		return errors.New("chat must be UTF-8 text")
	}
	if e.Kind == "file" {
		name, err := decode(e.Name, 255)
		if err != nil || len(name) == 0 || !utf8.Valid(name) || bytes.IndexByte(name, 0) >= 0 {
			return errors.New("invalid file display name")
		}
	}
	return nil
}

// Canonical signing bytes are a domain-separated, fixed-order JSON array.
// Every string is ASCII without JSON escapes; timestamps are safe integers.
func (e Envelope) signingBytes() []byte {
	return canonical("SMIP-ENVELOPE", []any{e.Version, e.ID, e.From, e.To, e.Sender, e.Recipient, e.Stream, e.Kind, e.Created, e.Expires, e.KeyID, e.Name, e.Payload})
}
func Sign(e Envelope, private ed25519.PrivateKey) (Packet, error) {
	if len(private) != ed25519.PrivateKeySize {
		return Packet{}, errors.New("invalid private key")
	}
	e.KeyID = KeyID(private.Public().(ed25519.PublicKey))
	if err := e.Validate(); err != nil {
		return Packet{}, err
	}
	return Packet{e, base64.RawURLEncoding.EncodeToString(ed25519.Sign(private, e.signingBytes()))}, nil
}
func (p Packet) Verify(key Key) error {
	if err := p.Envelope.Validate(); err != nil {
		return err
	}
	if len(key.Public) != ed25519.PublicKeySize || key.Revoked || KeyID(key.Public) != p.Envelope.KeyID || p.Envelope.Created < key.NotBefore || p.Envelope.Created >= key.NotAfter {
		return errors.New("untrusted or inactive signing key")
	}
	sig, err := decode(p.Signature, ed25519.SignatureSize)
	if err != nil || !ed25519.Verify(key.Public, p.Envelope.signingBytes(), sig) {
		return errors.New("invalid envelope signature")
	}
	return nil
}
func (p Packet) Digest() string {
	sum := sha256.Sum256(p.Envelope.signingBytes())
	return hex.EncodeToString(sum[:])
}
func (e Envelope) Bytes() ([]byte, error) { return decode(e.Payload, MaxPayload) }
func Encode(b []byte) string              { return base64.RawURLEncoding.EncodeToString(b) }
func strictJSON(b []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	// The reference profile requires the exact compact wire encoding: reject
	// duplicate fields, alternate escaping, unknown fields and trailing data.
	canonical, err := json.Marshal(target)
	if err != nil {
		return err
	}
	if !bytes.Equal(b, canonical) {
		return fmt.Errorf("noncanonical JSON packet")
	}
	return nil
}

// Only fixed primitive profile fields reach this encoder. An unsupported
// future field type is a programming error; never silently sign empty bytes.
func canonical(prefix string, fields []any) []byte {
	b, err := json.Marshal(fields)
	if err != nil {
		panic(fmt.Errorf("SMIP canonical field encoding: %w", err))
	}
	return append([]byte(prefix+"\n"), b...)
}
