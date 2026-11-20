package core

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"furkvpn/internal/jsonx"
	"furkvpn/internal/netx"
)

// Method is how a server's delay is measured.
type Method string

const (
	// ViaProxy fetches the test URL through the server: the whole way, and proof that it works.
	ViaProxy Method = "proxy"
	// TCP is the time to open a TCP connection to the server's port.
	TCP Method = "tcp"
	// ICMP is an echo request to the server's address, like the ping command.
	ICMP Method = "icmp"
)

// endpoint is where a server listens.
type endpoint struct {
	ip   net.IP
	port int
	udp  bool
}

var udpProtocols = map[string]bool{"hysteria": true, "wireguard": true}
var udpTransports = map[string]bool{"kcp": true, "mkcp": true, "quic": true, "hysteria": true}

// endpoints lists the servers of the outbounds at indexes, after pinServers has resolved them.
func endpoints(cfg jsonx.Object, indexes []int) []endpoint {
	outbounds := jsonx.Arr(cfg["outbounds"])
	var list []endpoint
	for _, idx := range indexes {
		ob := jsonx.Obj(outbounds[idx])
		settings := jsonx.Obj(ob["settings"])
		if settings == nil {
			continue
		}
		udp := udpProtocols[jsonx.S(ob, "protocol")] || udpTransports[jsonx.S(jsonx.Obj(ob["streamSettings"]), "network")]
		add := func(address string, port int) {
			if ip := net.ParseIP(address); ip != nil && port > 0 && port < 65536 {
				list = append(list, endpoint{ip, port, udp})
			}
		}
		add(jsonx.S(settings, "address"), portOf(settings["port"]))
		for _, key := range []string{"vnext", "servers"} {
			for _, entry := range jsonx.Objects(settings[key]) {
				add(jsonx.S(entry, "address"), portOf(entry["port"]))
			}
		}
		for _, peer := range jsonx.Objects(settings["peers"]) {
			if host, port, err := net.SplitHostPort(jsonx.S(peer, "endpoint")); err == nil {
				add(host, portOf(port))
			}
		}
	}
	return list
}

func portOf(v any) int {
	switch p := v.(type) {
	case float64:
		return int(p)
	case int:
		return p
	case int64:
		return int(p)
	case string:
		n, _ := strconv.Atoi(p)
		return n
	}
	if s, ok := v.(fmt.Stringer); ok { // json.Number
		n, _ := strconv.Atoi(s.String())
		return n
	}
	return 0
}

var errNotApplicable = errors.New("this kind of ping does not apply to the server")

// measureDirect pings the servers themselves, past the VPN, and returns the best delay: an
// auto-select config is as fast as the fastest of its servers. errNotApplicable when the method
// can't tell anything about them (a TCP connection to a server that only listens on UDP).
func measureDirect(list []endpoint, method Method, timeout time.Duration) (int64, error) {
	var usable []endpoint
	for _, ep := range list {
		if method == TCP && ep.udp {
			continue
		}
		usable = append(usable, ep)
	}
	if len(usable) == 0 {
		return -1, errNotApplicable
	}
	results := make([]int64, len(usable))
	errs := make([]error, len(usable))
	var wg sync.WaitGroup
	for n, ep := range usable {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ping := tcpPing
			if method == ICMP {
				ping = icmpPing
			}
			// Twice when it answers: the first packet often pays for ARP and the route, and the
			// better time is the server's. A server that is silent isn't waited for again.
			results[n], errs[n] = ping(ep, timeout)
			if errs[n] != nil {
				return
			}
			if again, err := ping(ep, timeout); err == nil && again < results[n] {
				results[n] = again
			}
		}()
	}
	wg.Wait()
	best, err := int64(-1), errs[0]
	for n := range usable {
		if errs[n] == nil && (best < 0 || results[n] < best) {
			best, err = results[n], nil
		}
	}
	return best, err
}

func tcpPing(ep endpoint, timeout time.Duration) (int64, error) {
	dialer := netx.BypassDialer(net.Dialer{Timeout: timeout})
	start := time.Now()
	conn, err := dialer.Dial("tcp", net.JoinHostPort(ep.ip.String(), strconv.Itoa(ep.port)))
	if err != nil {
		return -1, err
	}
	ms := time.Since(start).Milliseconds()
	_ = conn.Close()
	return ms, nil
}

func icmpPing(ep endpoint, timeout time.Duration) (int64, error) {
	return netx.Echo(ep.ip, timeout)
}
