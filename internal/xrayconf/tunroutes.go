package xrayconf

import (
	"math/big"
	"net/netip"
)

// What the TUN takes: everything, or with "LAN directly" on (Settings → Маршрутизация, on by
// default) everything but the local network. Leaving it out of the routes, not only sending it
// direct in Xray, keeps it off the TUN altogether: networks behind the router other than the
// computer's own subnet, link-local, multicast and broadcast. Port of the Android app's TunRoutes
// (1.7.1). The TUN's own subnet with its DNS server stays reachable through the adapter's address.

// Home networks, link-local, multicast and the reserved block with the broadcast address.
var (
	localIPv4 = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "224.0.0.0/4", "240.0.0.0/4"}
	localIPv6 = []string{"fc00::/7", "fe80::/10", "ff00::/8"}
)

// tunRoutes are the TUN's routes (autoSystemRoutingTable).
func tunRoutes(ipv6, lanDirect bool) []any {
	v4, v6 := []string{"0.0.0.0/0"}, []string{"::/0"}
	if lanDirect {
		v4, v6 = complementRoutes(32, localIPv4), complementRoutes(128, localIPv6)
	}
	routes := toAny(v4)
	if ipv6 {
		routes = append(routes, toAny(v6)...)
	}
	return routes
}

// complementRoutes is the whole address space without excluded, in as few prefixes as possible.
func complementRoutes(bits int, excluded []string) []string {
	type network struct {
		addr *big.Int
		len  int
	}
	var local []network
	for _, cidr := range excluded {
		p := netip.MustParsePrefix(cidr)
		local = append(local, network{new(big.Int).SetBytes(p.Addr().AsSlice()), p.Bits()})
	}
	sameTop := func(a, b *big.Int, length int) bool {
		return new(big.Int).Rsh(a, uint(bits-length)).Cmp(new(big.Int).Rsh(b, uint(bits-length))) == 0
	}
	var routes []string
	var walk func(net *big.Int, length int)
	walk = func(net *big.Int, length int) {
		inner := false
		for _, l := range local {
			if l.len <= length && sameTop(net, l.addr, l.len) {
				return // inside a local range
			}
			if l.len > length && sameTop(net, l.addr, length) {
				inner = true
			}
		}
		if !inner {
			b := make([]byte, bits/8)
			net.FillBytes(b)
			addr, _ := netip.AddrFromSlice(b)
			routes = append(routes, netip.PrefixFrom(addr, length).String())
			return
		}
		walk(net, length+1)
		walk(new(big.Int).SetBit(new(big.Int).Set(net), bits-length-1, 1), length+1)
	}
	walk(new(big.Int), 0)
	return routes
}
