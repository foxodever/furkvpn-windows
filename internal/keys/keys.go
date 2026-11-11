// Package keys turns whatever the user pasted or opened (a bare subscription link, a
// https://furkapp.net/import?url=… link, furkvpn://import?url=…, or a message that contains a
// link) into a normalized FurkVPN subscription key. Same rules as the Android app's KeyParser.
package keys

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// Remnawave client-type suffixes: https://sub.example.com/<shortUuid>/<type>.
var clientSuffixes = map[string]bool{
	"json": true, "v2ray-json": true, "singbox": true, "mihomo": true, "clash": true, "stash": true,
}

var importHosts = map[string]bool{"furkapp.net": true, "www.furkapp.net": true}

var (
	urlRegex      = regexp.MustCompile(`(?i)\b(?:https?|furkvpn)://[^\s"'<>«»]+`)
	bareHostRegex = regexp.MustCompile(`(?i)\b(?:[a-z0-9-]+\.)+[a-z]{2,}/[^\s"'<>«»]+`)
	urlParamRegex = regexp.MustCompile(`[?&]url=([^&#]+)`)
)

// Key is a normalized subscription link.
type Key struct {
	URL  string `json:"url"`
	Host string `json:"host"`
	Path string `json:"path"`
}

// ShortID is the Remnawave short UUID: the last path segment.
func (k Key) ShortID() string {
	return k.Path[strings.LastIndex(k.Path, "/")+1:]
}

// Status of a parse.
type Status int

const (
	OK Status = iota
	Empty
	NotALink
	Foreign
)

// Result of Parse; Key is set when Status is OK.
type Result struct {
	Status Status
	Key    Key
}

// Parse finds a subscription key in text and checks that it belongs to one of the allowed hosts.
func Parse(input string, allowedHosts []string) Result {
	text := strings.TrimSpace(input)
	if text == "" {
		return Result{Status: Empty}
	}
	links := findURLs(text)
	if len(links) == 0 {
		return Result{Status: NotALink}
	}
	// A message may have other links before the key (t.me, the site): the first key wins.
	var first Result
	for i, link := range links {
		r := parseLink(link, allowedHosts)
		if r.Status == OK {
			return r
		}
		if i == 0 {
			first = r
		}
	}
	return first
}

func parseLink(candidate string, allowedHosts []string) Result {
	for range 3 {
		inner, ok := unwrap(candidate)
		if !ok {
			break
		}
		candidate = inner
	}
	u, err := url.Parse(candidate)
	if err != nil || u.Host == "" {
		return Result{Status: NotALink}
	}
	host, ok := asciiHost(u.Hostname())
	if !ok {
		return Result{Status: NotALink}
	}
	if importHosts[host] {
		return Result{Status: NotALink}
	}
	if !HostAllowed(host, allowedHosts) {
		return Result{Status: Foreign}
	}
	key, ok := normalize(u, host)
	if !ok {
		return Result{Status: NotALink}
	}
	return Result{Status: OK, Key: key}
}

// HostAllowed matches a host against patterns like "*.furkvpn.com" (subdomains only) and
// "furkconnection.net" (exactly).
func HostAllowed(host string, patterns []string) bool {
	h := strings.TrimSuffix(strings.ToLower(host), ".")
	for _, raw := range patterns {
		p := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
		switch {
		case p == "":
			continue
		case strings.HasPrefix(p, "*."):
			suffix := p[1:] // ".furkvpn.com"
			if strings.HasSuffix(h, suffix) && len(h) > len(suffix) {
				return true
			}
		case h == p:
			return true
		}
	}
	return false
}

func findURLs(text string) []string {
	var links []string
	for _, m := range urlRegex.FindAllString(text, -1) {
		links = append(links, strings.TrimRight(m, ".,)]!?;"))
	}
	if len(links) > 0 {
		return links
	}
	for _, m := range bareHostRegex.FindAllString(text, -1) {
		links = append(links, "https://"+strings.TrimRight(m, ".,)]!?;"))
	}
	return links
}

// A TV with FurkVPN shows furkapp.net/tv/<code> as a QR code and the code itself; the computer
// sends its key for that code (POST /api/v1/tv/send).
var tvLink = regexp.MustCompile(`(?i)^(?:https://furkapp\.net/tv/|furkvpn://tv/)([a-z0-9]{3}-?[a-z0-9]{3})/?$`)

// TVCode returns the code of a TV's link ("https://furkapp.net/tv/ABC123", "furkvpn://tv/abc-123")
// as "ABC123", or "" when text is not such a link.
func TVCode(text string) string {
	m := tvLink.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return ""
	}
	return strings.ToUpper(strings.ReplaceAll(m[1], "-", ""))
}

var tvCode = regexp.MustCompile(`^[A-Z0-9]{6}$`)

// russianLayout maps the letters of the Russian keyboard layout to the Latin ones on the same keys:
// a code typed off the TV's screen with the Russian layout on ("РГ ЯОА") is still the code.
var russianLayout = func() *strings.Replacer {
	ru, en := []rune("йцукенгшщзфывапролдячсмить"), []rune("qwertyuiopasdfghjklzxcvbnm")
	var pairs []string
	for i := range ru {
		pairs = append(pairs, string(ru[i]), string(en[i]), strings.ToUpper(string(ru[i])), string(en[i]))
	}
	return strings.NewReplacer(pairs...)
}()

// NormalizeTVCode turns what was typed off the TV's screen ("abc 123", "ABC-123", or with the
// Russian layout on) into "ABC123", or "" when it can't be a code.
func NormalizeTVCode(text string) string {
	text = russianLayout.Replace(strings.TrimSpace(text))
	code := strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(text))
	if !tvCode.MatchString(code) {
		return ""
	}
	return code
}

// unwrap extracts the inner key from import wrappers; ok is false when link is not a wrapper.
func unwrap(link string) (string, bool) {
	lower := strings.ToLower(link)
	wrapper := strings.HasPrefix(lower, "furkvpn://")
	for host := range importHosts {
		if strings.HasPrefix(lower, "https://"+host+"/") || strings.HasPrefix(lower, "http://"+host+"/") {
			wrapper = true
		}
	}
	if !wrapper {
		return "", false
	}
	if m := urlParamRegex.FindStringSubmatch(link); m != nil {
		decoded, err := url.QueryUnescape(m[1])
		return decoded, err == nil
	}
	// furkvpn://add/<url>
	rest := link[strings.Index(link, "://")+3:]
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return "", false
	}
	tail := rest[slash+1:]
	if !strings.HasPrefix(strings.ToLower(tail), "http") {
		return "", false
	}
	decoded, err := url.QueryUnescape(tail)
	return decoded, err == nil
}

func asciiHost(host string) (string, bool) {
	h := strings.TrimSuffix(strings.ToLower(host), ".")
	if h == "" {
		return "", false
	}
	ascii, err := idna.Lookup.ToASCII(h)
	if err != nil {
		// IP addresses and odd but valid names: keep them as they are.
		ascii = h
	}
	return strings.ToLower(ascii), true
}

func normalize(u *url.URL, host string) (Key, bool) {
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return Key{}, false
	}
	var segments []string
	for _, s := range strings.Split(u.Path, "/") {
		if s != "" {
			segments = append(segments, s)
		}
	}
	if len(segments) > 1 && clientSuffixes[strings.ToLower(segments[len(segments)-1])] {
		segments = segments[:len(segments)-1]
	}
	if len(segments) == 0 {
		return Key{}, false
	}
	authority := host
	if strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	if p := u.Port(); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil || port <= 0 || port > 65535 {
			return Key{}, false
		}
		defaultPort := 443
		if scheme == "http" {
			defaultPort = 80
		}
		// The key always becomes https, where 443 goes without saying.
		if port != defaultPort && port != 443 {
			authority += ":" + strconv.Itoa(port)
		}
	}
	escaped := make([]string, len(segments))
	for i, s := range segments {
		escaped[i] = url.PathEscape(s)
	}
	path := "/" + strings.Join(escaped, "/")
	return Key{URL: "https://" + authority + path, Host: host, Path: path}, true
}
