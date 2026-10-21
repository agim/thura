package setup

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"regexp"
	"strings"
)

var diagnosticStatus = regexp.MustCompile(`\b([45][0-9]{2})\b`)
var diagnosticCode = regexp.MustCompile(`\b(AccessDenied|InvalidAccessKeyId|SignatureDoesNotMatch|AuthorizationHeaderMalformed|NoSuchBucket|NoSuchKey|PermanentRedirect|InvalidRegion|RequestTimeTooSkewed|ExpiredToken|InvalidToken|SlowDown|ServiceUnavailable)\b`)

// Provider error bodies can echo signatures, keys, signed URLs or private input.
// Persist only classified causes, status/code and application-owned stage names.
func probeDiagnostic(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	var parts []string
	for _, stage := range []string{"configuration", "initialization", "write", "read", "metadata", "listing", "deletion", "deletion verification", "read verification", "metadata verification", "listing verification"} {
		if strings.Contains(text, "storage "+stage+":") {
			parts = append(parts, "Storage "+stage)
		}
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		parts = append(parts, "Connection timed out")
	case errors.Is(err, context.Canceled):
		parts = append(parts, "Check was interrupted")
	case errors.Is(err, os.ErrPermission):
		parts = append(parts, "Filesystem permission denied")
	case errors.Is(err, os.ErrNotExist):
		parts = append(parts, "Filesystem path does not exist")
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		parts = append(parts, "DNS lookup failed")
	}
	var cert x509.UnknownAuthorityError
	var host x509.HostnameError
	if errors.As(err, &cert) || errors.As(err, &host) {
		parts = append(parts, "TLS certificate verification failed")
	}
	for _, cause := range []string{"connection refused", "network is unreachable", "not a directory", "no space left on device", "read-only file system", "public SMIP route does not prove TLS 1.3", "SMIP transport challenge failed", "model returned no response", "office editing is unavailable", "content or size mismatch", "size mismatch", "test object was not returned", "object absence was not confirmed"} {
		if strings.Contains(text, cause) {
			parts = append(parts, cause)
		}
	}
	if status := diagnosticStatus.FindString(text); status != "" {
		parts = append(parts, "HTTP "+status)
	}
	if code := diagnosticCode.FindString(text); code != "" {
		parts = append(parts, code)
	}
	if len(parts) == 0 {
		return "Provider connection or configuration failed; inspect the service logs using this check ID."
	}
	return strings.Join(parts, "; ") + "."
}

func probeResult(kind, state, diagnostic, id string) string {
	message := probeMessage(kind, state)
	if state != "failed" && state != "cleanup_required" {
		return message
	}
	if diagnostic == "" {
		diagnostic = "No diagnostic was recorded for this older attempt; retry to capture one."
	}
	return message + " Error: " + diagnostic + " Check ID: " + id + "."
}
