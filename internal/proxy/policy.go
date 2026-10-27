package proxy

import (
	"fmt"
	"net/netip"
)

// PublicHTTPS is the policy egress runs with: port 443, on a public address.
func PublicHTTPS(ap netip.AddrPort) error {
	if ap.Port() != 443 {
		return &Refusal{Reason: fmt.Sprintf("port %d, and only 443 goes through", ap.Port())}
	}
	if !Public(ap.Addr()) {
		return &Refusal{Reason: fmt.Sprintf("%s is not a public address", ap.Addr())}
	}
	return nil
}

// Public reports whether a is an address on the public internet. It is not
// when it points at the machine itself, at a network the machine sits in, or
// into any range IANA keeps for something other than the public internet.
func Public(a netip.Addr) bool {
	// A zone ties an address to one link, so a public address never has one.
	// It also hides the address from the checks below: Prefix.Contains is
	// false for any zoned address, and ::%en0 does not equal ::.
	if a.Zone() != "" {
		return false
	}
	a = a.Unmap() // ::ffff:10.0.0.1 is 10.0.0.1
	if !a.IsGlobalUnicast() || a.IsPrivate() {
		return false // loopback, link-local, multicast, unspecified, 10/8, 172.16/12, 192.168/16, fc00::/7
	}
	for _, p := range reserved {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// reserved holds the special-purpose ranges that Public's two netip checks do
// not cover, from IANA's IPv4 and IPv6 Special-Purpose Address Registries. An
// IPv6 range that carries an IPv4 address inside is here because that IPv4
// address can be a private one.
var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // shared address space: carrier NAT, Tailscale
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, and the broadcast address
	netip.MustParsePrefix("::/96"),           // IPv4-compatible, deprecated: IPv4 inside
	netip.MustParsePrefix("::ffff:0:0:0/96"), // IPv4-translated: IPv4 inside
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64: IPv4 inside
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local NAT64
	netip.MustParsePrefix("100::/64"),        // discard-only
	netip.MustParsePrefix("2001::/32"),       // Teredo: IPv4 inside
	netip.MustParsePrefix("2001:2::/48"),     // benchmarking
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("2002::/16"),       // 6to4: IPv4 inside
	netip.MustParsePrefix("3fff::/20"),       // documentation
	netip.MustParsePrefix("fec0::/10"),       // site-local, deprecated
}
