package app

import (
	"net/http"
	"net/netip"

	"github.com/agim/lidza/packs/auth"
	"github.com/agim/lidza/pkg/middleware"
)

// networkKey groups an IPv6 client by its /64: a visitor usually holds a
// whole block, so a per-address bucket is no limit at all there. IPv4 and
// unparsable addresses keep the client address.
func networkKey(r *http.Request) string {
	ip := middleware.ClientIP(r)
	a, err := netip.ParseAddr(ip)
	if err != nil || a.Is4() || a.Is4In6() {
		return ip
	}
	return netip.PrefixFrom(a, 64).Masked().String()
}

// userKey limits a signed-in route per account, else per network.
func userKey(r *http.Request) string {
	if u := auth.CurrentUser(r.Context()); u != nil {
		return "user:" + u.ID
	}
	return networkKey(r)
}
