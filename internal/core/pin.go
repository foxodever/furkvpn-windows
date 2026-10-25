package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/xtls/xray-core/transport/internet"

	"furkvpn/internal/jsonx"
)

// pinServers replaces the domain names of the servers of the outbounds at indexes with their
// addresses, so a delay test does not wait for DNS. What the name meant for the connection stays:
// it becomes the TLS / REALITY server name and the Host of WebSocket, HTTPUpgrade and XHTTP (the
// authority of gRPC) where those are not set. Returns the first lookup error.
func pinServers(cfg jsonx.Object, indexes []int) error {
	outbounds := jsonx.Arr(cfg["outbounds"])
	type target struct {
		outbound int
		domain   string
		set      func(ip string)
	}
	var targets []target
	for _, idx := range indexes {
		settings := jsonx.Obj(jsonx.Obj(outbounds[idx])["settings"])
		if settings == nil {
			continue
		}
		addAddress := func(entry jsonx.Object) {
			if domain := domainOf(jsonx.S(entry, "address")); domain != "" {
				targets = append(targets, target{idx, domain, func(ip string) { entry["address"] = ip }})
			}
		}
		addAddress(settings)
		for _, key := range []string{"vnext", "servers"} {
			for _, entry := range jsonx.Objects(settings[key]) {
				addAddress(entry)
			}
		}
		for _, peer := range jsonx.Objects(settings["peers"]) {
			if host, port, err := net.SplitHostPort(jsonx.S(peer, "endpoint")); err == nil && domainOf(host) != "" {
				targets = append(targets, target{idx, domainOf(host), func(ip string) { peer["endpoint"] = net.JoinHostPort(ip, port) }})
			}
		}
	}

	var domains []string
	seen := map[string]bool{}
	for _, t := range targets {
		if !seen[t.domain] {
			seen[t.domain] = true
			domains = append(domains, t.domain)
		}
	}
	found := make([]net.IP, len(domains))
	errs := make([]error, len(domains))
	var wg sync.WaitGroup
	limit := make(chan struct{}, 8)
	for n, domain := range domains {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			found[n], errs[n] = lookupServer(cfg, domain)
		}()
	}
	wg.Wait()
	ips := map[string]net.IP{}
	lookupErrs := map[string]error{}
	for n, domain := range domains {
		ips[domain], lookupErrs[domain] = found[n], errs[n]
	}

	var firstErr error
	named := map[int]bool{}
	for _, t := range targets {
		ip := ips[t.domain]
		if ip == nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("resolve %s: %w", t.domain, lookupErrs[t.domain])
			}
			continue
		}
		t.set(ip.String())
		if !named[t.outbound] {
			named[t.outbound] = true
			keepServerName(jsonx.Obj(outbounds[t.outbound]), t.domain)
		}
	}
	return firstErr
}

// keepServerName writes the server's domain name where Xray would otherwise take it from the
// address that pinServers replaces.
func keepServerName(ob jsonx.Object, domain string) {
	stream := jsonx.Obj(ob["streamSettings"])
	if stream == nil {
		return
	}
	setIfEmpty := func(settingsKey, field string) {
		settings := jsonx.Obj(stream[settingsKey])
		if settings == nil {
			settings = jsonx.Object{}
			stream[settingsKey] = settings
		}
		if strings.TrimSpace(jsonx.S(settings, field)) == "" {
			settings[field] = domain
		}
	}
	switch jsonx.S(stream, "security") {
	case "tls":
		setIfEmpty("tlsSettings", "serverName")
	case "reality":
		setIfEmpty("realitySettings", "serverName")
	}
	switch network := jsonx.S(stream, "network"); network {
	case "ws", "websocket":
		if headers := jsonx.Obj(jsonx.Obj(stream["wsSettings"])["headers"]); headers["Host"] == nil && headers["host"] == nil {
			setIfEmpty("wsSettings", "host")
		}
	case "httpupgrade":
		setIfEmpty("httpupgradeSettings", "host")
	case "xhttp", "splithttp":
		setIfEmpty(network+"Settings", "host")
	case "grpc", "gun":
		setIfEmpty("grpcSettings", "authority")
	}
}

// domainOf returns address when it is a domain name (not an IP address or a placeholder).
func domainOf(address string) string {
	address = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(address), "."))
	if address == "" || address == "localhost" || net.ParseIP(address) != nil || !strings.Contains(address, ".") {
		return ""
	}
	return address
}

type cachedLookup struct {
	ip    net.IP
	until time.Time
}

var (
	lookupMu    sync.Mutex
	lookupCache = map[string]cachedLookup{}
)

const (
	lookupTTL     = 2 * time.Minute
	lookupTimeout = 4 * time.Second
)

// lookupServer resolves a server's domain name: through the plain DNS server the config lists it
// under (the subscription template resolves its servers through 77.88.8.8 directly), then through
// Windows' DNS. Neither goes through a server: while the VPN is on, the TUN binds the service's own
// sockets to the physical interface and has Go resolve past the TUN (see lookupDial). An IPv4
// address is preferred.
func lookupServer(cfg jsonx.Object, domain string) (net.IP, error) {
	lookupMu.Lock()
	hit, ok := lookupCache[domain]
	lookupMu.Unlock()
	if ok && time.Now().Before(hit.until) {
		return hit.ip, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	var addrs []net.IP
	var err error
	if network, server := listedNameServer(cfg, domain); server != "" {
		r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return lookupDial(ctx, network, server)
		}}
		half, cancelHalf := context.WithTimeout(ctx, lookupTimeout/2)
		addrs, err = r.LookupIP(half, "ip", domain)
		cancelHalf()
	}
	if len(addrs) == 0 {
		addrs, err = net.DefaultResolver.LookupIP(ctx, "ip", domain)
	}
	var ip net.IP
	for _, a := range addrs {
		if a.To4() != nil {
			ip = a.To4()
			break
		}
		if ip == nil {
			ip = a
		}
	}
	if ip == nil {
		if err == nil {
			err = errors.New("no address")
		}
		return nil, err
	}
	lookupMu.Lock()
	if len(lookupCache) > 512 {
		lookupCache = map[string]cachedLookup{}
	}
	lookupCache[domain] = cachedLookup{ip, time.Now().Add(lookupTTL)}
	lookupMu.Unlock()
	return ip, nil
}

// lookupDial reaches a DNS server the way Xray's own sockets go: while the TUN runs, bound to the
// physical interface by the controllers it registered, so the query does not go into the TUN.
func lookupDial(ctx context.Context, network, address string) (net.Conn, error) {
	internet.ControllersLock.Lock()
	controllers := append([]func(network, address string, c syscall.RawConn) error(nil), internet.Controllers...)
	internet.ControllersLock.Unlock()
	d := net.Dialer{Control: func(network, address string, c syscall.RawConn) error {
		for _, ctl := range controllers {
			if err := ctl(network, address, c); err != nil {
				return err
			}
		}
		return nil
	}}
	return d.DialContext(ctx, network, address)
}

// listedNameServer returns the plain (UDP or TCP) DNS server of the config whose domain list
// covers domain, as a network and an address to dial.
func listedNameServer(cfg jsonx.Object, domain string) (network, address string) {
	for _, server := range jsonx.Objects(jsonx.Obj(cfg["dns"])["servers"]) {
		if !domainListCovers(server["domains"], domain) {
			continue
		}
		raw := jsonx.S(server, "address")
		port := jsonx.S(server, "port")
		if port == "" {
			port = "53"
		}
		network = "udp"
		switch {
		case strings.HasPrefix(raw, "tcp://"):
			network, raw = "tcp", strings.TrimPrefix(raw, "tcp://")
		case strings.HasPrefix(raw, "udp://"):
			raw = strings.TrimPrefix(raw, "udp://")
		}
		host := raw
		if h, p, err := net.SplitHostPort(raw); err == nil {
			host, port = h, p
		}
		if net.ParseIP(strings.Trim(host, "[]")) == nil {
			continue
		}
		return network, net.JoinHostPort(strings.Trim(host, "[]"), port)
	}
	return "", ""
}

// domainListCovers reports whether an Xray domain list ("full:", "domain:", "keyword:" or plain
// substring entries) matches domain.
func domainListCovers(list any, domain string) bool {
	for _, e := range jsonx.Arr(list) {
		rule, _ := e.(string)
		rule = strings.ToLower(strings.TrimSpace(rule))
		switch {
		case strings.HasPrefix(rule, "full:"):
			if rule[len("full:"):] == domain {
				return true
			}
		case strings.HasPrefix(rule, "domain:"):
			d := rule[len("domain:"):]
			if domain == d || strings.HasSuffix(domain, "."+d) {
				return true
			}
		case strings.HasPrefix(rule, "keyword:"):
			if strings.Contains(domain, rule[len("keyword:"):]) {
				return true
			}
		case strings.Contains(rule, ":"):
			// regexp:, geosite: and the like are not worth evaluating here.
		case rule != "" && strings.Contains(domain, rule):
			return true
		}
	}
	return false
}
