// Package routing turns what people type into the user's routing rules ("youtube.com", ".com",
// "https://site.ru/page", "1.2.3.4", "10.0.0.0/8", "кремль.рф", "keyword:yandex", Clash's
// "DOMAIN-KEYWORD,yandex") into stored entries ("domain:youtube.com", "domain:.com",
// "keyword:yandex", "1.2.3.4", ...), and those into Xray's form. Anything Xray could choke on is
// rejected here, because one bad entry stops the whole core. Same rules as the Android app.
package routing

import (
	"net/netip"
	"regexp"
	"strings"

	"golang.org/x/net/idna"
)

// Kind of a stored entry.
type Kind string

const (
	KindDomain  Kind = "domain"
	KindKeyword Kind = "keyword"
	KindFull    Kind = "full"
	KindRegexp  Kind = "regexp"
	KindIP      Kind = "ip"
)

// Action is the list an entry is in.
type Action string

const (
	Direct Action = "direct"
	Proxy  Action = "proxy"
	Block  Action = "block"
)

// Actions in the order the rules apply: blocked first, then direct, then through VPN.
var Actions = []Action{Block, Direct, Proxy}

// Clash / FLClash rule lines: "DOMAIN-KEYWORD,yandex", "DOMAIN-SUFFIX,ya.ru,DIRECT",
// "IP-CIDR,1.2.3.0/24". Plain DOMAIN needs its comma: "domain:" is our own prefix (a site with
// subdomains).
var clashRule = regexp.MustCompile(`(?i)^(?:(DOMAIN-SUFFIX|DOMAIN-KEYWORD|DOMAIN-REGEX|IP-CIDR6|IP-CIDR)\s*[,:]|(DOMAIN)\s*,)\s*([^,\s]+)`)

var (
	badCIDR  = regexp.MustCompile(`^[0-9a-fA-F:.]+/\d+$`)
	labelOK  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	allDigit = regexp.MustCompile(`^[0-9]+$`)
)

// LANRanges is "Локальная сеть напрямую".
var LANRanges = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "fc00::/7", "fe80::/10"}

// RUDomains is ".ru/.рф/.su напрямую".
var RUDomains = []string{"domain:ru", "domain:xn--p1ai", "domain:su"}

// ParseList splits text by commas, spaces and new lines (a Clash rule line stays whole) and
// returns the normalized valid entries and the rejected ones.
func ParseList(text string) (valid, invalid []string) {
	seen := map[string]bool{}
	valid, invalid = []string{}, []string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var tokens []string
		if clashRule.MatchString(line) {
			tokens = []string{line}
		} else {
			tokens = strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\r' })
		}
		for _, raw := range tokens {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			if entry, ok := Normalize(raw); ok {
				if !seen[entry] {
					seen[entry] = true
					valid = append(valid, entry)
				}
			} else {
				invalid = append(invalid, raw)
			}
		}
	}
	return valid, invalid
}

// Normalize returns the stored entry, or false when raw is not a domain, keyword, IP or subnet.
func Normalize(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	if m := clashRule.FindStringSubmatch(s); m != nil {
		name := m[1]
		if name == "" {
			name = m[2]
		}
		value := m[3]
		switch strings.ToUpper(name) {
		case "DOMAIN-SUFFIX":
			return domain(value)
		case "DOMAIN":
			if h, ok := host(value); ok {
				return "full:" + h, true
			}
			return "", false
		case "DOMAIN-KEYWORD":
			return keyword(value)
		case "DOMAIN-REGEX":
			return regexpRule(value)
		default:
			if IsIPOrCIDR(value) {
				return strings.ToLower(value), true
			}
			return "", false
		}
	}
	lower := strings.ToLower(s)
	switch {
	// Need geo databases the app doesn't ship.
	case strings.HasPrefix(lower, "geosite:"), strings.HasPrefix(lower, "geoip:"), strings.HasPrefix(lower, "ext:"):
		return "", false
	case strings.HasPrefix(lower, "regexp:"):
		return regexpRule(afterColon(s))
	case strings.HasPrefix(lower, "keyword:"):
		return keyword(afterColon(s))
	case strings.HasPrefix(lower, "full:"):
		if h, ok := host(afterColon(s)); ok {
			return "full:" + h, true
		}
		return "", false
	case strings.HasPrefix(lower, "domain:"):
		return domain(afterColon(s))
	}
	// Subnets first: "10.0.0.0/8" must not lose its prefix as if it were a URL path.
	if IsIPOrCIDR(s) {
		return strings.ToLower(s), true
	}
	if badCIDR.MatchString(s) {
		return "", false
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	for _, sep := range []string{"/", "?", "#"} {
		if i := strings.Index(s, sep); i >= 0 {
			s = s[:i]
		}
	}
	if IsIPOrCIDR(s) {
		return strings.ToLower(s), true
	}
	return domain(s)
}

func afterColon(s string) string { return s[strings.IndexByte(s, ':')+1:] }

// ToXray is the entry as Xray needs it: "domain:.com" (kept with its dot for the list) is
// "domain:com".
func ToXray(entry string) (string, bool) {
	n, ok := Normalize(entry)
	if !ok {
		return "", false
	}
	if strings.HasPrefix(n, "domain:.") {
		return "domain:" + strings.TrimPrefix(n, "domain:."), true
	}
	return n, true
}

// KindOf tells what a stored entry matches.
func KindOf(entry string) Kind {
	switch {
	case strings.HasPrefix(entry, "domain:"):
		return KindDomain
	case strings.HasPrefix(entry, "keyword:"):
		return KindKeyword
	case strings.HasPrefix(entry, "full:"):
		return KindFull
	case strings.HasPrefix(entry, "regexp:"):
		return KindRegexp
	}
	return KindIP
}

// Display is what the list shows: "youtube.com" for "domain:youtube.com", "yandex" for
// "keyword:yandex" (the kind is a label).
func Display(entry string) string {
	switch KindOf(entry) {
	case KindIP:
		return entry
	case KindRegexp, KindKeyword:
		return afterColon(entry)
	}
	plain := afterColon(entry)
	if strings.HasPrefix(plain, "xn--") || strings.Contains(plain, ".xn--") {
		if u, err := idna.ToUnicode(plain); err == nil {
			return u
		}
	}
	return plain
}

// Editable is the entry as one would type it: ".com", "keyword:yandex", "1.2.3.4".
func Editable(entry string) string {
	switch KindOf(entry) {
	case KindKeyword:
		return "keyword:" + Display(entry)
	case KindFull:
		return "full:" + Display(entry)
	case KindRegexp:
		return entry
	}
	return Display(entry)
}

// domain makes a "domain:" entry; a leading dot (".com", "*.site.ru") is kept so the list shows
// it as it was typed.
func domain(value string) (string, bool) {
	v := strings.TrimSpace(value)
	dot := strings.HasPrefix(v, ".") || strings.HasPrefix(v, "*.")
	h, ok := host(v)
	if !ok {
		return "", false
	}
	if dot {
		return "domain:." + h, true
	}
	return "domain:" + h, true
}

func keyword(value string) (string, bool) {
	word := strings.ToLower(strings.TrimSpace(value))
	if word == "" || strings.ContainsAny(word, " \t\r\n\f\v,") {
		return "", false
	}
	return "keyword:" + word, true
}

func regexpRule(pattern string) (string, bool) {
	if pattern == "" {
		return "", false
	}
	// Xray compiles these with Go's regexp, so this is exactly what it accepts.
	if _, err := regexp.Compile(pattern); err != nil {
		return "", false
	}
	return "regexp:" + pattern, true
}

func host(value string) (string, bool) {
	h := strings.ToLower(strings.TrimRight(strings.TrimSpace(value), "."))
	h = strings.TrimPrefix(h, "*.")
	h = strings.TrimPrefix(h, ".")
	// "host:443" -> "host"
	if strings.Count(h, ":") == 1 {
		h = h[:strings.IndexByte(h, ':')]
	}
	if h == "" || len(h) > 253 {
		return "", false
	}
	ascii, err := idna.Lookup.ToASCII(h)
	if err != nil {
		if ascii, err = idna.Punycode.ToASCII(h); err != nil {
			return "", false
		}
	}
	ascii = strings.ToLower(ascii)
	labels := strings.Split(ascii, ".")
	// "300.1.1.1" is a broken IP, not a domain: top-level domains are never all digits.
	if allDigit.MatchString(labels[len(labels)-1]) {
		return "", false
	}
	for _, l := range labels {
		if !labelOK.MatchString(l) {
			return "", false
		}
	}
	return ascii, true
}

// IsIPOrCIDR accepts an IPv4 or IPv6 address, optionally with a prefix length.
func IsIPOrCIDR(value string) bool {
	ip, prefix, hasPrefix := strings.Cut(value, "/")
	if strings.ContainsAny(ip, "%") {
		return false
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	if !hasPrefix {
		return true
	}
	if prefix == "" || len(prefix) > 3 || !allDigit.MatchString(prefix) {
		return false
	}
	_, err = netip.ParsePrefix(addr.String() + "/" + prefix)
	if err != nil {
		return false
	}
	bits := 0
	for _, c := range prefix {
		bits = bits*10 + int(c-'0')
	}
	if addr.Is4() {
		return bits <= 32
	}
	return bits <= 128
}
