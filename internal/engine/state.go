package engine

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"furkvpn/internal/core"
	"furkvpn/internal/netx"
	"furkvpn/internal/remote"
	"furkvpn/internal/routing"
	"furkvpn/internal/settings"
	"furkvpn/internal/subs"
	"furkvpn/internal/sysinfo"
	"furkvpn/internal/version"
	"furkvpn/internal/whitelist"
)

// VPN states.
const (
	StatusIdle       = "idle"
	StatusConnecting = "connecting"
	StatusConnected  = "connected"
	StatusStopping   = "stopping"
)

type vpnView struct {
	Status    string `json:"status"`
	Since     int64  `json:"since,omitempty"`
	ServerKey string `json:"serverKey,omitempty"`
	// The share proxy while it runs.
	Share *shareView `json:"share,omitempty"`
	// Without the TUN: the port of the proxy on 127.0.0.1 that programs use.
	LocalPort int `json:"localPort,omitempty"`
}

type shareView struct {
	HTTPPort  int  `json:"httpPort"`
	SocksPort int  `json:"socksPort"`
	Auth      bool `json:"auth"`
}

// Traffic: speeds are bytes per second, totals are bytes since connecting.
type Traffic struct {
	Up        int64 `json:"up"`
	Down      int64 `json:"down"`
	TotalUp   int64 `json:"totalUp"`
	TotalDown int64 `json:"totalDown"`
}

// Ping of one server: State is "testing", "ok" or "failed".
type Ping struct {
	State string `json:"state"`
	MS    int64  `json:"ms,omitempty"`
}

type probeView struct {
	URL   string `json:"url"`
	Host  string `json:"host"`
	MS    *int64 `json:"ms"`
	Error string `json:"error,omitempty"` // timeout, dns, connect, tls, other
}

type checkView struct {
	Time    int64       `json:"time"`
	Status  string      `json:"status"`
	Network netx.Kind   `json:"network"`
	Blocked []probeView `json:"blocked"`
	Allowed []probeView `json:"allowed"`
}

type whitelistView struct {
	Status    whitelist.Status `json:"status"`
	Checking  bool             `json:"checking"`
	LastCheck *checkView       `json:"lastCheck"`
}

type updateView struct {
	Available *remote.UpdateInfo `json:"available"`
	// Download progress 0..1 while the installer is being downloaded.
	Progress *float64 `json:"progress"`
	// The installer is downloaded and checked.
	Ready bool   `json:"ready"`
	Error string `json:"error,omitempty"`
}

type ruleView struct {
	Entry    string       `json:"entry"`
	Display  string       `json:"display"`
	Kind     routing.Kind `json:"kind"`
	Editable string       `json:"editable"`
}

func ruleViews(list []string) []ruleView {
	out := make([]ruleView, len(list))
	for i, e := range list {
		out[i] = ruleView{e, routing.Display(e), routing.KindOf(e), routing.Editable(e)}
	}
	return out
}

// settingsView is what the windows show of the settings (the routing lists go on their own).
type settingsView struct {
	SelectedServer          string         `json:"selectedServer"`
	ServersOnLeft           bool           `json:"serversOnLeft"`
	SubscriptionUpdateHours int            `json:"subscriptionUpdateHours"`
	WhitelistDetect         bool           `json:"whitelistDetect"`
	ConnectionLog           bool           `json:"connectionLog"`
	SkippedUpdate           int            `json:"skippedUpdate"`
	AutoConnect             bool           `json:"autoConnect"`
	Share                   settings.Share `json:"share"`
	Tun                     settings.Tun   `json:"tun"`
}

// sliceNames are the parts of the state; each is sent to the windows only when it changes.
var sliceNames = []string{
	"meta", "vpn", "traffic", "subscription", "account", "config", "settings", "exclusions",
	"routing", "whitelist", "pings", "update", "lan",
}

// slice builds one part of the state.
func (e *Engine) slice(name string) any {
	switch name {
	case "meta":
		return map[string]any{
			"version":     version.Name,
			"versionCode": version.CodeInt(),
			"xray":        core.Version(),
			"os":          sysinfo.OSVersion(),
		}
	case "subscription":
		info := e.subs.Info()
		selected := ""
		if sel := e.subs.Selected(e.settings.Get().SelectedServer); sel != nil {
			selected = sel.Key
		}
		renew := renewURL(info, e.cfg())
		e.mu.Lock()
		defer e.mu.Unlock()
		var v any
		if info != nil {
			v = info
		}
		return map[string]any{"info": v, "refreshing": e.refreshing, "selected": selected, "renewUrl": renew}
	case "config":
		return e.cfg()
	case "settings":
		s := e.settings.Get()
		selected := ""
		if sel := e.subs.Selected(s.SelectedServer); sel != nil {
			selected = sel.Key
		}
		return settingsView{
			SelectedServer:          selected,
			ServersOnLeft:           s.ServersOnLeft,
			SubscriptionUpdateHours: s.SubscriptionUpdateHours,
			WhitelistDetect:         s.WhitelistDetect,
			ConnectionLog:           s.ConnectionLog,
			SkippedUpdate:           s.SkippedUpdate,
			AutoConnect:             s.AutoConnect,
			Share:                   s.Share,
			Tun:                     s.Tun,
		}
	case "exclusions":
		x := settings.ExclusionsOf(e.settings.Get(), e.cfg().Windows.DefaultExcludedApps)
		return map[string]any{
			"manual":         x.Manual,
			"recommended":    x.Recommended,
			"useRecommended": x.UseRecommended,
			"overrides":      x.Overrides,
			"effective":      x.Effective(),
			"onlySelected":   x.OnlySelected,
			"selected":       x.Selected,
		}
	case "routing":
		r := e.settings.Get().Routing
		return map[string]any{
			"enabled":   r.Enabled,
			"lanDirect": r.LANDirect,
			"ruDirect":  r.RUDirect,
			"direct":    ruleViews(r.Direct),
			"proxy":     ruleViews(r.Proxy),
			"block":     ruleViews(r.Block),
		}
	case "pings":
		info, cfg := e.subs.Info(), e.cfg()
		e.mu.Lock()
		defer e.mu.Unlock()
		results := make(map[string]Ping, len(e.pings))
		for k, v := range e.pings {
			results[k] = v
		}
		return map[string]any{"pinging": e.pinging, "results": results, "whitelistOnly": e.pingWhitelistOnlyLocked(info, cfg)}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	switch name {
	case "vpn":
		return e.vpn
	case "traffic":
		return e.traffic
	case "account":
		return e.account
	case "whitelist":
		return e.wl
	case "update":
		return e.upd
	case "lan":
		return e.lan
	}
	return nil
}

// renewURL is where "Продлить" and "Купить тариф" lead: the site's payment page for this
// subscription (the last part of the key's path is its id), or the API's renewUrl (the bot) when
// there is none.
func renewURL(info *subs.Info, cfg remote.Config) string {
	if info != nil {
		if u, err := url.Parse(info.URL); err == nil {
			parts := strings.Split(u.Path, "/")
			for i := len(parts) - 1; i >= 0; i-- {
				if parts[i] != "" {
					return cfg.Site.RenewalURL(parts[i])
				}
			}
		}
	}
	return cfg.RenewURL
}

// pingWhitelistOnlyLocked: white lists are on, so the ping button checks only the servers that
// work under them.
func (e *Engine) pingWhitelistOnlyLocked(info *subs.Info, cfg remote.Config) bool {
	if e.wl.Status != whitelist.Whitelist || info == nil {
		return false
	}
	for _, s := range info.Servers {
		if whitelist.WorksUnderWhitelist(s, cfg.Whitelist) {
			return true
		}
	}
	return false
}

// changed asks the publisher to send the named parts of the state (all of them when none is
// named) if they are different from what the windows have.
func (e *Engine) changed(names ...string) {
	e.pubMu.Lock()
	if len(names) == 0 {
		names = sliceNames
	}
	for _, n := range names {
		e.dirtySlices[n] = true
	}
	e.pubMu.Unlock()
	select {
	case e.dirty <- struct{}{}:
	default:
	}
}

func (e *Engine) publisher(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.dirty:
		}
		// Let a burst of changes settle into one message.
		time.Sleep(30 * time.Millisecond)
		e.publish()
	}
}

func (e *Engine) publish() {
	e.pubMu.Lock()
	names := make([]string, 0, len(e.dirtySlices))
	for n := range e.dirtySlices {
		names = append(names, n)
	}
	clear(e.dirtySlices)
	e.pubMu.Unlock()
	if e.server == nil {
		return
	}
	diff := map[string]json.RawMessage{}
	for _, name := range names {
		raw, err := json.Marshal(e.slice(name))
		if err != nil {
			continue
		}
		e.pubMu.Lock()
		if e.published[name] != string(raw) {
			e.published[name] = string(raw)
			diff[name] = raw
		}
		e.pubMu.Unlock()
	}
	if len(diff) > 0 {
		e.server.Broadcast("state", diff)
	}
}

// fullState is everything, for a window that has just connected.
func (e *Engine) fullState() map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, name := range sliceNames {
		if raw, err := json.Marshal(e.slice(name)); err == nil {
			out[name] = raw
		}
	}
	return out
}

// message tells the windows something worth a notice ("switched to the LTE server").
func (e *Engine) message(code string, args ...string) {
	if e.server != nil {
		e.server.Broadcast("message", map[string]any{"code": code, "args": args})
	}
}
