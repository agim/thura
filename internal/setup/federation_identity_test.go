package setup

import (
	"encoding/json"
	"testing"
	"thura/internal/federation"
	"time"
)

func TestGeneratedFederationIdentityIsStableAndTrustsNoRemotePeer(t *testing.T) {
	v := map[string]string{"APP_URL": "https://chat.example.org", "SMIP_ENABLED": "true"}
	if changed, err := configureFederationIdentity(v, time.Now()); err != nil || !changed {
		t.Fatal("identity not generated", err)
	}
	before := v["SMIP_CONFIG"]
	var c federation.Config
	if err := json.Unmarshal([]byte(before), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Peers) != 0 || len(c.Seed) < 32 || c.Domain != "chat.example.org" {
		t.Fatal("invalid identity or invented peer")
	}
	if _, err := federation.NewGateway(c); err != nil {
		t.Fatal(err)
	}
	if changed, err := configureFederationIdentity(v, time.Now().Add(time.Hour)); err != nil || changed || v["SMIP_CONFIG"] != before {
		t.Fatal("ordinary save rotated identity")
	}
}
