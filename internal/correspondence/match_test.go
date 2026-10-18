package correspondence

import "testing"

func TestMatchesMailboxIdentity(t *testing.T) {
	identities := map[string]bool{"john@example.com": true}
	for _, tt := range []struct {
		header string
		want   bool
	}{
		{`John Smith <JOHN@example.com>`, true},
		{`Jane <jane@example.com>, John <john@example.com>`, true},
		{`"john@example.com" <stranger@example.com>`, false},
		{`notjohn@example.com`, false},
		{`john@example.com.evil.test`, false},
		{`invalid <`, false},
	} {
		if got := Matches(identities, tt.header); got != tt.want {
			t.Errorf("Matches(%q)=%v want %v", tt.header, got, tt.want)
		}
	}
	if !Matches(identities, "", "someone@example.com", "john@example.com") {
		t.Fatal("CC identity not found")
	}
}
