package core

import (
	"net/url"
	"testing"

	xlog "github.com/xtls/xray-core/common/log"
	xnet "github.com/xtls/xray-core/common/net"
)

func TestSplitDetour(t *testing.T) {
	cases := map[string][2]string{
		"tun-in -> proxy":     {"tun-in", "proxy"},
		"tun-in >> direct":    {"tun-in", "direct"},
		"tun-in ==> platform": {"tun-in", "platform"},
		"proxy":               {"", "proxy"},
	}
	for in, want := range cases {
		if a, b := splitDetour(in); a != want[0] || b != want[1] {
			t.Errorf("splitDetour(%q) = %q, %q", in, a, b)
		}
	}
}

func TestEventUsesDNSNames(t *testing.T) {
	dnsNames.put("142.250.1.1", "www.google.com")
	e, ok := eventOf(&xlog.AccessMessage{
		From:   xnet.TCPDestination(xnet.ParseAddress("172.19.0.1"), 40000),
		To:     xnet.TCPDestination(xnet.ParseAddress("142.250.1.1"), 443),
		Status: xlog.AccessAccepted,
		Detour: "tun-in -> proxy",
	})
	want := ConnEvent{Network: "tcp", SrcIP: "172.19.0.1", SrcPort: 40000, DstIP: "142.250.1.1", DstPort: 443,
		Domain: "www.google.com", Inbound: "tun-in", Outbound: "proxy"}
	if !ok || e != want {
		t.Fatalf("unexpected event %+v", e)
	}
}

func TestEventWithDomainAndURL(t *testing.T) {
	e, ok := eventOf(&xlog.AccessMessage{
		To:     xnet.UDPDestination(xnet.ParseAddress("example.com"), 443),
		Detour: "share-socks >> direct",
	})
	if !ok || e.Network != "udp" || e.DstIP != "" || e.Domain != "example.com" || e.Outbound != "direct" {
		t.Fatalf("unexpected event %+v", e)
	}
	u, _ := url.Parse("http://example.org/path")
	e, ok = eventOf(&xlog.AccessMessage{To: u, Detour: "share-http -> proxy"})
	if !ok || e.Domain != "example.org" || e.DstPort != 80 {
		t.Fatalf("unexpected event %+v", e)
	}
	if _, ok := eventOf(&xlog.AccessMessage{To: ""}); ok {
		t.Fatal("empty destination must be skipped")
	}
}

func TestNameCacheEvictsOldest(t *testing.T) {
	c := newNameCache(2)
	c.put("1.1.1.1", "a")
	c.put("2.2.2.2", "b")
	c.put("3.3.3.3", "c")
	if c.get("1.1.1.1") != "" || c.get("2.2.2.2") != "b" || c.get("3.3.3.3") != "c" {
		t.Fatalf("unexpected cache %v", c.names)
	}
}
