// Package push sends Web Push notifications (RFC 8030, RFC 8291 message
// encryption, RFC 8292 VAPID) to the browsers a member turned notifications
// on in. Events call Notify; one job per subscription delivers it with
// retries. Payloads are short and carry a URL that opens the right view.
package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/mail"
	"net/url"
	"os"
	"strings"

	"github.com/agim/lidza/packs/db"
	"github.com/agim/lidza/pkg/credentials"
	"github.com/agim/lidza/pkg/env"
	"github.com/jackc/pgx/v5"
	q "thura/db/queries/gen"
)

// vapidName is the one key pair this server signs with.
const vapidName = "default"

// ErrUnavailable means the server cannot sign pushes: no master key to seal
// or unseal the VAPID private key.
var ErrUnavailable = errors.New("push notifications are unavailable: the server master key is missing")

// Keys returns the server's VAPID key pair (base64url, uncompressed P-256
// public point and raw private scalar). The first call on a server generates
// it and stores it with the private key sealed by the master key; every node
// and every restart reads the same row. Concurrent first calls race on the
// unique name and all read the winner.
func Keys(ctx context.Context) (public, private string, err error) {
	key, err := sealKey()
	if err != nil {
		return "", "", ErrUnavailable
	}
	queries := q.New(db.From(ctx))
	row, err := queries.GetPushVapidKey(ctx, vapidName)
	if errors.Is(err, pgx.ErrNoRows) {
		pair, gerr := ecdh.P256().GenerateKey(rand.Reader)
		if gerr != nil {
			return "", "", gerr
		}
		sealed, serr := credentials.Encrypt(key, []byte(base64.RawURLEncoding.EncodeToString(pair.Bytes())))
		if serr != nil {
			return "", "", serr
		}
		if err = queries.AddPushVapidKey(ctx, q.AddPushVapidKeyParams{Name: vapidName, PublicKey: base64.RawURLEncoding.EncodeToString(pair.PublicKey().Bytes()), PrivateKey: sealed}); err != nil {
			return "", "", err
		}
		row, err = queries.GetPushVapidKey(ctx, vapidName)
	}
	if err != nil {
		return "", "", err
	}
	plain, err := credentials.Decrypt(key, row.PrivateKey)
	if err != nil {
		return "", "", ErrUnavailable
	}
	return row.PublicKey, string(plain), nil
}

// testSealKey is a public key for the browser suite only: lidza test --e2e
// runs the binary with LIDZA_MODE=test and the deployment master key made
// unreadable, so notifications there are sealed with this instead. It never
// applies outside test mode.
const testSealKey = "5075626c69632d746573742d7075736800000000000000000000000000000000"

func sealKey() ([]byte, error) {
	key, err := credentials.Key(".")
	if err != nil && os.Getenv("LIDZA_MODE") == "test" {
		return hex.DecodeString(testSealKey)
	}
	return key, err
}

// PublicKey is the application server key browsers subscribe with, or ""
// when the server cannot sign pushes.
func PublicKey(ctx context.Context) string {
	public, _, err := Keys(ctx)
	if err != nil {
		return ""
	}
	return public
}

// subscriber is the VAPID contact (the JWT "sub", sent to push services so
// they can reach the operator): the operator contact email from setup, else
// the sender address, else postmaster at the APP_URL host. The webpush
// library adds the mailto: scheme.
func subscriber() string {
	var cfg struct {
		Operator string `env:"THURA_OPERATOR_EMAIL"`
		From     string `env:"MAIL_FROM"`
		URL      string `env:"APP_URL"`
	}
	if env.Load(".", &cfg) == nil {
		for _, candidate := range []string{cfg.Operator, cfg.From} {
			if a, err := mail.ParseAddress(strings.TrimSpace(candidate)); err == nil && a.Address != "" {
				return a.Address
			}
		}
		if u, err := url.Parse(cfg.URL); err == nil && u.Hostname() != "" && strings.Contains(u.Hostname(), ".") {
			return "postmaster@" + u.Hostname()
		}
	}
	return "postmaster@localhost"
}
