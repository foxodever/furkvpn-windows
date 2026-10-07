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
	candidate, ok := findURL(text)
	if !ok {
		return Result{Status: NotALink}
	}
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

func findURL(text string) (string, bool) {
	if m := urlRegex.FindString(text); m != "" {
		return strings.TrimRight(m, ".,)]!?;"), true
	}
	if m := bareHostRegex.FindString(text); m != "" {
		return "https://" + strings.TrimRight(m, ".,)]!?;"), true
	}
	return "", false
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
