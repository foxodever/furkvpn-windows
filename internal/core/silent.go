package core

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/transport/internet"

	"furkvpn/internal/jsonx"
)

// A server that accepts the connection and then says nothing leaves Xray waiting for its TLS
// handshake for good: nothing in Xray times that wait out and nothing interrupts it (the request
// that wanted the connection gives up; the handshake doesn't hear of it, and XHTTP queues every
// later request behind it). A relay whose far end is gone is such a server. Auto-select checks
// each of its servers several times a minute, so with one of those in the list the VPN gathered
// a stuck connection per check for as long as it was on, and pings left theirs.
//
// So the connections to the servers are looked at from below: a socket on which something was
// sent and nothing has come back for silentAfter is shut down, which ends the wait with an error,
// as if the server had refused. Only the ports of servers behind TLS or REALITY are watched, where
// the server always speaks during the handshake: what a program sends directly, or through a
// server of another kind, may wait for its answer as long as it likes.

// silentAfter is also how often a connection is looked at; a var for the tests (under silentMu).
var silentAfter = 15 * time.Second

// silentLooks is how many times a connection that hasn't sent anything yet is looked at.
const silentLooks = 8

var (
	silentMu sync.Mutex
	// Port → the instances with a TLS or REALITY server on it.
	silentPorts   = map[string]int{}
	instancePorts = map[*core.Instance][]string{}
)

func init() { watchDialedSockets() }

// watchDialedSockets has Xray show every socket it dials to watchSilence.
func watchDialedSockets() {
	_ = internet.RegisterDialerController(watchSilence)
}

// watchSilentServers starts watching the ports of cfg's servers for as long as inst lives.
func watchSilentServers(inst *core.Instance, cfg jsonx.Object) {
	ports := tlsServerPorts(cfg)
	if len(ports) == 0 {
		return
	}
	silentMu.Lock()
	defer silentMu.Unlock()
	instancePorts[inst] = ports
	for _, p := range ports {
		silentPorts[p]++
	}
}

func unwatchSilentServers(inst *core.Instance) {
	silentMu.Lock()
	defer silentMu.Unlock()
	for _, p := range instancePorts[inst] {
		if silentPorts[p]--; silentPorts[p] <= 0 {
			delete(silentPorts, p)
		}
	}
	delete(instancePorts, inst)
}

// tlsServerPorts lists the ports of the servers that are reached over TCP with TLS or REALITY.
func tlsServerPorts(cfg jsonx.Object) []string {
	seen := map[string]bool{}
	var ports []string
	add := func(entry jsonx.Object) {
		if v, ok := entry["port"]; ok {
			if p := fmt.Sprint(v); p != "" && !seen[p] {
				seen[p] = true
				ports = append(ports, p)
			}
		}
	}
	for _, o := range jsonx.Objects(cfg["outbounds"]) {
		stream := jsonx.Obj(o["streamSettings"])
		if !proxyProtocols[jsonx.S(o, "protocol")] || stream == nil {
			continue
		}
		if s := jsonx.S(stream, "security"); s != "tls" && s != "reality" {
			continue
		}
		switch jsonx.S(stream, "network") {
		case "hysteria", "kcp", "mkcp", "quic":
			continue // over UDP
		}
		settings := jsonx.Obj(o["settings"])
		add(settings)
		for _, key := range []string{"vnext", "servers"} {
			for _, entry := range jsonx.Objects(settings[key]) {
				add(entry)
			}
		}
	}
	return ports
}

// watchSilence is called by Xray's dialer for every socket before it connects.
func watchSilence(network, address string, c syscall.RawConn) error {
	if !strings.HasPrefix(network, "tcp") {
		return nil
	}
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil
	}
	silentMu.Lock()
	watched, after := silentPorts[port] > 0, silentAfter
	silentMu.Unlock()
	if watched {
		time.AfterFunc(after, func() { checkSilence(c, false, 1) })
	}
	return nil
}

// checkSilence looks at a connection: sent says that at the last look, silentAfter ago, something
// had been sent on it and nothing had come.
func checkSilence(c syscall.RawConn, sent bool, look int) {
	var in, out uint64
	known := false
	if err := c.Control(func(fd uintptr) { in, out, known = socketBytes(fd) }); err != nil || !known || in > 0 {
		return // closed, or the system doesn't tell, or the server speaks
	}
	if sent {
		_ = c.Control(dropSocket)
		return
	}
	if out == 0 && look >= silentLooks {
		return
	}
	silentMu.Lock()
	after := silentAfter
	silentMu.Unlock()
	time.AfterFunc(after, func() { checkSilence(c, out > 0, look+1) })
}
