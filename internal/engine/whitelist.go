package engine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"furkvpn/internal/netx"
	"furkvpn/internal/settings"
	"furkvpn/internal/whitelist"
)

const whitelistPeriod = 5 * time.Minute

// kickWhitelist asks for a white-list check after delay.
func (e *Engine) kickWhitelist(delay time.Duration) {
	select {
	case e.whitelistKick <- delay:
	default:
	}
}

// whitelistLoop checks (directly, past the VPN) whether the operator has turned on white lists:
// when a window opens, when the network changes and every few minutes while connected; and
// switches servers accordingly. With no window and the VPN off it does nothing at all.
func (e *Engine) whitelistLoop(ctx context.Context) {
	periodic := time.NewTicker(whitelistPeriod)
	defer periodic.Stop()
	var due <-chan time.Time
	var timer *time.Timer
	schedule := func(d time.Duration) {
		if timer != nil {
			timer.Stop()
		}
		timer = time.NewTimer(d)
		due = timer.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case d := <-e.whitelistKick:
			schedule(d)
		case <-periodic.C:
			// Operators switch white lists on without a network change.
			if e.connected() {
				schedule(0)
			}
		case <-due:
			due = nil
			if e.settings.Get().WhitelistDetect && e.active() {
				e.checkWhitelist(ctx)
			}
		}
	}
}

// networkSettled is called when the network has changed and settled.
func (e *Engine) networkSettled(state *netx.State) {
	key := ""
	if state != nil {
		key = state.Key
	}
	e.mu.Lock()
	known := key == e.checkedNetwork
	e.mu.Unlock()
	if !known {
		e.kickWhitelist(0)
	}
}

// checkWhitelistIfStale is called when a window opens.
func (e *Engine) checkWhitelistIfStale() {
	e.mu.Lock()
	stale := time.Since(e.checkedAt) > time.Minute && !e.wl.Checking
	e.mu.Unlock()
	if stale {
		e.kickWhitelist(0)
	}
}

func (e *Engine) checkWhitelist(ctx context.Context) {
	e.mu.Lock()
	e.wl.Checking = true
	e.mu.Unlock()
	e.changed("whitelist")
	defer func() {
		e.mu.Lock()
		e.wl.Checking = false
		e.mu.Unlock()
		e.changed("whitelist")
	}()

	state := netx.Current()
	cfg := e.cfg().Whitelist
	blocked := make([]probeView, len(cfg.BlockedProbes))
	allowed := make([]probeView, len(cfg.AllowedProbes))
	var wg sync.WaitGroup
	for i, u := range cfg.BlockedProbes {
		wg.Add(1)
		go func() { defer wg.Done(); blocked[i] = e.probe(ctx, u) }()
	}
	for i, u := range cfg.AllowedProbes {
		wg.Add(1)
		go func() { defer wg.Done(); allowed[i] = e.probe(ctx, u) }()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return
	}
	anyOK := func(list []probeView) bool {
		for _, p := range list {
			if p.Error == "" {
				return true
			}
		}
		return false
	}
	status := whitelist.Classify(anyOK(blocked), anyOK(allowed))
	network := netx.None
	key := ""
	if state != nil {
		network, key = state.Kind, state.Key
	}
	e.mu.Lock()
	old := e.wl.Status
	e.wl.Status = status
	e.wl.LastCheck = &checkView{Time: time.Now().UnixMilli(), Status: string(status), Network: network, Blocked: blocked, Allowed: allowed}
	e.checkedAt = time.Now()
	e.checkedNetwork = key
	e.mu.Unlock()
	e.changed("whitelist", "pings")
	e.log.Printf("white lists: %s (network %q: %s)", status, network, probeSummary(blocked, allowed))
	e.autoSwitch(old, status)
}

func probeSummary(lists ...[]probeView) string {
	var parts []string
	for _, list := range lists {
		for _, p := range list {
			if p.Error != "" {
				parts = append(parts, p.Host+" "+p.Error)
			} else {
				parts = append(parts, fmt.Sprintf("%s %dms", p.Host, *p.MS))
			}
		}
	}
	return strings.Join(parts, ", ")
}

// probe is a plain HEAD request: any HTTP answer means the site is reachable. A fresh connection
// every time: a kept-alive one could still work after the operator has closed new connections.
func (e *Engine) probe(ctx context.Context, u string) probeView {
	view := probeView{URL: u, Host: hostOf(u)}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		view.Error = "other"
		return view
	}
	req.Header.Set("User-Agent", userAgent())
	start := time.Now()
	resp, err := e.probeClient().Do(req)
	if err != nil {
		view.Error = probeError(err)
		return view
	}
	resp.Body.Close()
	ms := time.Since(start).Milliseconds()
	view.MS = &ms
	return view
}

func (e *Engine) probeClient() *http.Client {
	e.probeOnce.Do(func() {
		dialer := netx.BypassDialer(net.Dialer{Timeout: 4 * time.Second})
		e.probes = &http.Client{
			Transport: &http.Transport{
				Proxy:                 nil,
				DialContext:           dialer.DialContext,
				DisableKeepAlives:     true,
				TLSHandshakeTimeout:   4 * time.Second,
				ResponseHeaderTimeout: 4 * time.Second,
			},
			Timeout:       6 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	})
	return e.probes
}

func hostOf(u string) string {
	p, err := url.Parse(u)
	if err != nil || p.Hostname() == "" {
		return u
	}
	return strings.TrimPrefix(p.Hostname(), "www.")
}

func probeError(err error) string {
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var hostErr x509.HostnameError
	var recordErr tls.RecordHeaderError
	var netErr net.Error
	switch {
	case errors.As(err, &dnsErr):
		return "dns"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded),
		errors.As(err, &netErr) && netErr.Timeout():
		return "timeout"
	case errors.As(err, &certErr), errors.As(err, &unknownCA), errors.As(err, &hostErr), errors.As(err, &recordErr),
		strings.Contains(err.Error(), "tls:"):
		return "tls"
	case errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.ECONNABORTED),
		strings.Contains(err.Error(), "connect"), strings.Contains(err.Error(), "forcibly closed"):
		return "connect"
	}
	return "other"
}

// autoSwitch moves to the LTE auto server when white lists turn on, and back when they are off.
func (e *Engine) autoSwitch(old, new whitelist.Status) {
	info := e.subs.Info()
	if info == nil {
		return
	}
	s := e.settings.Get()
	current := e.subs.Selected(s.SelectedServer)
	if current == nil {
		return
	}
	target := whitelist.Target(old, new, *current, info.Selectable(), e.cfg().Whitelist, s.WhitelistAutoSwitched)
	if new == whitelist.AllOK && old != whitelist.AllOK && target != nil {
		_, _ = e.settings.Update(func(s *settings.Settings) { s.WhitelistAutoSwitched = "" })
	}
	if target == nil {
		return
	}
	_, _ = e.settings.Update(func(s *settings.Settings) {
		s.SelectedServer = target.Key
		if new == whitelist.Whitelist {
			s.WhitelistAutoSwitched = target.Key
		}
	})
	e.log.Printf("white lists %s: switched to %s", new, target.DisplayName())
	go e.Reconnect()
	if new == whitelist.Whitelist {
		e.message("whitelist_switched_on", target.DisplayName())
	} else {
		e.message("whitelist_switched_off", target.DisplayName())
	}
}
