package setup

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestProbeDiagnosticsExposeCauseWithoutProviderSecrets(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errors.New("storage write: POST /private: 403 SignatureDoesNotMatch: key=VERY_SECRET&signature=PRIVATE"), "HTTP 403; SignatureDoesNotMatch"},
		{fmt.Errorf("storage read: %w", &net.DNSError{Err: "no such host", Name: "private-host"}), "DNS lookup failed"},
		{errors.New("storage initialization: mkdir /private/VERY_SECRET: not a directory"), "not a directory"},
		{errors.New("public SMIP route does not prove TLS 1.3"), "public SMIP route does not prove TLS 1.3"},
		{errors.New("provider echoed VERY_SECRET and PRIVATE"), "inspect the service logs"},
	}
	for _, tc := range cases {
		got := probeDiagnostic(tc.err)
		if !strings.Contains(got, tc.want) || strings.Contains(got, "VERY_SECRET") || strings.Contains(got, "PRIVATE") || strings.Contains(got, "private-host") {
			t.Fatalf("unsafe or missing diagnostic: %s", got)
		}
	}
	if got := probeResult("storage", "failed", "HTTP 403; AccessDenied.", "attempt-123"); !strings.Contains(got, "AccessDenied") || !strings.Contains(got, "attempt-123") {
		t.Fatal(got)
	}
}
