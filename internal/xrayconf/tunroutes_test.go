package xrayconf

import (
	"net/netip"
	"testing"
)

func inRoutes(ip string, routes []any) bool {
	addr := netip.MustParseAddr(ip)
	for _, r := range routes {
		if netip.MustParsePrefix(r.(string)).Contains(addr) {
			return true
		}
	}
	return false
}

func TestLANAndMulticastStayOutOfTheTun(t *testing.T) {
	routes := tunRoutes(true, true)
	for _, local := range []string{"192.168.1.50", "10.0.0.1", "172.16.5.5", "172.31.255.255", "169.254.1.1",
		"224.0.0.251", "239.255.255.250", "255.255.255.255", "fe80::1", "fd00::1", "ff02::fb"} {
		if inRoutes(local, routes) {
			t.Errorf("%s goes into the TUN", local)
		}
	}
	for _, internet := range []string{"8.8.8.8", "1.1.1.1", "172.15.255.255", "172.32.0.1", "100.64.0.1",
		"223.255.255.255", "77.88.8.8", "2a00:1450:4010::1", "2001:4860:4860::8888"} {
		if !inRoutes(internet, routes) {
			t.Errorf("%s misses the TUN", internet)
		}
	}
	if len(routes) >= 70 {
		t.Errorf("%d routes", len(routes))
	}
	if all := tunRoutes(true, false); len(all) != 2 || all[0] != "0.0.0.0/0" || all[1] != "::/0" {
		t.Errorf("without LAN directly: %v", all)
	}
	if v4 := tunRoutes(false, true); inRoutes("2001:4860:4860::8888", v4) || !inRoutes("8.8.8.8", v4) {
		t.Error("IPv6 routes without IPv6")
	}
}
