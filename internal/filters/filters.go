// Package filters makes the server tabs by feature emoji in the server names
// ("⚡️ — Надёжный", "🎮 — Игровой", ...). Same rules as the Android app's ServerFilters.
package filters

import (
	"strings"

	"furkvpn/internal/remote"
	"furkvpn/internal/subs"
)

// Names mix "⚡" and "⚡️": compare without the emoji variation selector.
func normalize(text string) string { return strings.ReplaceAll(text, "️", "") }

// Matches reports whether the entry is a server with the emoji in its name.
func Matches(e subs.Entry, emoji string) bool {
	return e.Kind != subs.KindSeparator && strings.Contains(normalize(e.Remarks), normalize(emoji))
}

// Available are the tabs that have at least one server in this subscription, in the
// configured order.
func Available(filters []remote.ServerFilter, servers []subs.Entry) []remote.ServerFilter {
	out := []remote.ServerFilter{}
	for _, f := range filters {
		for _, s := range servers {
			if Matches(s, f.Emoji) {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// Apply keeps the servers with the emoji; a section header is kept only if something below it
// matches. An empty emoji keeps everything.
func Apply(servers []subs.Entry, emoji string) []subs.Entry {
	if emoji == "" {
		return servers
	}
	var out []subs.Entry
	var header *subs.Entry
	for i := range servers {
		e := servers[i]
		if e.Kind == subs.KindSeparator {
			header = &servers[i]
		} else if Matches(e, emoji) {
			if header != nil {
				out = append(out, *header)
				header = nil
			}
			out = append(out, e)
		}
	}
	return out
}
