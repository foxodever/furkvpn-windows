package core

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xtls/xray-core/transport/internet"

	"furkvpn/internal/jsonx"
)

func mustParse(t *testing.T, s string) jsonx.Object {
	t.Helper()
	cfg, err := jsonx.ParseObject([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

const templateLike = `{
  "dns": {"servers": [
    {"address": "77.88.8.8", "domains": ["full:listed.net"], "skipFallback": true},
    "https://8.8.8.8/dns-query"
  ]},
  "outbounds": [
    {"tag": "proxy", "protocol": "vless", "settings": {"vnext": [{"address": "listed.net", "port": 443}]}},
    {"tag": "proxy-2", "protocol": "vless", "settings": {"vnext": [{"address": "missing.net", "port": 443}]}},
    {"tag": "proxy-3", "protocol": "vless", "settings": {"address": "1.2.3.4", "port": 443}},
    {"tag": "proxy-4", "protocol": "trojan", "settings": {"servers": [{"address": "Other.Missing.lol.", "port": 443}]}},
    {"tag": "direct", "protocol": "freedom"}
  ],
  "routing": {
    "rules": [
      {"type": "field", "ip": ["77.88.8.8/32"], "port": "53", "outboundTag": "direct"},
      {"type": "field", "network": "tcp,udp", "balancerTag": "auto"}
    ],
    "balancers": [{"tag": "auto", "selector": ["proxy"], "strategy": {"type": "leastLoad"}}]
  },
  "burstObservatory": {"subjectSelector": ["proxy"]},
  "inbounds": [{"protocol": "socks", "port": 10808}],
  "stats": {}
}`

func TestServersResolvedDirectly(t *testing.T) {
	cfg := mustParse(t, templateLike)
	if !resolveServersDirectly(cfg) {
		t.Fatal("nothing added")
	}
	servers := cfg["dns"].(map[string]any)["servers"].([]any)
	got := servers[0].(map[string]any)["domains"]
	want := []any{"full:listed.net", "full:missing.net", "full:other.missing.lol"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("domains = %v, want %v", got, want)
	}
	if resolveServersDirectly(cfg) {
		t.Fatal("added twice")
	}
}

func TestServersResolvedDirectlyNeedsAList(t *testing.T) {
	// A config that does not list its servers anywhere is left alone.
	cfg := mustParse(t, `{"dns": {"servers": ["1.1.1.1"]}, "outbounds": [{"protocol": "vless", "settings": {"vnext": [{"address": "a.net"}]}}]}`)
	if resolveServersDirectly(cfg) {
		t.Fatal("changed a config without a server list")
	}
}

func TestWithServersResolvedDirectlyKeepsNumbers(t *testing.T) {
	in := `{"dns": {"servers": [{"address": "77.88.8.8", "domains": ["full:a.net"]}]}, "outbounds": [` +
		`{"protocol": "vless", "settings": {"vnext": [{"address": "a.net", "port": 443}]}},` +
		`{"protocol": "vless", "settings": {"vnext": [{"address": "b.net", "port": 443}]}, "streamSettings": {"xhttpSettings": {"extra": {"scMaxEachPostBytes": 1000000, "big": 12345678901234567890}}}}]}`
	out := string(withServersResolvedDirectly([]byte(in)))
	var cfg map[string]any
	dec := json.NewDecoder(bytesReader(out))
	dec.UseNumber()
	if err := dec.Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	extra := cfg["outbounds"].([]any)[1].(map[string]any)["streamSettings"].(map[string]any)["xhttpSettings"].(map[string]any)["extra"].(map[string]any)
	if extra["scMaxEachPostBytes"].(json.Number) != "1000000" || extra["big"].(json.Number) != "12345678901234567890" {
		t.Fatalf("numbers changed: %v", extra)
	}
	if string(withServersResolvedDirectly([]byte(out))) != out {
		t.Fatal("second pass changed the config")
	}
}

func TestDelayTestConfig(t *testing.T) {
	cfg := mustParse(t, templateLike)
	single := delayTestConfig(cfg, false)
	for _, key := range []string{"inbounds", "stats", "burstObservatory", "dns", "routing"} {
		if _, ok := single[key]; ok {
			t.Errorf("single-server test keeps %s", key)
		}
	}
	auto := delayTestConfig(cfg, true)
	if _, ok := auto["burstObservatory"]; !ok || !hasBalancers(auto) {
		t.Error("auto-select test lost its balancer or checks")
	}
	if _, ok := auto["dns"]; ok {
		t.Error("auto-select test keeps dns")
	}
	if got := auto["routing"].(map[string]any)["domainStrategy"]; got != "AsIs" {
		t.Errorf("routing domainStrategy = %v, want AsIs", got)
	}
	if _, ok := cfg["routing"].(map[string]any)["domainStrategy"]; ok {
		t.Error("the original config was changed")
	}
}

// fakeLookup puts answers into the lookup cache so tests do not depend on DNS.
func fakeLookup(t *testing.T, answers map[string]string) {
	t.Helper()
	lookupMu.Lock()
	defer lookupMu.Unlock()
	for domain, ip := range answers {
		lookupCache[domain] = cachedLookup{net.ParseIP(ip), time.Now().Add(time.Minute)}
	}
	t.Cleanup(func() {
		lookupMu.Lock()
		defer lookupMu.Unlock()
		for domain := range answers {
			delete(lookupCache, domain)
		}
	})
}

func TestPinServers(t *testing.T) {
	fakeLookup(t, map[string]string{"reality.test": "10.0.0.1", "tls.test": "10.0.0.2", "ws.test": "10.0.0.3", "wg.test": "10.0.0.4"})
	cfg := mustParse(t, `{"outbounds": [
	  {"protocol": "vless", "settings": {"vnext": [{"address": "reality.test", "port": 443}]},
	   "streamSettings": {"network": "grpc", "security": "reality", "realitySettings": {"serverName": "cover.example"}}},
	  {"protocol": "vless", "settings": {"address": "tls.test", "port": 443},
	   "streamSettings": {"network": "xhttp", "security": "tls", "xhttpSettings": {"path": "/x"}}},
	  {"protocol": "trojan", "settings": {"servers": [{"address": "ws.test", "port": 443}]},
	   "streamSettings": {"network": "ws", "security": "tls", "tlsSettings": {"serverName": "sni.example"}, "wsSettings": {"host": "front.example"}}},
	  {"protocol": "wireguard", "settings": {"peers": [{"endpoint": "wg.test:51820"}]}},
	  {"protocol": "vless", "settings": {"vnext": [{"address": "1.2.3.4", "port": 443}]}}
	]}`)
	if err := pinServers(cfg, proxyOutbounds(cfg)); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cfg["outbounds"])
	got := string(b)
	for _, want := range []string{
		`"address":"10.0.0.1"`, `"serverName":"cover.example"`, `"authority":"reality.test"`,
		`"address":"10.0.0.2"`, `"tlsSettings":{"serverName":"tls.test"}`, `"host":"tls.test"`,
		`"address":"10.0.0.3"`, `"serverName":"sni.example"`, `"host":"front.example"`,
		`"endpoint":"10.0.0.4:51820"`, `"address":"1.2.3.4"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

func TestPinServersReportsFailures(t *testing.T) {
	cfg := mustParse(t, `{"outbounds": [{"protocol": "vless", "settings": {"vnext": [{"address": "no-such-host.invalid", "port": 443}]}}]}`)
	if err := pinServers(cfg, proxyOutbounds(cfg)); err == nil {
		t.Fatal("no error for an unknown host")
	}
}

func TestListedNameServer(t *testing.T) {
	cfg := mustParse(t, `{"dns": {"servers": [
	  "https://8.8.8.8/dns-query",
	  {"address": "https://1.1.1.1/dns-query", "domains": ["full:doh.test"]},
	  {"address": "77.88.8.8", "domains": ["full:a.test", "domain:b.test"]},
	  {"address": "tcp://9.9.9.9:5353", "domains": ["keyword:kw"]}
	]}}`)
	for domain, want := range map[string]string{
		"a.test": "udp 77.88.8.8:53", "x.b.test": "udp 77.88.8.8:53", "some-kw.net": "tcp 9.9.9.9:5353",
		"doh.test": " ", "other.test": " ",
	} {
		network, address := listedNameServer(cfg, domain)
		if got := network + " " + address; got != want {
			t.Errorf("%s: %q, want %q", domain, got, want)
		}
	}
}

func TestProbeURL(t *testing.T) {
	cfg := mustParse(t, `{"burstObservatory": {"pingConfig": {"destination": "http://cp.example/generate_204"}}}`)
	if got := probeURL(cfg, "https://default"); got != "http://cp.example/generate_204" {
		t.Errorf("burst: %s", got)
	}
	cfg = mustParse(t, `{"observatory": {"probeURL": "https://probe.example/"}}`)
	if got := probeURL(cfg, "https://default"); got != "https://probe.example/" {
		t.Errorf("observatory: %s", got)
	}
	if got := probeURL(mustParse(t, `{}`), "https://default"); got != "https://default" {
		t.Errorf("none: %s", got)
	}
}

func TestTagOutbounds(t *testing.T) {
	cfg := mustParse(t, `{"outbounds": [{"protocol": "freedom"}, {"protocol": "vless", "tag": "x"}, {"protocol": "vless"}]}`)
	if got := tagOutbounds(cfg, []int{1, 2}); !reflect.DeepEqual(got, []string{"x", "furk-test-2"}) {
		t.Fatalf("tags = %v", got)
	}
}

func TestHTTPDelayUsesTheGivenOutbound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer srv.Close()
	// "block" is first, so routing alone would send everything there.
	cfg := mustParse(t, `{"outbounds": [{"tag": "block", "protocol": "blackhole"}, {"tag": "direct", "protocol": "freedom"}]}`)
	results, err := measureTags(delayTestConfig(cfg, false), []string{"direct", "block"}, srv.URL, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].err != nil || results[0].delay < 0 {
		t.Errorf("through direct: %d, %v", results[0].delay, results[0].err)
	}
	if results[1].err == nil {
		t.Error("through block: no error")
	}
}

func bytesReader(s string) *strings.Reader { return strings.NewReader(s) }

// A delay test creates its own instance; afterwards the running VPN must still resolve its
// servers through its own DNS, not through the closed test instance.
func TestDelayTestLeavesTheRunningInstanceItsDNS(t *testing.T) {
	err := Start([]byte(`{"log": {"loglevel": "none"}, "dns": {"hosts": {"server.running.test": "10.1.1.1"}},
	  "outbounds": [{"tag": "direct", "protocol": "freedom"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer Stop()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer srv.Close()
	cfg := mustParse(t, `{"outbounds": [{"tag": "direct", "protocol": "freedom"}]}`)
	if _, err := measureTags(delayTestConfig(cfg, false), []string{"direct"}, srv.URL, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	ips, err := internet.LookupForIP("server.running.test", internet.DomainStrategy_USE_IP, nil)
	if err != nil || len(ips) != 1 || ips[0].String() != "10.1.1.1" {
		t.Fatalf("lookup after a delay test: %v, %v", ips, err)
	}
}
