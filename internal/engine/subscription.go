package engine

import (
	"context"
	"errors"
	"time"

	"furkvpn/internal/ipc"
	"furkvpn/internal/keys"
	"furkvpn/internal/settings"
	"furkvpn/internal/subs"
)

// KeyResult tells the window what happened to a submitted key.
type KeyResult struct {
	// "imported", "refreshed", "confirm" (another key is in use: replace it?) or "tv" (a TV's
	// link: send it this computer's key?).
	Result string `json:"result"`
	URL    string `json:"url,omitempty"`
	Code   string `json:"code,omitempty"`
}

// SubmitKey takes whatever was pasted or opened (a link, a message with a link, furkvpn://...).
func (e *Engine) SubmitKey(text string) (*KeyResult, error) {
	if code := keys.TVCode(text); code != "" {
		if e.subs.Info() == nil {
			return nil, ipc.Errorf("tv_need_key", "")
		}
		return &KeyResult{Result: "tv", Code: code}, nil
	}
	res := keys.Parse(text, e.cfg().AllowedSubscriptionHosts)
	// The settings may not have come yet (a fresh install, furkapp.net slow to answer) or may
	// predate the key's domain: fetch them again before calling the key foreign.
	if res.Status == keys.Foreign && e.recheckConfig() {
		res = keys.Parse(text, e.cfg().AllowedSubscriptionHosts)
	}
	switch res.Status {
	case keys.Empty:
		return nil, ipc.Errorf("empty", "")
	case keys.NotALink:
		return nil, ipc.Errorf("not_a_key", "")
	case keys.Foreign:
		return nil, ipc.Errorf("foreign_key", "")
	}
	current := e.subs.Info()
	switch {
	case current == nil:
		return e.importKey(res.Key)
	case current.URL == res.Key.URL:
		if err := e.RefreshSubscription(); err != nil {
			return nil, err
		}
		return &KeyResult{Result: "refreshed", URL: res.Key.URL}, nil
	}
	return &KeyResult{Result: "confirm", URL: res.Key.URL}, nil
}

// ImportKey replaces the key after the window asked "replace the key?".
func (e *Engine) ImportKey(url string) (*KeyResult, error) {
	res := keys.Parse(url, e.cfg().AllowedSubscriptionHosts)
	if res.Status != keys.OK {
		return nil, ipc.Errorf("not_a_key", "")
	}
	return e.importKey(res.Key)
}

func (e *Engine) importKey(key keys.Key) (*KeyResult, error) {
	ctx, cancel := context.WithTimeout(e.ctx, 90*time.Second)
	defer cancel()
	wasConnected := e.connected()
	info, err := e.subs.Import(ctx, key)
	if err != nil {
		return nil, subscriptionError(err)
	}
	if first := info.Selectable(); len(first) > 0 {
		_, _ = e.settings.Update(func(s *settings.Settings) {
			s.SelectedServer = first[0].Key
			s.WhitelistAutoSwitched = ""
		})
	}
	e.clearPings()
	e.clearAccount()
	go e.refreshAccount(true)
	// The settings may not have come yet (furkapp.net unreachable when the window opened, behind
	// another VPN): the network works now, and the main screen shows their links and proxies.
	go e.refreshConfigIfOlder(configStaleOnOpen)
	if wasConnected {
		go e.Reconnect()
	}
	return &KeyResult{Result: "imported", URL: info.URL}, nil
}

// RefreshSubscription downloads the subscription again.
func (e *Engine) RefreshSubscription() error { return e.refreshSubscription(false) }

func (e *Engine) refreshSubscription(onlyIfStale bool) error {
	e.mu.Lock()
	if e.refreshing {
		e.mu.Unlock()
		return nil
	}
	e.refreshing = true
	e.mu.Unlock()
	e.changed("subscription")
	defer func() {
		e.mu.Lock()
		e.refreshing = false
		e.mu.Unlock()
		e.changed("subscription")
	}()
	ctx, cancel := context.WithTimeout(e.ctx, 90*time.Second)
	defer cancel()
	var info *subs.Info
	var err error
	if onlyIfStale {
		info, err = e.subs.RefreshIfStale(ctx, e.settings.Get().SubscriptionUpdateHours)
	} else {
		info, err = e.subs.Refresh(ctx)
	}
	if err != nil {
		return subscriptionError(err)
	}
	if info == nil {
		return nil
	}
	go e.refreshAccount(true)
	go e.reconnectIfConfigChanged()
	return nil
}

// refreshSubscriptionIfStale keeps the subscription fresh at the interval from the settings.
func (e *Engine) refreshSubscriptionIfStale() {
	if e.subs.IsStale(e.settings.Get().SubscriptionUpdateHours, time.Now()) {
		if err := e.refreshSubscription(true); err != nil {
			e.log.Printf("subscription refresh: %v", err)
		}
	}
}

// Logout removes the key from this computer.
func (e *Engine) Logout() {
	e.Disconnect()
	e.subs.Clear()
	e.clearAccount()
	e.clearPings()
	_, _ = e.settings.Update(func(s *settings.Settings) {
		s.SelectedServer = ""
		s.WhitelistAutoSwitched = ""
	})
}

// subscriptionError gives the window a code to pick its message by.
func subscriptionError(err error) error {
	switch {
	case errors.Is(err, subs.ErrForeign):
		return ipc.Errorf("foreign_key", "")
	case errors.Is(err, subs.ErrNotFound):
		return ipc.Errorf("not_found", "")
	case errors.Is(err, subs.ErrBadFormat):
		return ipc.Errorf("bad_format", "")
	case errors.Is(err, subs.ErrNoServers):
		return ipc.Errorf("no_servers", "")
	}
	return ipc.Errorf("download", err.Error())
}
