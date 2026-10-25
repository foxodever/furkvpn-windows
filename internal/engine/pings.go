package engine

import (
	"sync"
	"time"

	"furkvpn/internal/core"
	"furkvpn/internal/subs"
	"furkvpn/internal/whitelist"
)

const pingTimeout = 5 * time.Second

// pingable: servers and auto-selects get a ping (auto-select through its own routing, as the VPN
// would send the traffic).
func pingable(s subs.Entry) bool { return s.Kind == subs.KindServer || s.Kind == subs.KindAuto }

// TestPings measures every server and auto-select of the list (only those that work under white
// lists while they are on).
func (e *Engine) TestPings() {
	e.mu.Lock()
	if e.pinging {
		e.mu.Unlock()
		return
	}
	e.pinging = true
	e.mu.Unlock()
	info := e.subs.Info()
	cfg := e.cfg()
	if info == nil {
		e.mu.Lock()
		e.pinging = false
		e.mu.Unlock()
		return
	}
	e.mu.Lock()
	onlyWhitelist := e.pingWhitelistOnlyLocked(info, cfg)
	var servers []subs.Entry
	for _, s := range info.Servers {
		if pingable(s) && (!onlyWhitelist || whitelist.WorksUnderWhitelist(s, cfg.Whitelist)) {
			servers = append(servers, s)
			e.pings[s.Key] = Ping{State: "testing"}
		}
	}
	e.mu.Unlock()
	e.changed("pings")
	go func() {
		limit := make(chan struct{}, 6)
		var wg sync.WaitGroup
		for _, s := range servers {
			wg.Add(1)
			limit <- struct{}{}
			go func() {
				defer func() { <-limit; wg.Done() }()
				e.measure(s.Key, cfg.DelayTestURL)
			}()
		}
		wg.Wait()
		e.mu.Lock()
		e.pinging = false
		e.mu.Unlock()
		e.changed("pings")
	}()
}

// PingServer measures one server or auto-select (right-clicking it in the list).
func (e *Engine) PingServer(key string) {
	e.mu.Lock()
	if e.pings[key].State == "testing" {
		e.mu.Unlock()
		return
	}
	e.pings[key] = Ping{State: "testing"}
	e.mu.Unlock()
	e.changed("pings")
	go e.measure(key, e.cfg().DelayTestURL)
}

func (e *Engine) measure(key, testURL string) {
	result := Ping{State: "failed"}
	if raw := e.subs.ConfigFor(key); raw != nil {
		if ms, err := core.MeasureDelay(raw, testURL, pingTimeout); err == nil && ms >= 0 {
			result = Ping{State: "ok", MS: ms}
		}
	}
	e.mu.Lock()
	e.pings[key] = result
	e.mu.Unlock()
	e.changed("pings")
}

func (e *Engine) clearPings() {
	e.mu.Lock()
	clear(e.pings)
	e.mu.Unlock()
	e.changed("pings")
}
