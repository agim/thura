package setup

import "testing"

func TestSetupProbeBudgetsSeparateSideEffectsFromTroubleshooting(t *testing.T) {
	for _, kind := range []string{"mail", "ai"} {
		if probeLimit(kind) != 3 {
			t.Fatalf("%s lost its tighter side-effect budget", kind)
		}
	}
	for _, kind := range []string{"storage", "matrix", "collabora", "livekit", "smip"} {
		if probeLimit(kind) != 30 {
			t.Fatalf("%s has an impractical connection-check budget", kind)
		}
	}
}
