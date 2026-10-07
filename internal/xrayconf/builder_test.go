package xrayconf

import (
	"reflect"
	"slices"
	"testing"

	"furkvpn/internal/jsonx"
)

const raw = `
{"remarks":"x","inbounds":[{"tag":"socks","protocol":"socks","port":10808,"sniffing":{"enabled":true}}],
 "outbounds":[{"tag":"proxy","protocol":"vless"},{"tag":"direct","protocol":"freedom"}],
 "routing":{"rules":[{"type":"field","domain":["keyword:yandex"],"outboundTag":"direct"}]},
 "dns":{"servers":["77.88.8.8"],"queryStrategy":"UseIP"},
 "api":{"tag":"api","listen":"0.0.0.0:10085","services":["HandlerService"]},
 "observatory":{"subjectSelector":["proxy"]}}`

func build(t *testing.T, config string, opt Options) jsonx.Object {
	t.Helper()
	out, err := Build([]byte(config), opt)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := jsonx.ParseObject(out)
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

func rules(cfg jsonx.Object) []jsonx.Object {
	return jsonx.Objects(jsonx.Obj(cfg["routing"])["rules"])
}

func tags(list []jsonx.Object) []string {
	var out []string
	for _, o := range list {
		out = append(out, jsonx.S(o, "tag"))
	}
	return out
}

func TestTunInboundDNSHijackAndNoAccessLog(t *testing.T) {
	cfg := build(t, raw, Options{LogLevel: "error"})
	inbounds := jsonx.Objects(cfg["inbounds"])
	if !reflect.DeepEqual(tags(inbounds), []string{"tun-in"}) {
		t.Fatalf("inbounds %v", tags(inbounds))
	}
	settings := jsonx.Obj(inbounds[0]["settings"])
	if jsonx.S(settings, "autoOutboundsInterface") != "auto" ||
		!reflect.DeepEqual(jsonx.Strings(settings["autoSystemRoutingTable"]), []string{"0.0.0.0/0", "::/0"}) ||
		!reflect.DeepEqual(jsonx.Strings(settings["autoSystemWfpBlockLeak"]), []string{"dns"}) ||
		!reflect.DeepEqual(jsonx.Strings(settings["dns"]), []string{TunDNS}) {
		t.Fatalf("tun settings %v", settings)
	}
	// The template's sniffing is kept.
	if !reflect.DeepEqual(jsonx.Obj(inbounds[0]["sniffing"]), jsonx.Object{"enabled": true}) {
		t.Fatalf("sniffing %v", inbounds[0]["sniffing"])
	}
	if first := rules(cfg)[0]; jsonx.S(first, "outboundTag") != "dns-out" || jsonx.S(first, "port") != "53" {
		t.Fatalf("first rule %v", first)
	}
	if jsonx.S(jsonx.Obj(cfg["dns"]), "queryStrategy") != "UseIPv4" {
		t.Fatal("query strategy")
	}
	if jsonx.S(jsonx.Obj(cfg["log"]), "access") != "none" {
		t.Fatal("access log")
	}
	for _, key := range []string{"remarks", "api"} {
		if _, ok := cfg[key]; ok {
			t.Errorf("%s must be dropped", key)
		}
	}
	if _, ok := cfg["observatory"]; !ok {
		t.Error("observatory must stay: auto-select needs it")
	}
}

func TestShareProxyServesOnlyLocalNetwork(t *testing.T) {
	cfg := build(t, raw, Options{Share: &ShareProxy{HTTPPort: 8080, SocksPort: 1080}})
	inbounds := jsonx.Objects(cfg["inbounds"])
	if !reflect.DeepEqual(tags(inbounds), []string{"tun-in", "share-http", "share-socks"}) {
		t.Fatalf("%v", tags(inbounds))
	}
	for _, in := range inbounds[1:] {
		if jsonx.S(in, "listen") != "0.0.0.0" {
			t.Fatal("share proxy must listen on all interfaces")
		}
	}
	block := rules(cfg)[0]
	if jsonx.S(block, "outboundTag") != "share-block" {
		t.Fatalf("%v", block)
	}
	sources := jsonx.Strings(block["source"])
	if !slices.Contains(sources, "112.0.0.0/4") || !slices.Contains(sources, "::/1") {
		t.Fatal("loopback (127.0.0.0/8 is inside 112.0.0.0/4) must be blocked")
	}
	for _, s := range sources {
		if s[:3] == "10." || len(s) > 8 && s[:8] == "192.168." {
			t.Fatalf("private range %s blocked", s)
		}
	}
	if !slices.Contains(tags(jsonx.Objects(cfg["outbounds"])), "share-block") {
		t.Fatal("share-block outbound")
	}
}

func TestShareProxyWithPassword(t *testing.T) {
	cfg := build(t, raw, Options{Share: &ShareProxy{HTTPPort: 18080, SocksPort: 11080, User: "furk_1", Password: "secret"}})
	var socks jsonx.Object
	for _, in := range jsonx.Objects(cfg["inbounds"]) {
		if jsonx.S(in, "tag") == "share-socks" {
			socks = in
		}
	}
	settings := jsonx.Obj(socks["settings"])
	if jsonx.S(socks, "port") != "11080" || jsonx.S(settings, "auth") != "password" {
		t.Fatalf("%v", socks)
	}
	if user := jsonx.S(jsonx.Objects(settings["accounts"])[0], "user"); user != "furk_1" {
		t.Fatal(user)
	}
}

const auto = `
{"outbounds":[{"tag":"proxy-1","protocol":"vless"},{"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"}],
 "routing":{"rules":[{"type":"field","protocol":["bittorrent"],"outboundTag":"direct"},
                     {"type":"field","network":"tcp,udp","balancerTag":"Super_Balancer"}],
            "balancers":[{"tag":"Super_Balancer","selector":["proxy"]}]}}`

func TestUserRulesGoBeforeServerRulesAndUseTheBalancer(t *testing.T) {
	withDot := build(t, auto, Options{Routing: &Routing{Enabled: true, Direct: []string{"domain:.com", "keyword:yandex"}}})
	var directDomains []string
	for _, r := range rules(withDot) {
		if jsonx.S(r, "outboundTag") == "direct" && r["domain"] != nil {
			directDomains = jsonx.Strings(r["domain"])
		}
	}
	if !reflect.DeepEqual(directDomains, []string{"domain:com", "keyword:yandex"}) {
		t.Fatalf("%v", directDomains)
	}

	cfg := build(t, auto, Options{Routing: &Routing{
		Enabled: true, LANDirect: true,
		Direct: []string{"domain:sber.ru"}, Proxy: []string{"domain:yandex.ru"}, Block: []string{"domain:ads.example", "1.2.3.4"},
	}})
	r := rules(cfg)
	want := []string{"dns-out", "block", "block", "direct", "direct"}
	for i, tag := range want {
		if got := jsonx.S(r[i], "outboundTag"); got != tag {
			t.Fatalf("rule %d: %s, want %s (%v)", i, got, tag, r[i])
		}
	}
	if !slices.Contains(jsonx.Strings(r[4]["ip"]), "192.168.0.0/16") {
		t.Fatal("LAN ranges go direct")
	}
	if jsonx.S(r[5], "balancerTag") != "Super_Balancer" {
		t.Fatalf("through VPN goes to the balancer: %v", r[5])
	}
	if jsonx.Strings(r[6]["protocol"])[0] != "bittorrent" {
		t.Fatal("then the panel's rules")
	}
	// Turned off: none of the user's rules.
	off := build(t, auto, Options{Routing: &Routing{Enabled: false, Block: []string{"1.2.3.4"}}})
	if len(rules(off)) != 3 {
		t.Fatalf("%v", rules(off))
	}
}

func TestExcludedProgramsGoAroundEverything(t *testing.T) {
	cfg := build(t, raw, Options{
		Excluded: []string{"Telegram.exe", "C:\\Games\\steam.EXE"},
		Routing:  &Routing{Enabled: true, Block: []string{"domain:telegram.org"}},
	})
	r := rules(cfg)
	if jsonx.S(r[0], "outboundTag") != "dns-out" {
		t.Fatal("DNS first: the DNS Client service asks for every program")
	}
	process := jsonx.Strings(r[1]["process"])
	if !reflect.DeepEqual(process, []string{"Telegram", "telegram", "steam"}) {
		t.Fatalf("%v", process)
	}
	if jsonx.S(r[1], "outboundTag") != "direct" || !reflect.DeepEqual(jsonx.Strings(r[1]["inboundTag"]), []string{"tun-in"}) {
		t.Fatalf("%v", r[1])
	}
	if jsonx.S(r[2], "outboundTag") != "user-block" {
		t.Fatalf("then the user's rules: %v", r[2])
	}
	if !slices.Contains(tags(jsonx.Objects(cfg["outbounds"])), "user-block") {
		t.Fatal("a blackhole is added when the template has none")
	}
}

func TestFallbackIsTheServerThatWasBestAMomentAgo(t *testing.T) {
	config := `
{"outbounds":[{"tag":"proxy","protocol":"vless"},{"tag":"proxy-2","protocol":"vless"},{"tag":"direct","protocol":"freedom"}],
 "routing":{"rules":[{"type":"field","network":"tcp,udp","balancerTag":"Super_Balancer"}],
            "balancers":[{"tag":"Super_Balancer","selector":["proxy"],"fallbackTag":"proxy"},
                         {"tag":"Other","selector":["eu-"]}]}}`
	balancers := func(fallback string) []jsonx.Object {
		return jsonx.Objects(jsonx.Obj(build(t, config, Options{Fallback: fallback})["routing"])["balancers"])
	}
	patched := balancers("proxy-2")
	if jsonx.S(patched[0], "fallbackTag") != "proxy-2" {
		t.Fatal("patched")
	}
	if _, ok := patched[1]["fallbackTag"]; ok {
		t.Fatal("not one of that balancer's servers")
	}
	if jsonx.S(balancers("gone")[0], "fallbackTag") != "proxy" || jsonx.S(balancers("")[0], "fallbackTag") != "proxy" {
		t.Fatal("unknown or no fallback keeps the template's")
	}
}

func TestDNSGoesOverTCPSoEachQueryPicksALiveServer(t *testing.T) {
	config := `
{"outbounds":[{"tag":"proxy","protocol":"vless"}],
 "dns":{"servers":["9.9.9.9","2606:4700:4700::1111",{"address":"77.88.8.8","port":5353,"domains":["domain:ru"]},
                   "localhost","https://dns.google/dns-query","tcp://1.1.1.1"]}}`
	dns := jsonx.Obj(build(t, config, Options{})["dns"])
	servers := jsonx.Arr(dns["servers"])
	if servers[0] != "tcp://9.9.9.9:53" || servers[1] != "tcp://[2606:4700:4700::1111]:53" {
		t.Fatalf("%v", servers)
	}
	withDomains := jsonx.Obj(servers[2])
	if jsonx.S(withDomains, "address") != "tcp://77.88.8.8:5353" || withDomains["port"] != nil {
		t.Fatalf("%v", withDomains)
	}
	if jsonx.Strings(withDomains["domains"])[0] != "domain:ru" {
		t.Fatal("domains kept")
	}
	if !reflect.DeepEqual(servers[3:], []any{"localhost", "https://dns.google/dns-query", "tcp://1.1.1.1"}) {
		t.Fatalf("%v", servers[3:])
	}
	if dns["serveStale"] != true || jsonx.S(dns, "queryStrategy") != "UseIPv4" {
		t.Fatalf("%v", dns)
	}
}

func TestOutboundProtocols(t *testing.T) {
	out, _ := Build([]byte(raw), Options{})
	got := OutboundProtocols(out)
	if got["proxy"] != "vless" || got["direct"] != "freedom" || got["dns-out"] != "dns" {
		t.Fatalf("%v", got)
	}
}
