package smipmail

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"thura/internal/smip"
)

// StatusLifetime bounds how long a DNS/transport status is reused in this
// process; StatusTimeout bounds how long one page waits for all its lookups.
const StatusLifetime = 60 * time.Second
const StatusTimeout = 6 * time.Second

// Status is the informational DNS and transport view of one SMIP mail domain.
// It is advisory: a missing record or failed probe tells the operator what is
// still unpublished and never blocks publication or replaces the recorded
// transport check. TXT is "found", "missing" or "error"; MatchesServer is
// "yes", "no" or "unknown"; TLS is "yes" or "no".
type Status struct {
	Domain        string
	TXT           string
	Addresses     []string
	MatchesServer string
	TLS           string
	Detail        string
	CheckedAt     time.Time
}

var statusCache = struct {
	sync.Mutex
	entries map[string]Status
}{entries: map[string]Status{}}

// DomainStatus resolves _smip.<domain> TXT and smip.<domain> addresses,
// compares them with the public addresses of appURL's host, and repeats the
// setup transport challenge against the SMIP host through the SSRF-safe TLS
// 1.3 client. A pinned origin (v0.2 origins) replaces the public host.
func (g *Gateway) DomainStatus(ctx context.Context, domain, appURL string, now time.Time) Status {
	domain = strings.ToLower(strings.TrimSpace(domain))
	statusCache.Lock()
	cached, ok := statusCache.entries[domain]
	statusCache.Unlock()
	if ok && now.Before(cached.CheckedAt.Add(StatusLifetime)) {
		return cached
	}
	status := g.probeStatus(ctx, domain, appURL, now)
	statusCache.Lock()
	if len(statusCache.entries) >= 1000 {
		statusCache.entries = map[string]Status{}
	}
	statusCache.entries[domain] = status
	statusCache.Unlock()
	return status
}

func (g *Gateway) probeStatus(ctx context.Context, domain, appURL string, now time.Time) Status {
	status := Status{Domain: domain, TXT: "error", Addresses: []string{}, MatchesServer: "unknown", TLS: "no", CheckedAt: now}
	if !smip.ValidDomain(domain) {
		status.Detail = "The identity domain is not a valid DNS name."
		return status
	}
	ctx, cancel := context.WithTimeout(ctx, StatusTimeout)
	defer cancel()
	resolver := g.resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	var details []string
	records, err := resolver.LookupTXT(ctx, "_smip."+domain)
	found := 0
	for _, record := range records {
		if record == "v=SMIP1" {
			found++
		}
	}
	var dns *net.DNSError
	switch {
	case err == nil && found == 1:
		status.TXT = "found"
	case err == nil && found > 1:
		details = append(details, "_smip."+domain+" publishes v=SMIP1 more than once; keep exactly one record.")
	case err == nil || errors.As(err, &dns) && dns.IsNotFound:
		status.TXT = "missing"
	default:
		details = append(details, "The _smip."+domain+" TXT lookup failed; the resolver may be unavailable.")
	}
	host := "smip." + domain
	origin := "https://" + host
	pinned := false
	origins, err := g.pinnedOrigins(ctx)
	if err != nil {
		details = append(details, "The pinned origins could not be read; the status may not reflect them.")
	}
	if pinnedOrigin, ok := origins[domain]; ok {
		origin, pinned = pinnedOrigin, true
		if u, err := url.Parse(pinnedOrigin); err == nil {
			host = u.Hostname()
		}
	}
	addresses, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		details = append(details, host+" does not resolve yet.")
	}
	for _, address := range addresses {
		status.Addresses = append(status.Addresses, address.IP.String())
	}
	sort.Strings(status.Addresses)
	if len(status.Addresses) > 0 {
		if server := serverAddresses(ctx, resolver, appURL); len(server) > 0 {
			status.MatchesServer = "no"
			for _, address := range status.Addresses {
				if server[address] {
					status.MatchesServer = "yes"
				}
			}
		}
		if err := g.challenge(ctx, origin, pinned); err == nil {
			status.TLS = "yes"
		} else {
			details = append(details, "Direct TLS 1.3 to "+origin+" was not confirmed: "+err.Error())
		}
	}
	status.Detail = strings.Join(details, " ")
	return status
}

// serverAddresses resolves the application host so the page can say whether
// smip.<domain> points at this installation. A loopback, private or
// unresolvable APP_URL yields nothing, which the caller reports as unknown.
func serverAddresses(ctx context.Context, resolver *net.Resolver, appURL string) map[string]bool {
	u, err := url.Parse(appURL)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	addresses, err := resolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, address := range addresses {
		if publicIP(address.IP) {
			out[address.IP.String()] = true
		}
	}
	return out
}

// challenge repeats the setup transport check against the SMIP host: a fresh
// nonce answered over verified TLS 1.3 by Thura's own route. A forwarded
// header or a browser certificate alone never passes.
func (g *Gateway) challenge(ctx context.Context, origin string, pinned bool) error {
	client, err := g.client(ctx, origin, pinned)
	if err != nil {
		if !pinned {
			return errors.New("the host must resolve to public addresses")
		}
		return err
	}
	defer client.CloseIdleConnections()
	nonce := uuid.NewString()
	request, err := http.NewRequestWithContext(ctx, "GET", origin+"/api/v1/setup/transport?nonce="+nonce, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("connection or certificate verification failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.TLS == nil || response.TLS.Version < tls.VersionTLS13 {
		return errors.New("the route did not answer over TLS 1.3 (HTTP " + http.StatusText(response.StatusCode) + ")")
	}
	var reply struct {
		Nonce     string `json:"nonce"`
		Transport string `json:"transport"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1024)).Decode(&reply) != nil || reply.Nonce != nonce || reply.Transport != "tls1.3" {
		return errors.New("the transport challenge was not answered by Thura")
	}
	return nil
}

// ForgetStatus drops a cached status so a changed configuration is re-checked.
func ForgetStatus(domain string) {
	statusCache.Lock()
	delete(statusCache.entries, strings.ToLower(strings.TrimSpace(domain)))
	statusCache.Unlock()
}

// ForgetAllStatus drops every cached status, after the CA bundle changed.
func ForgetAllStatus() {
	statusCache.Lock()
	statusCache.entries = map[string]Status{}
	statusCache.Unlock()
}
