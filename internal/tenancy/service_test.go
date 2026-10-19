package tenancy

import (
	"testing"
	q "thura/db/queries/gen"
	"time"
)

func TestDomainCanonicalizationAndAdministratorProof(t *testing.T) {
	for _, name := range []string{"localhost", "192.168.1.2", "https://team.example", "team.example:443", "a..example", "team.local", "*.example.com"} {
		if _, e := Domain(name); e == nil {
			t.Errorf("accepted unsafe domain %q", name)
		}
	}
	got, e := Domain(" Team.Example.COM. ")
	if e != nil || got != "team.example.com" {
		t.Fatalf("canonical domain %q: %v", got, e)
	}
	now := time.Now()
	expired := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	for _, tc := range []struct {
		d      q.TeamDomain
		active bool
	}{{q.TeamDomain{Status: "verified", Verification: "administrator"}, true}, {q.TeamDomain{Status: "suspended", Verification: "administrator"}, false}, {q.TeamDomain{Status: "verified", Verification: "dns", ValidUntil: &expired}, false}, {q.TeamDomain{Status: "verified", Verification: "dns", ValidUntil: &future}, true}, {q.TeamDomain{Status: "pending", Verification: "dns", ValidUntil: &future}, false}} {
		if Active(tc.d, now) != tc.active {
			t.Errorf("invalid ownership state %+v", tc.d)
		}
	}
}
