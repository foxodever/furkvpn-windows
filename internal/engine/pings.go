package engine

import (
	"context"
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
// lists while they are on). Pressed again while pinging, it starts over, like Happ: measurements
// already under way finish and report (the core can't be interrupted), the queue starts anew,
// and the six-at-a-time limit is shared, so a restart doesn't double the load.
func (e *Engine) TestPings() {
	info := e.subs.Info()
	cfg := e.cfg()
	if info == nil {
		return
	}
	ctx, cancel := context.WithCancel(e.ctx)
	e.mu.Lock()
	if e.pingCancel != nil {
		e.pingCancel()
	}
	e.pingRun++
	run := e.pingRun
	e.pingCancel = cancel
	e.pinging = true
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
		defer cancel()
		var wg sync.WaitGroup
		for _, s := range servers {
			acquired := false
			select {
			case e.pingLimit <- struct{}{}:
				acquired = true
			case <-ctx.Done():
			}
			if ctx.Err() != nil {
				if acquired {
					<-e.pingLimit
				}
				break
			}
			e.mu.Lock()
			// Already pinged by a right-click meanwhile, or still being measured (it will report).
			due := e.pings[s.Key].State == "testing" && !e.measuring[s.Key]
			e.mu.Unlock()
			if !due {
				<-e.pingLimit
				continue
			}
			wg.Add(1)
			go func() {
				defer func() { <-e.pingLimit; wg.Done() }()
				e.measure(s.Key, cfg.DelayTestURL)
			}()
		}
		wg.Wait()
		e.mu.Lock()
		if run == e.pingRun {
			e.pinging = false
		}
		e.mu.Unlock()
		e.changed("pings")
	}()
}

// PingServer measures one server or auto-select (right-clicking it in the list) right away, also
// while it waits in the "ping all" queue, which then skips it.
func (e *Engine) PingServer(key string) {
	e.mu.Lock()
	if e.measuring[key] {
		e.mu.Unlock()
		return
	}
	e.pings[key] = Ping{State: "testing"}
	e.mu.Unlock()
	e.changed("pings")
	go e.measure(key, e.cfg().DelayTestURL)
}

func (e *Engine) measure(key, testURL string) {
	e.mu.Lock()
	if e.measuring[key] {
		e.mu.Unlock()
		return
	}
	e.measuring[key] = true
	e.mu.Unlock()
	result := Ping{State: "failed"}
	if raw := e.subs.ConfigFor(key); raw != nil {
		if ms, err := core.MeasureDelay(raw, testURL, pingTimeout); err == nil && ms >= 0 {
			result = Ping{State: "ok", MS: ms}
		}
	}
	e.mu.Lock()
	delete(e.measuring, key)
	e.pings[key] = result
	e.mu.Unlock()
	e.changed("pings")
}

func (e *Engine) clearPings() {
	e.mu.Lock()
	if e.pingCancel != nil {
		e.pingCancel() // another key: the queued servers are gone
	}
	clear(e.pings)
	e.mu.Unlock()
	e.changed("pings")
}
