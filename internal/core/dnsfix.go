package core

import (
	"net"

	"furkvpn/internal/jsonx"
)

// withServersResolvedDirectly returns configJSON with resolveServersDirectly applied, or
// unchanged when there is nothing to add.
func withServersResolvedDirectly(configJSON []byte) []byte {
	cfg, err := jsonx.ParseObject(configJSON)
	if err != nil || !resolveServersDirectly(cfg) {
		return configJSON
	}
	out, err := jsonx.Marshal(cfg)
	if err != nil {
		return configJSON
	}
	return out
}

// resolveServersDirectly makes the config resolve all of its server domains the way it resolves
// the ones it lists. The subscription template lists the servers' domains under a DNS server
// that routing sends directly (77.88.8.8); a server missing from that list is resolved through
// the other DNS servers, which go through a proxy, often the very server being resolved, so
// every lookup of it waits out the 4-second DNS timeout (and auto-select, which gives a server
// 3 seconds, sees it as down). Reports whether it added anything.
func resolveServersDirectly(cfg jsonx.Object) bool {
	domains := serverDomains(cfg)
	if len(domains) == 0 {
		return false
	}
	for _, server := range jsonx.Objects(jsonx.Obj(cfg["dns"])["servers"]) {
		list := jsonx.Arr(server["domains"])
		covers := func(domain string) bool { return domainListCovers(list, domain) }
		matched := false
		for _, d := range domains {
			if covers(d) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		added := false
		for _, d := range domains {
			if !covers(d) {
				list = append(list, "full:"+d)
				added = true
			}
		}
		if added {
			server["domains"] = list
		}
		return added
	}
	return false
}

// serverDomains returns the domain names of the servers the config's proxy outbounds connect to.
func serverDomains(cfg jsonx.Object) []string {
	outbounds := jsonx.Arr(cfg["outbounds"])
	seen := map[string]bool{}
	var domains []string
	add := func(address string) {
		if d := domainOf(address); d != "" && !seen[d] {
			seen[d] = true
			domains = append(domains, d)
		}
	}
	for _, idx := range proxyOutbounds(cfg) {
		settings := jsonx.Obj(jsonx.Obj(outbounds[idx])["settings"])
		add(jsonx.S(settings, "address"))
		for _, key := range []string{"vnext", "servers", "peers"} {
			for _, entry := range jsonx.Objects(settings[key]) {
				add(jsonx.S(entry, "address"))
				if host, _, err := net.SplitHostPort(jsonx.S(entry, "endpoint")); err == nil {
					add(host)
				}
			}
		}
	}
	return domains
}
