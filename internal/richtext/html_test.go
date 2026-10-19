package richtext

import (
	"strings"
	"testing"
)

func TestAuthoredFormattingSurvivesWithoutActiveContent(t *testing.T) {
	clean := Clean(`<p><strong>Plan</strong> <em>today</em> <a href="https://example.com/plan" onclick="steal()">Details</a></p><ul><li>One</li></ul><script>secret</script><img src="https://tracker.test"><a href="javascript:alert(1)">Unsafe</a>`)
	for _, keep := range []string{"<strong>Plan</strong>", "<em>today</em>", `href="https://example.com/plan"`, "<ul><li>One</li></ul>"} {
		if !strings.Contains(clean, keep) {
			t.Errorf("lost authored formatting %q: %s", keep, clean)
		}
	}
	for _, blocked := range []string{"onclick", "secret", "<img", "javascript:"} {
		if strings.Contains(clean, blocked) {
			t.Errorf("kept active content %q: %s", blocked, clean)
		}
	}
	if again := Clean(clean); again != clean {
		t.Fatalf("policy is not stable: %s", again)
	}
}
