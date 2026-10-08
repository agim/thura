package smip

import (
	"crypto/ed25519"
	"errors"
)

// Accepted means the receiver durably stored the complete payload and receipt.
// It does not mean a person read the chat or imported the file into Drive.
type Receipt struct {
	Version   string `json:"version"`
	ID        string `json:"id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Digest    string `json:"digest"`
	Accepted  int64  `json:"accepted"`
	KeyID     string `json:"keyId"`
	Signature string `json:"signature"`
}

func (r Receipt) signingBytes() []byte {
	return canonical("SMIP-RECEIPT", []any{r.Version, r.ID, r.From, r.To, r.Digest, r.Accepted, r.KeyID})
}
func receiptFor(p Packet, private ed25519.PrivateKey, now int64) Receipt {
	r := Receipt{Version: Version, ID: p.Envelope.ID, From: p.Envelope.To, To: p.Envelope.From, Digest: p.Digest(), Accepted: now, KeyID: KeyID(private.Public().(ed25519.PublicKey))}
	r.Signature = Encode(ed25519.Sign(private, r.signingBytes()))
	return r
}
func (r Receipt) Verify(p Packet, key Key) error {
	if r.Version != Version || r.ID != p.Envelope.ID || r.From != p.Envelope.To || r.To != p.Envelope.From || r.Digest != p.Digest() || r.Accepted <= 0 || r.Accepted < p.Envelope.Created-120 || r.Accepted >= p.Envelope.Expires || len(key.Public) != ed25519.PublicKeySize || KeyID(key.Public) != r.KeyID || key.Revoked || r.Accepted < key.NotBefore || r.Accepted >= key.NotAfter {
		return errors.New("untrusted or mismatched receipt")
	}
	sig, err := decode(r.Signature, ed25519.SignatureSize)
	if err != nil || !ed25519.Verify(key.Public, r.signingBytes(), sig) {
		return errors.New("invalid receipt signature")
	}
	return nil
}
