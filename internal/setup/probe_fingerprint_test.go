package setup

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"thura/internal/federation"
	"thura/internal/smipmail"
)

func TestSMIPFingerprintBindsSigningIdentityNotSerialisation(t *testing.T) {
	t.Setenv("LIDZA_MASTER_KEY", strings.Repeat("4b", 32))
	now := time.Now()
	v := map[string]string{"APP_URL": "https://setup.example", "SMIP_ENABLED": "true", "SMIP_MAIL_ENABLED": "true", "MAIL_FROM": "sender@setup.example", "THURA_MAIL_ADDRESS": "inbox@setup.example", "THURA_MAX_MEMBERS": "5"}
	if err := configureMailIdentity(v, url.Values{}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := configureFederationIdentity(v, now); err != nil {
		t.Fatal(err)
	}
	checked, err := probeFingerprint(v, "smip")
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := func(values map[string]string) string {
		t.Helper()
		out, err := probeFingerprint(values, "smip")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	clone := func() map[string]string {
		out := map[string]string{}
		for k, val := range v {
			out[k] = val
		}
		return out
	}

	// An unrelated step saved after the check keeps the identities verbatim.
	unrelated := clone()
	unrelated["THURA_MAX_MEMBERS"] = "9"
	if err := configureMailIdentity(unrelated, url.Values{}, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := configureFederationIdentity(unrelated, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if fingerprint(unrelated) != checked {
		t.Fatal("unrelated save invalidated the smip check")
	}

	// The same identity serialised differently (key order, whitespace, a shifted
	// signing interval) still names the checked signing key.
	var mail smipmail.Config
	if err := json.Unmarshal([]byte(v["SMIP_MAIL_CONFIG"]), &mail); err != nil {
		t.Fatal(err)
	}
	var chat federation.Config
	if err := json.Unmarshal([]byte(v["SMIP_CONFIG"]), &chat); err != nil {
		t.Fatal(err)
	}
	reserialised := clone()
	reserialised["SMIP_MAIL_CONFIG"] = `{ "notAfter": ` + strconv.FormatInt(mail.NotAfter+1, 10) + `, "notBefore": ` + strconv.FormatInt(mail.NotBefore+1, 10) + `, "seed": "` + mail.Seed + `", "domain": "` + mail.Domain + `" }`
	reserialised["SMIP_CONFIG"] = `{"peers":{},"notAfter":` + strconv.FormatInt(chat.NotAfter+1, 10) + `,"notBefore":` + strconv.FormatInt(chat.NotBefore+1, 10) + `,"seed":"` + chat.Seed + `","domain":"` + chat.Domain + `"}`
	if fingerprint(reserialised) != checked {
		t.Fatal("re-serialised identity invalidated the smip check")
	}

	// A different signing key, domain, route or a malformed identity is a new check.
	rotated := clone()
	delete(rotated, "SMIP_MAIL_CONFIG")
	if err := configureMailIdentity(rotated, url.Values{}, now); err != nil {
		t.Fatal(err)
	}
	if fingerprint(rotated) == checked {
		t.Fatal("rotated mail signing key kept the old check")
	}
	moved := clone()
	moved["SMIP_MAIL_CONFIG"] = strings.Replace(v["SMIP_MAIL_CONFIG"], "setup.example", "other.example", 1)
	if fingerprint(moved) == checked {
		t.Fatal("changed identity domain kept the old check")
	}
	route := clone()
	route["APP_URL"] = "https://other.example"
	if fingerprint(route) == checked {
		t.Fatal("changed public route kept the old check")
	}
	malformed := clone()
	malformed["SMIP_CONFIG"] = v["SMIP_CONFIG"][:len(v["SMIP_CONFIG"])-1]
	if fingerprint(malformed) == checked {
		t.Fatal("malformed identity shared the valid fingerprint")
	}
	if signingIdentity("not json") != "not json" || signingIdentity(`{"domain":"setup.example","seed":"short"}`) != `{"domain":"setup.example","seed":"short"}` {
		t.Fatal("unparseable identity was not bound verbatim")
	}
}
