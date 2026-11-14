// Package xrayconf turns a server config from the subscription into a config the Windows app
// can run: the panel's local socks/http inbounds are replaced by Xray's own TUN inbound (Wintun),
// which also sets up the system routes and keeps DNS from leaking past it, or, without the TUN
// (Settings → Расширенные настройки), by a proxy on 127.0.0.1; DNS from the TUN is answered by
// Xray's DNS module, traffic counters are enabled. Everything else — outbounds, routing rules,
// balancers, observatory — is used exactly as the panel sent it.
package xrayconf

import (
	"errors"
	"strconv"
	"strings"

	"furkvpn/internal/jsonx"
	"furkvpn/internal/routing"
	"furkvpn/internal/subs"
)

const (
	TunTag        = "tun-in"
	LocalTag      = "local-in"
	DNSOutTag     = "dns-out"
	ShareHTTPTag  = "share-http"
	ShareSocksTag = "share-socks"
	ShareBlockTag = "share-block"
	UserDirectTag = "user-direct"
	UserBlockTag  = "user-block"
	MTU           = 1500
	AdapterName   = "FurkVPN"
	TunAddress4   = "172.19.0.1/30"
	TunAddress6   = "fdfe:dcba:9876::1/126"
	TunDNS        = "172.19.0.2"
)

// Tun is the VPN adapter.
type Tun struct {
	MTU int
	// IPv6 goes into the adapter as well; otherwise it is blocked past it.
	IPv6 bool
	// DNS may only go through the adapter.
	StrictDNS bool
}

// ShareProxy is "Раздача интернета": a proxy for other devices on the same network.
type ShareProxy struct {
	HTTPPort  int
	SocksPort int
	User      string
	Password  string
}

func (s ShareProxy) auth() bool { return s.User != "" && s.Password != "" }

// Routing is the user's own rules (Settings → Маршрутизация).
type Routing struct {
	Enabled   bool
	LANDirect bool
	RUDirect  bool
	Direct    []string
	Proxy     []string
	Block     []string
}

func (r Routing) rules(a routing.Action) []string {
	switch a {
	case routing.Direct:
		return r.Direct
	case routing.Proxy:
		return r.Proxy
	}
	return r.Block
}

// Options of a build.
type Options struct {
	LogLevel string
	// The VPN adapter; without it (nil) the computer's programs reach the VPN through a proxy
	// (SOCKS5 and HTTP) on 127.0.0.1:LocalPort.
	Tun       *Tun
	LocalPort int
	Share     *ShareProxy
	Routing   *Routing
	// Programs that go around the VPN (exe names).
	Excluded []string
	// The server auto-select found best a moment ago: its first choice until the first checks end.
	Fallback string
	// File for Xray's own errors (never the visited addresses); none when empty.
	ErrorLog string
}

var ruleConditions = map[string]bool{
	"domain": true, "ip": true, "port": true, "sourcePort": true, "source": true, "protocol": true,
	"inboundTag": true, "user": true, "attrs": true, "process": true,
}

// Everything outside private networks (RFC 1918, CGNAT, link-local, IPv6 ULA and link-local),
// including loopback: the share proxy only serves devices on the local network.
var nonLocalSources = []string{
	"0.0.0.0/5", "8.0.0.0/7", "11.0.0.0/8", "12.0.0.0/6", "16.0.0.0/4", "32.0.0.0/3", "64.0.0.0/3",
	"96.0.0.0/6", "100.0.0.0/10", "100.128.0.0/9", "101.0.0.0/8", "102.0.0.0/7", "104.0.0.0/5",
	"112.0.0.0/4", "128.0.0.0/3", "160.0.0.0/5", "168.0.0.0/8", "169.0.0.0/9", "169.128.0.0/10",
	"169.192.0.0/11", "169.224.0.0/12", "169.240.0.0/13", "169.248.0.0/14", "169.252.0.0/15",
	"169.255.0.0/16", "170.0.0.0/7", "172.0.0.0/12", "172.32.0.0/11", "172.64.0.0/10", "172.128.0.0/9",
	"173.0.0.0/8", "174.0.0.0/7", "176.0.0.0/4", "192.0.0.0/9", "192.128.0.0/11", "192.160.0.0/13",
	"192.169.0.0/16", "192.170.0.0/15", "192.172.0.0/14", "192.176.0.0/12", "192.192.0.0/10",
	"193.0.0.0/8", "194.0.0.0/7", "196.0.0.0/6", "200.0.0.0/5", "208.0.0.0/4", "224.0.0.0/3",
	"::/1", "8000::/2", "c000::/3", "e000::/4", "f000::/5", "f800::/6", "fe00::/9", "fec0::/10", "ff00::/8",
}

// DNSLevel is the policy level of the DNS outbound, idle for DNSIdleSeconds at most. Xray keeps
// every DNS query of a program (each comes from a port of its own) as a connection until it has
// been idle for the level's connIdle: 300 seconds by default, and with the way it counts, up to
// twice that. At a few queries a second that is thousands of finished queries held in memory.
// Their own level ends them in seconds and leaves the timeout of everything else as it is.
const (
	DNSLevel       = 53
	DNSIdleSeconds = 10
)

// withDNSLevel puts a DNS outbound on DNSLevel unless the panel gave it a level of its own.
func withDNSLevel(ob jsonx.Object) jsonx.Object {
	settings := jsonx.Clone(jsonx.Obj(ob["settings"]))
	if _, ok := settings["userLevel"]; ok {
		return ob
	}
	settings["userLevel"] = DNSLevel
	ob = jsonx.Clone(ob)
	ob["settings"] = settings
	return ob
}

// Sections of a subscription config the app never runs: they could listen on ports, expose
// control APIs or write files (the service runs as SYSTEM).
var droppedSections = []string{"remarks", "remnawave", "api", "metrics", "reverse", "transport"}

func defaultSniffing() jsonx.Object {
	return jsonx.Object{"enabled": true, "destOverride": []any{"http", "tls", "quic"}, "routeOnly": false}
}

// Build makes the runnable config.
func Build(raw []byte, opt Options) ([]byte, error) {
	source, err := jsonx.ParseObject(raw)
	if err != nil {
		return nil, err
	}
	if _, ok := source["outbounds"].([]any); !ok {
		return nil, errors.New("config has no outbounds")
	}
	out := jsonx.Clone(source)
	for _, key := range droppedSections {
		delete(out, key)
	}

	sniffing := defaultSniffing()
	for _, in := range jsonx.Objects(source["inbounds"]) {
		if s := jsonx.Obj(in["sniffing"]); s != nil {
			sniffing = s
			break
		}
	}
	mainTag := LocalTag
	main := localInbound(opt.LocalPort, sniffing)
	if opt.Tun != nil {
		mainTag = TunTag
		main = tunInbound(*opt.Tun, opt.Routing != nil && opt.Routing.LANDirect, sniffing)
	}
	inbounds := []any{main}
	if s := opt.Share; s != nil {
		httpSettings := jsonx.Object{"allowTransparent": false}
		socksSettings := jsonx.Object{"auth": "noauth", "udp": true}
		if s.auth() {
			account := jsonx.Object{"user": s.User, "pass": s.Password}
			httpSettings["accounts"] = []any{account}
			socksSettings["auth"] = "password"
			socksSettings["accounts"] = []any{jsonx.Clone(account)}
		}
		inbounds = append(inbounds,
			jsonx.Object{"tag": ShareHTTPTag, "protocol": "http", "listen": "0.0.0.0", "port": s.HTTPPort, "settings": httpSettings, "sniffing": sniffing},
			jsonx.Object{"tag": ShareSocksTag, "protocol": "socks", "listen": "0.0.0.0", "port": s.SocksPort, "settings": socksSettings, "sniffing": sniffing},
		)
	}
	out["inbounds"] = inbounds

	// DNS queries arriving from the TUN are resolved by Xray's DNS module (the panel's "dns" servers).
	outbounds := append([]any{}, jsonx.Arr(source["outbounds"])...)
	template := jsonx.Objects(source["outbounds"])
	dnsTag := ""
	for _, o := range template {
		if jsonx.S(o, "protocol") == "dns" {
			dnsTag = jsonx.S(o, "tag")
			break
		}
	}
	if dnsTag == "" && opt.Tun != nil {
		dnsTag = DNSOutTag
		outbounds = append(outbounds, jsonx.Object{"tag": DNSOutTag, "protocol": "dns"})
	}
	for i, o := range outbounds {
		if ob := jsonx.Obj(o); jsonx.S(ob, "protocol") == "dns" {
			outbounds[i] = withDNSLevel(ob)
		}
	}
	if opt.Share != nil {
		outbounds = append(outbounds, jsonx.Object{"tag": ShareBlockTag, "protocol": "blackhole"})
	}

	routingSection := jsonx.Clone(jsonx.Obj(source["routing"]))
	templateRules := jsonx.Arr(routingSection["rules"])
	var first []any
	if opt.Share != nil {
		first = append(first, jsonx.Object{
			"type":        "field",
			"inboundTag":  []any{ShareHTTPTag, ShareSocksTag},
			"source":      toAny(nonLocalSources),
			"outboundTag": ShareBlockTag,
		})
	}
	if opt.Tun != nil {
		first = append(first, jsonx.Object{"type": "field", "inboundTag": []any{TunTag}, "port": "53", "outboundTag": dnsTag})
	}
	// Programs without VPN go around everything else, as if they were not in the TUN at all.
	if names := processNames(opt.Excluded); len(names) > 0 {
		first = append(first, jsonx.Object{
			"type":        "field",
			"inboundTag":  []any{mainTag},
			"process":     toAny(names),
			"outboundTag": tagOf(template, &outbounds, "freedom", UserDirectTag),
		})
	}
	// The user's own rules go before the panel's ones.
	if opt.Routing != nil && opt.Routing.Enabled {
		first = append(first, userRules(*opt.Routing, template, templateRules, &outbounds)...)
	}
	routingSection["rules"] = append(first, templateRules...)

	// Auto-select sends traffic to the balancer's fallback until its first checks finish and while
	// none of its servers answers; start with the server that was best a moment ago.
	if opt.Fallback != "" && hasTag(outbounds, opt.Fallback) {
		if balancers := jsonx.Arr(routingSection["balancers"]); balancers != nil {
			patched := make([]any, len(balancers))
			for i, b := range balancers {
				patched[i] = b
				obj := jsonx.Obj(b)
				if obj == nil {
					continue
				}
				for _, prefix := range jsonx.Strings(obj["selector"]) {
					if strings.HasPrefix(opt.Fallback, prefix) {
						c := jsonx.Clone(obj)
						c["fallbackTag"] = opt.Fallback
						patched[i] = c
						break
					}
				}
			}
			routingSection["balancers"] = patched
		}
	}
	out["routing"] = routingSection
	out["outbounds"] = outbounds

	// Servers are reached over IPv4; AAAA answers would make programs try IPv6 destinations that
	// the TUN accepts but the proxy cannot reach.
	dns := jsonx.Object{"servers": []any{"1.1.1.1", "8.8.8.8"}}
	if d := jsonx.Obj(source["dns"]); d != nil {
		dns = jsonx.Clone(d)
	}
	dns["queryStrategy"] = "UseIPv4"
	// A plain (UDP) nameserver keeps one connection through whichever server auto-select picked
	// when it opened, and every DNS query goes through it for as long as answers keep coming: a
	// slow server keeps all DNS slow even after auto-select has moved on. Over TCP every query is
	// a new connection that the balancer sends to a live server.
	servers := jsonx.Arr(dns["servers"])
	tcp := make([]any, len(servers))
	for i, s := range servers {
		tcp[i] = overTCP(s)
	}
	dns["servers"] = tcp
	// An expired answer is given right away while a fresh one is fetched in the background, so a
	// server switch doesn't stall every site for the length of a DNS query.
	if _, ok := dns["serveStale"]; !ok {
		dns["serveStale"] = true
	}
	out["dns"] = dns

	out["stats"] = jsonx.Object{}
	policy := jsonx.Clone(jsonx.Obj(source["policy"]))
	system := jsonx.Clone(jsonx.Obj(policy["system"]))
	system["statsInboundUplink"] = true
	system["statsInboundDownlink"] = true
	policy["system"] = system
	levels := jsonx.Clone(jsonx.Obj(policy["levels"]))
	if _, ok := levels[strconv.Itoa(DNSLevel)]; !ok {
		levels[strconv.Itoa(DNSLevel)] = jsonx.Object{"connIdle": DNSIdleSeconds}
	}
	policy["levels"] = levels
	out["policy"] = policy

	// Never write visited addresses anywhere.
	level := opt.LogLevel
	if level == "" {
		level = "error"
	}
	logSection := jsonx.Object{"loglevel": level, "access": "none", "dnsLog": false}
	if opt.ErrorLog != "" {
		logSection["error"] = opt.ErrorLog
	}
	out["log"] = logSection
	return jsonx.Marshal(out)
}

// tunInbound is Xray's own TUN (Wintun). It also sets up the system routes, and keeps DNS, and
// IPv6 when that doesn't go into it, from leaving past it.
func tunInbound(t Tun, lanDirect bool, sniffing jsonx.Object) jsonx.Object {
	mtu := t.MTU
	if mtu == 0 {
		mtu = MTU
	}
	gateway := []any{TunAddress4}
	if t.IPv6 {
		gateway = append(gateway, TunAddress6)
	}
	routes := tunRoutes(t.IPv6, lanDirect)
	settings := jsonx.Object{
		"name":                   AdapterName,
		"desc":                   AdapterName,
		"mtu":                    mtu,
		"gateway":                gateway,
		"dns":                    []any{TunDNS},
		"autoSystemRoutingTable": routes,
		"autoOutboundsInterface": "auto",
	}
	var leaks []any
	if t.StrictDNS {
		// Windows asks the DNS servers of every interface, and programs reach the router's
		// resolver through its LAN route.
		leaks = append(leaks, "dns")
	}
	if !t.IPv6 {
		// No IPv6 route leads to the TUN: block it everywhere else, so programs use IPv4.
		leaks = append(leaks, "misconfigtun")
	}
	if len(leaks) > 0 {
		settings["autoSystemWfpBlockLeak"] = leaks
	}
	return jsonx.Object{"tag": TunTag, "protocol": "tun", "port": 0, "settings": settings, "sniffing": sniffing}
}

// localInbound is the proxy for this computer's programs when there is no TUN: SOCKS5 and HTTP on
// one port (Xray's socks inbound answers both), for this computer only.
func localInbound(port int, sniffing jsonx.Object) jsonx.Object {
	return jsonx.Object{
		"tag":      LocalTag,
		"protocol": "socks",
		"listen":   "127.0.0.1",
		"port":     port,
		"settings": jsonx.Object{"auth": "noauth", "udp": true},
		"sniffing": sniffing,
	}
}

// userRules: blocked first, then direct (with the LAN and .ru presets), then through VPN.
// "Through VPN" uses the same route as the panel's catch-all rule, so it also works for
// auto-select configs with balancers.
func userRules(r Routing, template []jsonx.Object, templateRules []any, outbounds *[]any) []any {
	var catchAll jsonx.Object
	for _, rule := range jsonx.Objects(templateRules) {
		conditional := false
		for key := range rule {
			if ruleConditions[key] {
				conditional = true
				break
			}
		}
		if !conditional && (rule["balancerTag"] != nil || rule["outboundTag"] != nil) {
			catchAll = rule
		}
	}
	proxyKey, proxyTag := "outboundTag", ""
	switch {
	case catchAll != nil && jsonx.S(catchAll, "balancerTag") != "":
		proxyKey, proxyTag = "balancerTag", jsonx.S(catchAll, "balancerTag")
	case catchAll != nil && jsonx.S(catchAll, "outboundTag") != "":
		proxyTag = jsonx.S(catchAll, "outboundTag")
	default:
		for _, o := range template {
			if subs.IsProxyProtocol(jsonx.S(o, "protocol")) {
				proxyTag = jsonx.S(o, "tag")
				break
			}
		}
		if proxyTag == "" && len(template) > 0 {
			proxyTag = jsonx.S(template[0], "tag")
		}
		if proxyTag == "" {
			proxyTag = "proxy"
		}
	}

	var result []any
	for _, action := range routing.Actions {
		entries := append([]string{}, r.rules(action)...)
		if action == routing.Direct && r.LANDirect {
			entries = append(entries, routing.LANRanges...)
		}
		if action == routing.Direct && r.RUDirect {
			entries = append(entries, routing.RUDomains...)
		}
		seen := map[string]bool{}
		var ips, domains []any
		for _, e := range entries {
			x, ok := routing.ToXray(e)
			if !ok || seen[x] {
				continue
			}
			seen[x] = true
			if routing.IsIPOrCIDR(x) {
				ips = append(ips, x)
			} else {
				domains = append(domains, x)
			}
		}
		if len(ips) == 0 && len(domains) == 0 {
			continue
		}
		key, tag := "outboundTag", ""
		switch action {
		case routing.Block:
			tag = tagOf(template, outbounds, "blackhole", UserBlockTag)
		case routing.Direct:
			tag = tagOf(template, outbounds, "freedom", UserDirectTag)
		default:
			key, tag = proxyKey, proxyTag
		}
		if len(domains) > 0 {
			result = append(result, jsonx.Object{"type": "field", "domain": domains, key: tag})
		}
		if len(ips) > 0 {
			result = append(result, jsonx.Object{"type": "field", "ip": ips, key: tag})
		}
	}
	return result
}

// tagOf finds the template's outbound of a protocol, or adds one.
func tagOf(template []jsonx.Object, outbounds *[]any, protocol, fallbackTag string) string {
	for _, o := range template {
		if jsonx.S(o, "protocol") == protocol {
			if tag := jsonx.S(o, "tag"); tag != "" {
				return tag
			}
		}
	}
	if !hasTag(*outbounds, fallbackTag) {
		*outbounds = append(*outbounds, jsonx.Object{"tag": fallbackTag, "protocol": protocol})
	}
	return fallbackTag
}

func hasTag(outbounds []any, tag string) bool {
	for _, o := range outbounds {
		if jsonx.S(jsonx.Obj(o), "tag") == tag {
			return true
		}
	}
	return false
}

// processNames is how Xray matches programs: by name without ".exe", compared as it is; the
// lower-case form covers a program started with a differently cased path.
func processNames(excluded []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, e := range excluded {
		name := e
		if i := strings.LastIndexAny(name, `\/`); i >= 0 {
			name = name[i+1:]
		}
		if strings.HasSuffix(strings.ToLower(name), ".exe") {
			name = name[:len(name)-4]
		}
		add(name)
		add(strings.ToLower(name))
	}
	return out
}

// overTCP turns "9.9.9.9" (DNS over UDP) into "tcp://9.9.9.9:53"; DoH, local and other servers
// stay as they are.
func overTCP(server any) any {
	switch s := server.(type) {
	case string:
		if tcp, ok := tcpAddress(s, "53"); ok {
			return tcp
		}
	case jsonx.Object:
		port := jsonx.S(s, "port")
		if port == "" {
			port = "53"
		}
		if tcp, ok := tcpAddress(jsonx.S(s, "address"), port); ok {
			c := jsonx.Clone(s)
			delete(c, "port")
			c["address"] = tcp
			return c
		}
	}
	return server
}

func tcpAddress(address, port string) (string, bool) {
	if address == "" || strings.Contains(address, "/") || !routing.IsIPOrCIDR(address) {
		return "", false
	}
	if strings.Contains(address, ":") {
		return "tcp://[" + address + "]:" + port, true
	}
	return "tcp://" + address + ":" + port, true
}

// OutboundProtocols is the protocol of every outbound of a built config, by tag (the connection
// log tells VPN from direct).
func OutboundProtocols(config []byte) map[string]string {
	out := map[string]string{}
	obj, err := jsonx.ParseObject(config)
	if err != nil {
		return out
	}
	for _, o := range jsonx.Objects(obj["outbounds"]) {
		if tag := jsonx.S(o, "tag"); tag != "" {
			out[tag] = jsonx.S(o, "protocol")
		}
	}
	return out
}

// Counters are the traffic counters of everything that goes through the VPN: this computer and
// the devices it shares the VPN with.
func Counters(direction string) []string {
	return []string{
		"inbound>>>" + TunTag + ">>>traffic>>>" + direction,
		"inbound>>>" + LocalTag + ">>>traffic>>>" + direction,
		"inbound>>>" + ShareHTTPTag + ">>>traffic>>>" + direction,
		"inbound>>>" + ShareSocksTag + ">>>traffic>>>" + direction,
	}
}

func toAny(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}
