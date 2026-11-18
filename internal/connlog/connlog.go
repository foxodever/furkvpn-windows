// Package connlog is the debug log of a session (Settings → Журнал подключений, off by default):
// which addresses programs open and whether each connection goes through the VPN, directly or is
// blocked. Kept in memory until the next connection. Same model as the Android app's ConnectionLog.
package connlog

import (
	"slices"
	"sync"
	"time"

	"furkvpn/internal/core"
	"furkvpn/internal/xrayconf"
)

// Route of a connection.
type Route string

const (
	VPN     Route = "vpn"
	Direct  Route = "direct"
	Blocked Route = "blocked"
)

// MaxEntries is how many addresses are kept; the oldest tenth is forgotten when there are more.
const MaxEntries = 3000

// Entry is one address of one program: how many connections and the latest one's details.
type Entry struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	UDP       bool   `json:"udp"`
	Route     Route  `json:"route"`
	Count     int    `json:"count"`
	FirstSeen int64  `json:"firstSeen"`
	LastSeen  int64  `json:"lastSeen"`
}

// App is a program (or a device using the share proxy) with its addresses.
type App struct {
	Key string `json:"key"`
	// Full path of the program's file, for its icon; empty for devices and unknown owners.
	Path      string  `json:"path,omitempty"`
	Label     string  `json:"label"`
	Kind      string  `json:"kind"` // "program", "system", "device", "unknown"
	Entries   []Entry `json:"entries"`
	FirstSeen int64   `json:"firstSeen"`
}

type entryKey struct {
	app, host string
	route     Route
}

type appInfo struct {
	path, label, kind string
}

// Log is the session's log.
type Log struct {
	mu      sync.Mutex
	entries map[entryKey]*Entry
	apps    map[string]appInfo
	version int64
	routes  map[string]*Route
}

// New makes an empty log.
func New() *Log {
	return &Log{entries: map[entryKey]*Entry{}, apps: map[string]appInfo{}, routes: map[string]*Route{}}
}

// SetOutbounds tells where each outbound of the running config leads; DNS answers are not
// connections.
func (l *Log) SetOutbounds(protocols map[string]string) {
	routes := map[string]*Route{}
	for tag, protocol := range protocols {
		var r *Route
		switch protocol {
		case "freedom":
			r = ptr(Direct)
		case "blackhole":
			r = ptr(Blocked)
		case "dns", "loopback":
			r = nil
		default:
			r = ptr(VPN)
		}
		routes[tag] = r
	}
	l.mu.Lock()
	l.routes = routes
	l.mu.Unlock()
}

func ptr(r Route) *Route { return &r }

// RouteOf tells where an outbound leads; unknown tags are the panel's proxies (balancers pick
// them by tag). ok is false for DNS.
func (l *Log) RouteOf(outbound string) (Route, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if r, known := l.routes[outbound]; known {
		if r == nil {
			return "", false
		}
		return *r, true
	}
	return VPN, true
}

// Record adds one connection.
func (l *Log) Record(appKey, path, label, kind, host string, port int, udp bool, route Route, at int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.apps[appKey]; !ok {
		l.apps[appKey] = appInfo{path, label, kind}
	}
	key := entryKey{appKey, host, route}
	if e, ok := l.entries[key]; ok {
		e.Count++
		e.Port, e.UDP, e.LastSeen = port, udp, at
	} else {
		l.entries[key] = &Entry{Host: host, Port: port, UDP: udp, Route: route, Count: 1, FirstSeen: at, LastSeen: at}
	}
	if len(l.entries) > MaxEntries {
		// Forget the oldest tenth.
		keys := make([]entryKey, 0, len(l.entries))
		for k := range l.entries {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, func(a, b entryKey) int { return cmp64(l.entries[a].LastSeen, l.entries[b].LastSeen) })
		for _, k := range keys[:MaxEntries/10] {
			delete(l.entries, k)
		}
		used := map[string]bool{}
		for k := range l.entries {
			used[k.app] = true
		}
		for k := range l.apps {
			if !used[k] {
				delete(l.apps, k)
			}
		}
	}
	l.version++
}

// Clear empties the log.
func (l *Log) Clear() {
	l.mu.Lock()
	clear(l.entries)
	clear(l.apps)
	l.version++
	l.mu.Unlock()
}

// Version grows with every change, so a screen redraws only when there is something new.
func (l *Log) Version() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.version
}

// Snapshot lists programs and their addresses in the order they first appeared: new ones are added
// at the end, so the list doesn't jump while it updates.
func (l *Log) Snapshot() []App {
	l.mu.Lock()
	byApp := map[string][]Entry{}
	for k, e := range l.entries {
		byApp[k.app] = append(byApp[k.app], *e)
	}
	apps := make([]App, 0, len(byApp))
	for key, list := range byApp {
		slices.SortFunc(list, func(a, b Entry) int { return cmp64(a.FirstSeen, b.FirstSeen) })
		info := l.apps[key]
		label := info.label
		if label == "" {
			label = key
		}
		apps = append(apps, App{Key: key, Path: info.path, Label: label, Kind: info.kind, Entries: list, FirstSeen: list[0].FirstSeen})
	}
	l.mu.Unlock()
	slices.SortFunc(apps, func(a, b App) int {
		if c := cmp64(a.FirstSeen, b.FirstSeen); c != 0 {
			return c
		}
		return compareStrings(a.Key, b.Key)
	})
	return apps
}

func cmp64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Owner is who a connection belongs to.
type Owner struct {
	Key, Path, Label, Kind string
}

// Resolver finds the program behind a connection from the TUN.
type Resolver interface {
	Owner(network, srcIP string, srcPort int, dstIP string, dstPort int) Owner
}

// Listener receives new connections from the core and records them, finding the program behind
// each one. Devices using the share proxy are labelled with their address, system services with
// their process name, and the app shows them in the user's language.
func (l *Log) Listener(resolver Resolver) core.ConnectionListener {
	return func(e core.ConnEvent) {
		route, ok := l.RouteOf(e.Outbound)
		if !ok {
			return
		}
		host := e.Domain
		if host == "" {
			host = e.DstIP
		}
		if host == "" {
			return
		}
		udp := e.Network == "udp"
		var owner Owner
		switch e.Inbound {
		case xrayconf.TunTag, xrayconf.LocalTag, xrayconf.AppsTag:
			owner = resolver.Owner(e.Network, e.SrcIP, e.SrcPort, e.DstIP, e.DstPort)
		case xrayconf.ShareHTTPTag, xrayconf.ShareSocksTag:
			owner = Owner{Key: "share:" + e.SrcIP, Label: e.SrcIP, Kind: "device"}
		default:
			return
		}
		l.Record(owner.Key, owner.Path, owner.Label, owner.Kind, host, e.DstPort, udp, route, time.Now().UnixMilli())
	}
}
