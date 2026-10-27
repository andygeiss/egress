package proxy

import (
	"errors"
	"net/netip"
	"testing"
)

func TestPublic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		addr string
		want bool
	}{
		// The public internet, including the neighbours of every refused range.
		{"1.1.1.1", true},
		{"8.8.8.8", true},
		{"9.255.255.255", true},
		{"11.0.0.0", true},
		{"100.63.255.255", true},
		{"100.128.0.0", true},
		{"172.15.255.255", true},
		{"172.32.0.0", true},
		{"192.88.98.255", true},
		{"192.88.100.0", true},
		{"198.17.255.255", true},
		{"198.20.0.0", true},
		{"2001:200::1", true},
		{"2606:4700:4700::1111", true},
		{"::ffff:1.1.1.1", true},

		// The machine itself.
		{"127.0.0.1", false},
		{"127.255.255.254", false},
		{"::1", false},
		{"0.0.0.0", false},
		{"::", false},

		// The networks it sits in.
		{"10.0.0.1", false},
		{"172.16.0.1", false},
		{"172.31.255.255", false},
		{"192.168.1.1", false},
		{"192.168.127.254", false}, // the podman machine's way back to the Mac
		{"fc00::1", false},
		{"fd12:3456::1", false},

		// Link-local and multicast.
		{"169.254.169.254", false}, // cloud metadata
		{"fe80::1", false},
		{"224.0.0.1", false},
		{"ff02::1", false},

		// Shared, reserved and special-purpose ranges.
		{"0.1.2.3", false},
		{"100.64.0.1", false},
		{"100.127.255.254", false},
		{"192.0.0.8", false},
		{"192.0.2.1", false},
		{"192.88.99.1", false},
		{"192.88.99.2", false},
		{"198.18.0.1", false},
		{"198.19.255.255", false},
		{"198.51.100.1", false},
		{"203.0.113.1", false},
		{"240.0.0.1", false},
		{"255.255.255.255", false},
		{"100::1", false},
		{"100:0:0:1::1", false},
		{"2001:2::1", false},
		{"2001:10::1", false},
		{"2001:1ff::1", false}, // the last /32 in 2001::/23
		{"2001:db8::1", false},
		{"3fff::1", false},
		{"5f00::1", false},
		{"fec0::1", false},

		// An IPv4 address inside an IPv6 one.
		{"::ffff:10.0.0.1", false},
		{"::ffff:127.0.0.1", false},
		{"::10.0.0.1", false},
		{"::ffff:0:10.0.0.1", false},
		{"64:ff9b::10.0.0.1", false},
		{"64:ff9b:1::1", false},
		{"2001::1", false},
		{"2002:a00:1::1", false},

		// An address with a zone, which netip's range checks skip.
		{"::%en0", false}, // dials ::1, the machine itself
		{"::%1", false},
		{"64:ff9b::a00:1%en0", false},
		{"2002:a00:1::1%en0", false},
		{"fec0::1%en0", false},
		{"2606:4700:4700::1111%en0", false}, // a public address never needs a zone
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			t.Parallel()
			if got := Public(netip.MustParseAddr(tt.addr)); got != tt.want {
				t.Errorf("Public(%s) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestPublicHTTPS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		addr   string
		refuse bool
	}{
		{"port 443 on a public address goes through", "1.1.1.1:443", false},
		{"another port is refused", "1.1.1.1:80", true},
		{"a private address is refused", "10.0.0.1:443", true},
		{"a mapped private address is refused", "[::ffff:192.168.1.1]:443", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := PublicHTTPS(netip.MustParseAddrPort(tt.addr))
			if _, refused := errors.AsType[*Refusal](err); refused != tt.refuse || (err != nil) != tt.refuse {
				t.Errorf("PublicHTTPS(%s) = %v, want a refusal: %v", tt.addr, err, tt.refuse)
			}
		})
	}
}
