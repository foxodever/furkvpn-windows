package core

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"

	applog "github.com/xtls/xray-core/app/log"
	xlog "github.com/xtls/xray-core/common/log"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
)

// ConnEvent is one new connection of the running instance.
type ConnEvent struct {
	// "tcp" or "udp".
	Network string
	// The client: a program's socket on the TUN, or a device using the share proxy.
	SrcIP   string
	SrcPort int
	// The address the client connected to; empty when it asked for a domain.
	DstIP   string
	DstPort int
	// That domain, or the name the address was resolved from through Xray's DNS when known.
	Domain string
	// Tags of the route.
	Inbound, Outbound string
}

// ConnectionListener receives every new connection while it is set (the connection log, off by
// default). It is called on the connection's path, before the connection goes on, while the
// client's socket is still open, so that its owner can still be found: it must be quick.
type ConnectionListener func(ConnEvent)

var (
	listenerMu sync.RWMutex
	listener   ConnectionListener
	dnsNames   = newNameCache(8192)
)

// SetConnectionListener turns the connection log on (non-nil) or off (nil), for the running
// instance right away and for the ones started later.
func SetConnectionListener(l ConnectionListener) {
	listenerMu.Lock()
	listener = l
	listenerMu.Unlock()
	restoreLogHandler()
}

func currentListener() ConnectionListener {
	listenerMu.RLock()
	defer listenerMu.RUnlock()
	return listener
}

// restoreLogHandler points Xray's process-wide log handler back at the running instance's logger.
// Every core.New installs its own logger, including the short-lived delay-test instances.
func restoreLogHandler() {
	if inst := current(); inst != nil {
		installLogHandler(inst)
	}
}

// installLogHandler wraps the instance's logger with the connection log when it is on.
func installLogHandler(inst *core.Instance) {
	next, _ := inst.GetFeature((*applog.Instance)(nil)).(xlog.Handler)
	switch {
	case currentListener() != nil:
		xlog.RegisterHandler(&connLogHandler{next: next})
	case next != nil:
		xlog.RegisterHandler(next)
	}
}

// connLogHandler sees access records (one per connection, with the route taken) and DNS answers
// (to show names instead of addresses), then passes everything on to Xray's own logger.
type connLogHandler struct {
	next xlog.Handler
}

func (h *connLogHandler) Handle(msg xlog.Message) {
	switch m := msg.(type) {
	case *xlog.AccessMessage:
		recordConnection(m)
	case *xlog.DNSLog:
		if m.Error == nil {
			name := strings.TrimSuffix(m.Domain, ".")
			for _, ip := range m.Result {
				dnsNames.put(ip.String(), name)
			}
		}
	}
	if h.next != nil {
		h.next.Handle(msg)
	}
}

func recordConnection(m *xlog.AccessMessage) {
	// A debug feature must never take the VPN down.
	defer func() { _ = recover() }()
	if m.Status != xlog.AccessAccepted {
		return
	}
	l := currentListener()
	if l == nil {
		return
	}
	if e, ok := eventOf(m); ok {
		l(e)
	}
}

func eventOf(m *xlog.AccessMessage) (ConnEvent, bool) {
	network, dstHost, dstPort, ok := endpointOf(m.To)
	if !ok || dstHost == "" {
		return ConnEvent{}, false
	}
	e := ConnEvent{Network: network, DstPort: dstPort}
	e.Inbound, e.Outbound = splitDetour(m.Detour)
	if ip := net.ParseIP(dstHost); ip != nil {
		e.DstIP = ip.String()
		e.Domain = dnsNames.get(e.DstIP)
	} else {
		e.Domain = dstHost
	}
	if _, srcHost, srcPort, ok := endpointOf(m.From); ok && net.ParseIP(srcHost) != nil {
		e.SrcIP = srcHost
		e.SrcPort = srcPort
	}
	return e, true
}

// endpointOf understands what Xray puts into access records: destinations, socket addresses and URLs.
func endpointOf(v any) (network, host string, port int, ok bool) {
	switch d := v.(type) {
	case xnet.Destination:
		return destinationEndpoint(d)
	case *xnet.Destination:
		if d != nil {
			return destinationEndpoint(*d)
		}
	case *net.TCPAddr:
		if d != nil {
			return "tcp", d.IP.String(), d.Port, true
		}
	case *net.UDPAddr:
		if d != nil {
			return "udp", d.IP.String(), d.Port, true
		}
	case *url.URL:
		if d != nil && d.Hostname() != "" {
			p, _ := strconv.Atoi(d.Port())
			if p == 0 {
				p = 80
				if d.Scheme == "https" {
					p = 443
				}
			}
			return "tcp", d.Hostname(), p, true
		}
	}
	return "", "", 0, false
}

func destinationEndpoint(d xnet.Destination) (string, string, int, bool) {
	if d.Address == nil {
		return "", "", 0, false
	}
	network := "tcp"
	if d.Network == xnet.Network_UDP {
		network = "udp"
	}
	host := ""
	if d.Address.Family().IsDomain() {
		host = d.Address.Domain()
	} else {
		host = d.Address.IP().String()
	}
	return network, host, int(d.Port), true
}

// splitDetour turns "tun-in -> proxy" (also ">>" and "==>") into the inbound and outbound tags.
func splitDetour(detour string) (inbound, outbound string) {
	for _, sep := range []string{" ==> ", " -> ", " >> "} {
		if i := strings.Index(detour, sep); i >= 0 {
			return detour[:i], detour[i+len(sep):]
		}
	}
	return "", detour
}

// nameCache remembers which name an address was resolved from, keeping the latest size addresses.
type nameCache struct {
	mu    sync.Mutex
	names map[string]string
	ring  []string
	next  int
}

func newNameCache(size int) *nameCache {
	return &nameCache{names: make(map[string]string, size), ring: make([]string, size)}
}

func (c *nameCache) put(ip, name string) {
	if ip == "" || name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.names[ip]; !ok {
		if old := c.ring[c.next]; old != "" {
			delete(c.names, old)
		}
		c.ring[c.next] = ip
		c.next = (c.next + 1) % len(c.ring)
	}
	c.names[ip] = name
}

func (c *nameCache) get(ip string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.names[ip]
}
