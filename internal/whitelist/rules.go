// Package whitelist detects operator "white lists" (only approved services work) by probing
// sites directly, bypassing the VPN, and decides which server to switch to. Same rules as the
// Android app's WhitelistRules and WhitelistMonitor.
package whitelist

import (
	"strings"

	"furkvpn/internal/remote"
	"furkvpn/internal/subs"
)

// Status of the operator's network.
type Status string

const (
	Unknown    Status = "unknown"
	AllOK      Status = "all_ok"
	Whitelist  Status = "whitelist"
	NoInternet Status = "no_internet"
)

func normalize(text string) string { return strings.ToLower(strings.ReplaceAll(text, "️", "")) }

// Classify turns probe results into a status.
func Classify(blockedOK, allowedOK bool) Status {
	switch {
	case blockedOK:
		return AllOK
	case allowedOK:
		return Whitelist
	}
	return NoInternet
}

// Matches reports whether all words appear in the server name ("LTE" and "🎲").
func Matches(e subs.Entry, words []string) bool {
	if len(words) == 0 || e.Kind == subs.KindSeparator {
		return false
	}
	name := normalize(e.Remarks)
	for _, w := range words {
		if !strings.Contains(name, normalize(w)) {
			return false
		}
	}
	return true
}

func containsAny(e subs.Entry, words []string) bool {
	name := normalize(e.Remarks)
	for _, w := range words {
		if strings.Contains(name, normalize(w)) {
			return true
		}
	}
	return false
}

// IsBypass reports whether the server works under any operator's white lists ("Все операторы").
func IsBypass(e subs.Entry, c remote.WhitelistConfig) bool { return containsAny(e, c.BypassServer) }

// WorksUnderWhitelist: servers meant for white lists ("Все операторы", "LTE") are the only ones
// worth pinging while they are on.
func WorksUnderWhitelist(e subs.Entry, c remote.WhitelistConfig) bool {
	return containsAny(e, c.PingServers)
}

// Target is the server to switch to when the status changes, or nil to keep the current one.
// White lists on: move to the LTE auto server unless a bypass or LTE server is already chosen.
// White lists off: move back to the Wi-Fi auto server only from a bypass server, or from the
// server the app itself chose earlier; anything the user picked by hand stays.
func Target(old, new Status, current subs.Entry, servers []subs.Entry, c remote.WhitelistConfig, autoSwitchedKey string) *subs.Entry {
	switch {
	case new == Whitelist && old != Whitelist:
		if IsBypass(current, c) || Matches(current, c.LTEServer) {
			return nil
		}
		return first(servers, func(e subs.Entry) bool { return Matches(e, c.LTEServer) })
	case new == AllOK && old != AllOK:
		wasAutoSwitched := autoSwitchedKey != "" && current.Key == autoSwitchedKey
		fromBypass := old == Whitelist && IsBypass(current, c)
		if !wasAutoSwitched && !fromBypass {
			return nil
		}
		t := first(servers, func(e subs.Entry) bool { return Matches(e, c.WifiServer) })
		if t == nil || t.Key == current.Key {
			return nil
		}
		return t
	}
	return nil
}

func first(servers []subs.Entry, pred func(subs.Entry) bool) *subs.Entry {
	for i := range servers {
		if pred(servers[i]) {
			e := servers[i]
			return &e
		}
	}
	return nil
}
