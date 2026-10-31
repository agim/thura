package livekit

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/agim/lidza"
	"github.com/livekit/protocol/auth"
)

// The outcomes of CheckSignal, in plain words; setup shows and persists them.
var (
	ErrSignalNotForwarded = errors.New("/rtc is not forwarded to LiveKit port 7880 with the prefix stripped")
	ErrSignalTimeout      = errors.New("the public signalling address did not answer within 8 seconds")
	ErrSignalUnreachable  = errors.New("the public signalling address could not be reached from this server")
	ErrSignalRefusedHost  = errors.New("the public signalling address resolves to a private, link-local or metadata address")
	ErrSignalTLS          = errors.New("the public signalling address has no trusted TLS certificate")
)

// SignalTimeout bounds the whole check; tests shorten it.
var SignalTimeout = 8 * time.Second

// ValidateURL is where livekit-client asks LiveKit why a connection failed:
// <public URL>/rtc/validate, over HTTP(S). Through the proxy of
// deploy/lidza.stack.json that is LiveKit's own /rtc/validate.
func (c Config) ValidateURL() (*url.URL, error) {
	if err := c.ValidatePublic(); err != nil {
		return nil, err
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil {
		return nil, err
	}
	u.Scheme = map[string]string{"wss": "https", "ws": "http"}[u.Scheme]
	u.Path = strings.TrimRight(u.Path, "/") + "/rtc/validate"
	return u, nil
}

// CheckSignal asks the public signalling address what livekit-client asks,
// from this server, with a signed token that grants nothing: LiveKit answers
// 401 (or 400) in plain text, while a missing proxy route gives the app's
// page, a 404 or a 502. Only the configured host is called; it must resolve
// to public addresses, except that appHost (the app's own domain) may
// resolve to a loopback or private one. Link-local and metadata addresses
// are always refused, the connection goes to the addresses checked, and
// redirects are not followed. A transport the services provide (a test
// fixture) is used as it is, without a DNS lookup.
func (c Config) CheckSignal(ctx context.Context, appHost string, resolver *net.Resolver) error {
	target, err := c.ValidateURL()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, SignalTimeout)
	defer cancel()
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	host := target.Hostname()
	local := appHost != "" && strings.EqualFold(host, appHost)
	fixture, injected := lidza.Optional[http.RoundTripper](ctx)
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip.Unmap()}
	} else if !injected {
		found, err := resolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(found) == 0 {
			if ctx.Err() != nil {
				return ErrSignalTimeout
			}
			return ErrSignalUnreachable
		}
		for _, ip := range found {
			ips = append(ips, ip.Unmap())
		}
	}
	for _, ip := range ips {
		if !allowedSignalAddr(ip, local) {
			return ErrSignalRefusedHost
		}
	}
	port := target.Port()
	if port == "" {
		port = map[string]string{"https": "443", "http": "80"}[target.Scheme]
	}
	client := http.Client{Timeout: SignalTimeout, Transport: fixture}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if !injected {
		// Dial only the addresses checked above: no proxy, no second lookup.
		base := http.DefaultTransport.(*http.Transport).Clone()
		base.Proxy = nil
		base.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			var last error
			for _, ip := range ips {
				conn, err := (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			return nil, last
		}
		client.Transport = base
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return err
	}
	// No identity, room or grant: LiveKit verifies the signature and refuses.
	token, err := auth.NewAccessToken(c.Key, c.Secret).SetValidFor(time.Minute).ToJWT()
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := client.Do(req)
	if err != nil {
		var authority x509.UnknownAuthorityError
		var hostname x509.HostnameError
		var invalid x509.CertificateInvalidError
		switch {
		case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
			return ErrSignalTimeout
		case errors.As(err, &authority) || errors.As(err, &hostname) || errors.As(err, &invalid):
			return ErrSignalTLS
		}
		return ErrSignalUnreachable
	}
	defer res.Body.Close()
	head, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		if ctx.Err() != nil {
			return ErrSignalTimeout
		}
		return ErrSignalUnreachable
	}
	media, _, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if err != nil {
		media = "" // no or an unreadable type: the body decides
	}
	html := media == "text/html" || strings.HasPrefix(strings.TrimSpace(strings.ToLower(string(head))), "<")
	if !html && (res.StatusCode == 200 || res.StatusCode == 400 || res.StatusCode == 401 || res.StatusCode == 403) {
		return nil
	}
	return ErrSignalNotForwarded
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

// blockedSignal are shared, documentation and translation ranges, as
// internal/smipmail's publicIP refuses them.
func blockedSignal() [12]string {
	return [12]string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16", "2001::/32", "fd00:ec2::254/128"}
}

// allowedSignalAddr admits a public unicast address; for the app's own
// domain also loopback and private ranges. Link-local (which holds the cloud
// metadata service), unspecified and multicast never pass.
func allowedSignalAddr(ip netip.Addr, local bool) bool {
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return local
	}
	if !ip.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range blockedSignal() {
		if netip.MustParsePrefix(prefix).Contains(ip) {
			return false
		}
	}
	return true
}
