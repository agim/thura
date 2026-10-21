package setup

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/url"
	"thura/internal/federation"
	"thura/internal/smip"
	"time"
)

// A new server owns its identity, not another server's trust. Generating a local
// key never invents a peer or authorizes remote chat/file delivery. Pairing and
// consent remain explicit when an actual remote installation is connected.
func configureFederationIdentity(v map[string]string, now time.Time) (bool, error) {
	if v["SMIP_ENABLED"] != "true" || v["SMIP_CONFIG"] != "" {
		return false, nil
	}
	public, err := url.Parse(v["APP_URL"])
	if err != nil || !smip.ValidDomain(public.Hostname()) {
		return false, nil
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err = rand.Read(seed); err != nil {
		return false, err
	}
	config := federation.Config{Domain: public.Hostname(), Seed: smip.Encode(seed), NotBefore: now.Add(-time.Minute).Unix(), NotAfter: now.Add(365 * 24 * time.Hour).Unix(), Peers: map[string]map[string]smip.Key{}}
	raw, err := json.Marshal(config)
	if err != nil {
		return false, err
	}
	v["SMIP_CONFIG"] = string(raw)
	return true, nil
}
