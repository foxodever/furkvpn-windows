// Package subs reads the Remnawave Xray JSON subscription: an array of complete Xray configs,
// one per server, each with "remarks". Same rules as the Android app's SubscriptionParser.
package subs

import (
	"encoding/base64"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"furkvpn/internal/jsonx"
)

// Kind of a list entry.
type Kind string

const (
	KindServer    Kind = "server"
	KindAuto      Kind = "auto"
	KindSeparator Kind = "separator"
)

// Entry is one line of the server list.
type Entry struct {
	// Stable id across subscription updates: remarks plus occurrence number.
	Key     string `json:"key"`
	Remarks string `json:"remarks"`
	Flag    string `json:"flag,omitempty"`
	Title   string `json:"title"`
	Kind    Kind   `json:"kind"`
	Summary string `json:"summary,omitempty"`
}

// DisplayName is "🇳🇱 Нидерланды".
func (e Entry) DisplayName() string {
	if e.Flag == "" {
		return e.Title
	}
	return e.Flag + " " + e.Title
}

// Info is what the app keeps about the subscription besides the configs themselves.
type Info struct {
	URL                 string  `json:"url"`
	Title               string  `json:"title,omitempty"`
	Announce            string  `json:"announce,omitempty"`
	SupportURL          string  `json:"supportUrl,omitempty"`
	UpdateIntervalHours int     `json:"updateIntervalHours,omitempty"`
	Upload              *int64  `json:"upload,omitempty"`
	Download            *int64  `json:"download,omitempty"`
	Total               *int64  `json:"total,omitempty"`
	Expire              *int64  `json:"expire,omitempty"`
	FetchedAt           int64   `json:"fetchedAt"`
	Servers             []Entry `json:"servers"`
}

// Selectable are the entries one can connect to (not section headers).
func (i *Info) Selectable() []Entry {
	out := make([]Entry, 0, len(i.Servers))
	for _, e := range i.Servers {
		if e.Kind != KindSeparator {
			out = append(out, e)
		}
	}
	return out
}

var proxyProtocols = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "shadowsocks": true,
	"hysteria": true, "wireguard": true, "socks": true, "http": true,
}

// IsProxyProtocol reports whether an outbound protocol leads to a server.
func IsProxyProtocol(p string) bool { return proxyProtocols[p] }

// ParseConfigs reads the body: an array of configs (or one config); only objects with
// "outbounds" count.
func ParseConfigs(body []byte) ([]jsonx.Object, error) {
	root, err := jsonx.Parse([]byte(strings.TrimSpace(string(body))))
	if err != nil {
		return nil, err
	}
	var items []any
	switch v := root.(type) {
	case []any:
		items = v
	case jsonx.Object:
		items = []any{v}
	}
	out := make([]jsonx.Object, 0, len(items))
	for _, item := range items {
		if o, ok := item.(jsonx.Object); ok {
			if _, ok := o["outbounds"].([]any); ok {
				out = append(out, o)
			}
		}
	}
	return out, nil
}

// Entries makes the server list from the configs.
func Entries(configs []jsonx.Object) []Entry {
	seen := map[string]int{}
	out := make([]Entry, 0, len(configs))
	for index, config := range configs {
		remarks := strings.TrimSpace(jsonx.S(config, "remarks"))
		if remarks == "" {
			remarks = fmt.Sprintf("Server %d", index+1)
		}
		seen[remarks]++
		proxies := proxiesOf(config)
		var first jsonx.Object
		if len(proxies) > 0 {
			first = proxies[0]
		}
		balancers := jsonx.Arr(jsonx.Obj(config["routing"])["balancers"])
		var kind Kind
		switch {
		case first == nil || AddressOf(first) == "0.0.0.0":
			kind = KindSeparator
		case len(proxies) > 1 || len(balancers) > 0:
			kind = KindAuto
		default:
			kind = KindServer
		}
		e := Entry{Key: fmt.Sprintf("%s#%d", remarks, seen[remarks]), Remarks: remarks, Kind: kind}
		if kind == KindSeparator {
			e.Title = SeparatorTitle(remarks)
		} else {
			e.Flag, e.Title = SplitFlag(remarks)
		}
		if kind == KindServer {
			e.Summary = summaryOf(first)
		}
		out = append(out, e)
	}
	return out
}

func proxiesOf(config jsonx.Object) []jsonx.Object {
	var out []jsonx.Object
	for _, o := range jsonx.Objects(config["outbounds"]) {
		if proxyProtocols[jsonx.S(o, "protocol")] {
			out = append(out, o)
		}
	}
	return out
}

// AddressOf is the server address of a proxy outbound.
func AddressOf(outbound jsonx.Object) string {
	settings := jsonx.Obj(outbound["settings"])
	if settings == nil {
		return ""
	}
	for _, key := range []string{"vnext", "servers"} {
		if list := jsonx.Arr(settings[key]); len(list) > 0 {
			if a := jsonx.S(jsonx.Obj(list[0]), "address"); a != "" {
				return a
			}
		}
	}
	if peers := jsonx.Arr(settings["peers"]); len(peers) > 0 {
		if ep := jsonx.S(jsonx.Obj(peers[0]), "endpoint"); ep != "" {
			if i := strings.LastIndexByte(ep, ':'); i >= 0 {
				return ep[:i]
			}
			return ep
		}
	}
	return jsonx.S(settings, "address")
}

func summaryOf(outbound jsonx.Object) string {
	stream := jsonx.Obj(outbound["streamSettings"])
	var parts []string
	if p, ok := jsonx.Str(outbound, "protocol"); ok {
		parts = append(parts, strings.ToUpper(p))
	}
	n := jsonx.S(stream, "network")
	switch strings.ToLower(n) {
	case "", "raw", "tcp":
		parts = append(parts, "TCP")
	case "ws":
		parts = append(parts, "WS")
	case "xhttp", "splithttp":
		parts = append(parts, "XHTTP")
	case "grpc":
		parts = append(parts, "gRPC")
	case "httpupgrade":
		parts = append(parts, "HTTPUpgrade")
	case "kcp", "mkcp":
		parts = append(parts, "mKCP")
	default:
		parts = append(parts, strings.ToUpper(n))
	}
	switch strings.ToLower(jsonx.S(stream, "security")) {
	case "tls":
		parts = append(parts, "TLS")
	case "reality":
		parts = append(parts, "REALITY")
	}
	return strings.Join(parts, " · ")
}

// SplitFlag turns "🇳🇱 Нидерланды ⚡" into ("🇳🇱", "Нидерланды ⚡").
func SplitFlag(remarks string) (flag, title string) {
	r1, n1 := utf8.DecodeRuneInString(remarks)
	r2, n2 := utf8.DecodeRuneInString(remarks[n1:])
	if isRegionalIndicator(r1) && isRegionalIndicator(r2) {
		rest := strings.TrimSpace(remarks[n1+n2:])
		if rest == "" {
			rest = remarks
		}
		return remarks[:n1+n2], rest
	}
	return "", remarks
}

func isRegionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

var (
	separatorDecor  = regexp.MustCompile(`[\x{2B07}\x{2B06}\x{2B05}\x{27A1}\x{2193}\x{2191}\x{2190}\x{2192}\x{FE0F}\x{1F447}\x{1F446}\x{1F53D}\x{1F53C}]|[—–-]{2,}`)
	separatorPrefix = regexp.MustCompile(`(?i)^(внизу|ниже|below)\s+`)
	spaces          = regexp.MustCompile(`\s+`)
)

// SeparatorTitle turns "⬇️ Внизу Домашние (Wi-Fi) ⬇️" into "Домашние (Wi-Fi)".
func SeparatorTitle(remarks string) string {
	cleaned := strings.TrimSpace(spaces.ReplaceAllString(separatorDecor.ReplaceAllString(remarks, " "), " "))
	title := strings.TrimSpace(separatorPrefix.ReplaceAllString(cleaned, ""))
	if title == "" {
		return strings.TrimSpace(remarks)
	}
	return title
}

// DecodeHeader reads Remnawave header values, which may be prefixed with "base64:".
func DecodeHeader(value string) string {
	v := strings.TrimSpace(value)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(v), "base64:") {
		return v
	}
	data := strings.TrimSpace(v[len("base64:"):])
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(data); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return ""
}

// ParseUserInfo reads "upload=1; download=2; total=3; expire=4".
func ParseUserInfo(value string) map[string]int64 {
	out := map[string]int64{}
	for _, part := range strings.Split(value, ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(k))] = int64(f)
	}
	return out
}
