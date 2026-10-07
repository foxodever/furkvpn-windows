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

// TestPings measures every server of the list (only those that work under white lists while
// they are on).
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
		if s.Kind == subs.KindServer && (!onlyWhitelist || whitelist.WorksUnderWhitelist(s, cfg.Whitelist)) {
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

// PingServer measures one server (holding it in the list).
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

// OpenAutoCheck shows the servers inside auto-select: while the VPN runs it, what its own checks
// see (refreshed every couple of seconds); otherwise each of its servers pinged directly.
func (e *Engine) OpenAutoCheck(key string) {
	e.CloseAutoCheck()
	var entry *subs.Entry
	if info := e.subs.Info(); info != nil {
		for i := range info.Servers {
			if info.Servers[i].Key == key {
				entry = &info.Servers[i]
			}
		}
	}
	if entry == nil {
		return
	}
	members := e.subs.AutoMembers(key)
	servers := func(results map[string]core.ServerCheck, measured map[string]int64) []AutoServer {
		out := make([]AutoServer, 0, len(members))
		for _, m := range members {
			s := AutoServer{Tag: m.Tag, Title: m.Tag}
			if m.Server != nil {
				s.Flag, s.Title = m.Server.Flag, m.Server.Title
			} else if m.Address != "" {
				s.Title = m.Address
			}
			if r, ok := results[m.Tag]; ok {
				alive := r.Alive
				s.Alive = &alive
				if alive {
					d := r.Delay
					s.Delay = &d
				}
			}
			if ms, ok := measured[m.Tag]; ok {
				alive := ms >= 0
				s.Alive = &alive
				if alive {
					s.Delay = &ms
				}
			}
			out = append(out, s)
		}
		return out
	}
	e.mu.Lock()
	live := e.vpn.Status == StatusConnected && e.vpn.ServerKey == key
	e.mu.Unlock()
	ctx, cancel := context.WithCancel(e.ctx)
	check := &AutoCheck{Key: key, Name: entry.DisplayName(), Live: live, Checking: true, Servers: servers(nil, nil), cancel: cancel}
	e.mu.Lock()
	e.autoCheck = check
	e.mu.Unlock()
	e.changed("autoCheck")
	update := func(f func(c *AutoCheck)) {
		e.mu.Lock()
		if e.autoCheck == check {
			f(check)
		}
		e.mu.Unlock()
		e.changed("autoCheck")
	}
	if live {
		go func() {
			t := time.NewTicker(2 * time.Second)
			defer t.Stop()
			for {
				results := map[string]core.ServerCheck{}
				for _, c := range core.Observatory() {
					results[c.Tag] = c
				}
				update(func(c *AutoCheck) {
					// Right after connecting the first checks are still running.
					c.Checking = c.rechecking || len(results) == 0
					c.Servers = servers(results, nil)
				})
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		}()
		return
	}
	go func() {
		raw := e.subs.ConfigFor(key)
		measured := map[string]int64{}
		if raw != nil {
			delays, _ := core.MeasureOutbounds(raw, e.cfg().DelayTestURL, pingTimeout)
			for _, d := range delays {
				measured[d.Tag] = d.Delay
			}
		}
		if ctx.Err() != nil {
			return
		}
		update(func(c *AutoCheck) {
			c.Checking = false
			c.Servers = servers(nil, measured)
		})
	}()
}

// RecheckAuto is "Проверить сейчас" in the auto-select window.
func (e *Engine) RecheckAuto() {
	e.mu.Lock()
	check := e.autoCheck
	if check == nil || check.Checking {
		e.mu.Unlock()
		return
	}
	if !check.Live {
		e.mu.Unlock()
		e.OpenAutoCheck(check.Key)
		return
	}
	check.rechecking = true
	check.Checking = true
	e.mu.Unlock()
	e.changed("autoCheck")
	go func() {
		core.RecheckServers()
		e.mu.Lock()
		check.rechecking = false
		check.Checking = false
		e.mu.Unlock()
		e.changed("autoCheck")
	}()
}

// CloseAutoCheck closes the auto-select window.
func (e *Engine) CloseAutoCheck() {
	e.mu.Lock()
	check := e.autoCheck
	e.autoCheck = nil
	e.mu.Unlock()
	if check != nil && check.cancel != nil {
		check.cancel()
	}
	e.changed("autoCheck")
}
