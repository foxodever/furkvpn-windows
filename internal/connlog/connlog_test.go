package connlog

import (
	"fmt"
	"testing"

	"furkvpn/internal/core"
)

func newLog() *Log {
	l := New()
	l.SetOutbounds(map[string]string{"proxy": "vless", "direct": "freedom", "block": "blackhole", "dns-out": "dns", "apps-loop": "loopback"})
	return l
}

func TestRoutesFollowOutboundProtocols(t *testing.T) {
	l := newLog()
	check := func(tag string, want Route, wantOK bool) {
		t.Helper()
		if got, ok := l.RouteOf(tag); got != want || ok != wantOK {
			t.Errorf("%s: %v %v", tag, got, ok)
		}
	}
	check("proxy", VPN, true)
	check("direct", Direct, true)
	check("block", Blocked, true)
	check("dns-out", "", false)
	// Picked by a balancer, not listed by protocol: still a proxy.
	check("auto-3", VPN, true)
}

func TestConnectionsAreGroupedByProgramAndAddress(t *testing.T) {
	l := newLog()
	l.Record("exe:telegram", `C:\T\Telegram.exe`, "Telegram", "program", "api.telegram.org", 443, false, VPN, 1000)
	l.Record("exe:telegram", `C:\T\Telegram.exe`, "Telegram", "program", "api.telegram.org", 443, false, VPN, 2000)
	l.Record("exe:app", "", "App", "program", "ya.ru", 443, true, Direct, 3000)
	// In the order of appearance, so the list doesn't jump while it updates.
	apps := l.Snapshot()
	if len(apps) != 2 || apps[0].Label != "Telegram" || apps[1].Label != "App" {
		t.Fatalf("%+v", apps)
	}
	e := apps[0].Entries[0]
	if len(apps[0].Entries) != 1 || e.Count != 2 || e.FirstSeen != 1000 || e.LastSeen != 2000 {
		t.Fatalf("%+v", e)
	}
	if !apps[1].Entries[0].UDP {
		t.Fatal("udp")
	}
}

func TestOldestAddressesAreForgotten(t *testing.T) {
	l := newLog()
	for i := 0; i <= MaxEntries; i++ {
		l.Record("exe:1", "", "App", "program", fmt.Sprintf("host%d.example", i), 443, false, VPN, int64(i))
	}
	entries := l.Snapshot()[0].Entries
	if len(entries) > MaxEntries {
		t.Fatalf("%d entries", len(entries))
	}
	if entries[len(entries)-1].Host != fmt.Sprintf("host%d.example", MaxEntries) || entries[0].Host == "host0.example" {
		t.Fatal("the oldest go first")
	}
}

type fakeResolver struct{}

func (fakeResolver) Owner(network, srcIP string, srcPort int, dstIP string, dstPort int) Owner {
	return Owner{Key: "exe:chrome", Path: `C:\Chrome\chrome.exe`, Label: "Google Chrome", Kind: "program"}
}

func TestListener(t *testing.T) {
	l := newLog()
	listen := l.Listener(fakeResolver{})
	listen(core.ConnEvent{Network: "tcp", SrcIP: "172.19.0.1", SrcPort: 5000, DstIP: "1.2.3.4", DstPort: 443, Domain: "example.com", Inbound: "tun-in", Outbound: "proxy"})
	listen(core.ConnEvent{Network: "udp", SrcIP: "192.168.1.7", SrcPort: 5000, DstIP: "8.8.8.8", DstPort: 53, Inbound: "share-socks", Outbound: "direct"})
	listen(core.ConnEvent{Network: "udp", SrcIP: "172.19.0.1", SrcPort: 5001, DstIP: "172.19.0.2", DstPort: 53, Inbound: "tun-in", Outbound: "dns-out"})
	// Only the chosen programs through VPN: their connections are seen twice, on the way to the
	// loopback (not a route) and back from it.
	listen(core.ConnEvent{Network: "tcp", SrcIP: "172.19.0.1", SrcPort: 5002, DstIP: "5.6.7.8", DstPort: 443, Domain: "chosen.example", Inbound: "tun-in", Outbound: "apps-loop"})
	listen(core.ConnEvent{Network: "tcp", SrcIP: "172.19.0.1", SrcPort: 5002, DstIP: "5.6.7.8", DstPort: 443, Domain: "chosen.example", Inbound: "apps-in", Outbound: "proxy"})
	apps := l.Snapshot()
	if len(apps) != 2 || apps[0].Label != "Google Chrome" {
		t.Fatalf("%+v", apps)
	}
	hosts := map[string]Entry{}
	for _, e := range apps[0].Entries {
		hosts[e.Host] = e
	}
	if len(hosts) != 2 || hosts["example.com"].Route != VPN {
		t.Fatalf("%+v", apps[0].Entries)
	}
	if e := hosts["chosen.example"]; e.Count != 1 || e.Route != VPN {
		t.Fatalf("the chosen program's connection is one entry through VPN: %+v", e)
	}
	if apps[1].Kind != "device" || apps[1].Label != "192.168.1.7" || apps[1].Entries[0].Route != Direct || apps[1].Entries[0].Host != "8.8.8.8" {
		t.Fatalf("%+v", apps[1])
	}
}
