// Package remote holds the settings served by https://furkapp.net/api/v1/config — the same
// endpoint the Android app reads — with built-in defaults for when it can't be reached.
package remote

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/idna"

	"furkvpn/internal/transfer"
)

// Config is the "app" block of /opt/furkapp/config/config.json on furkapp.net.
type Config struct {
	AllowedSubscriptionHosts []string         `json:"allowedSubscriptionHosts"`
	SubscriptionMirrors      []string         `json:"subscriptionMirrors"`
	TelegramBotURL           string           `json:"telegramBotUrl"`
	RenewURL                 string           `json:"renewUrl"`
	Support                  []SupportContact `json:"support"`
	// "Наш сайт" / "Наш бот" buttons on the main screen.
	Links []AppLink `json:"links"`
	// Current domains of the website and its API.
	Site SiteConfig `json:"site"`
	// "Прокси для Telegram" under the links on the main screen; hidden while empty.
	TelegramProxies []TelegramProxy `json:"telegramProxies"`
	// Tabs above the server list: servers whose names contain the emoji.
	ServerFilters []ServerFilter `json:"serverFilters"`
	// Operator white-list detection and the servers to switch to.
	Whitelist WhitelistConfig `json:"whitelist"`
	// Android packages; the Windows app uses Windows.DefaultExcludedApps instead.
	DefaultExcludedApps []string `json:"defaultExcludedApps"`
	DelayTestURL        string   `json:"delayTestUrl"`
	// What only the Windows app reads: its update and its own default exclusions.
	Windows WindowsConfig `json:"windows"`
}

type SupportContact struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type ServerFilter struct {
	Emoji string `json:"emoji"`
	Title string `json:"title"`
}

// AppLink is a button with a link; known types ("website", "bot") get a localized title when
// Title is empty.
type AppLink struct {
	Type  string `json:"type"`
	Title string `json:"title,omitempty"`
	URL   string `json:"url"`
}

// WhitelistConfig: if only AllowedProbes answer while BlockedProbes don't, the operator lets
// through only approved services. Servers are found by words in their names.
type WhitelistConfig struct {
	BlockedProbes []string `json:"blockedProbes"`
	AllowedProbes []string `json:"allowedProbes"`
	LTEServer     []string `json:"lteServer"`
	WifiServer    []string `json:"wifiServer"`
	BypassServer  []string `json:"bypassServer"`
	// While white lists are on, the ping button checks only servers with one of these words.
	PingServers []string `json:"pingServers"`
}

// TelegramProxy is a proxy Telegram adds from its link (https://t.me/proxy, /socks, /webproxy or
// tg://).
type TelegramProxy struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Flag  string `json:"flag,omitempty"`
}

var telegramLinkPrefixes = []string{"https://t.me/", "https://telegram.me/", "tg://"}

func (p TelegramProxy) valid() bool {
	if strings.TrimSpace(p.Title) == "" {
		return false
	}
	for _, prefix := range telegramLinkPrefixes {
		if len(p.URL) >= len(prefix) && strings.EqualFold(p.URL[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}

// SiteConfig holds host names without "https://".
type SiteConfig struct {
	Domain string `json:"domain"`
	// For signing in by email (in development).
	APIDomain string `json:"apiDomain"`
}

// InstallURL is "Войти через сайт" → "Войти через браузер": the account page with the button
// that adds the key to the app.
func (s SiteConfig) InstallURL() string { return "https://" + s.Domain + "/dashboard/install" }

// RenewalURL is "Продлить" / "Купить тариф": the site's payment page for the subscription
// subscriptionID (its short UUID).
func (s SiteConfig) RenewalURL(subscriptionID string) string {
	return "https://" + s.Domain + "/renewal?sub=" + url.QueryEscape(subscriptionID)
}

// WindowsConfig is the "windows" block of the API, ignored by the Android app.
type WindowsConfig struct {
	Update *UpdateInfo `json:"update"`
	// Programs that work without VPN by default (exe names, e.g. "max.exe").
	DefaultExcludedApps []string `json:"defaultExcludedApps"`
}

// UpdateInfo describes the installer of a newer version.
type UpdateInfo struct {
	VersionCode int    `json:"versionCode"`
	VersionName string `json:"versionName"`
	URL         string `json:"url"`
	SHA256      string `json:"sha256"`
	Changelog   string `json:"changelog,omitempty"`
}

// AllowHTTPUpdates lets a test API (FURKVPN_API) offer installers over plain http.
var AllowHTTPUpdates = false

// Defaults are used until the API answers, and for anything it leaves out.
func Defaults() Config {
	return Config{
		AllowedSubscriptionHosts: []string{"*.furkvpn.com", "furkconnection.net"},
		SubscriptionMirrors:      []string{"https://furkconnection.net"},
		TelegramBotURL:           "https://t.me/FurkVPNbot?start=app",
		RenewURL:                 "https://t.me/FurkVPNbot",
		Support: []SupportContact{
			{"telegram", "Telegram", "https://t.me/FurkVPNSupport"},
			{"vk", "ВКонтакте", "https://vk.me/FurkVPN"},
			{"email", "furkvpn@vulpine.email", "mailto:furkvpn@vulpine.email"},
		},
		Links: []AppLink{
			{Type: "website", URL: "https://furkvpn.info"},
			{Type: "bot", URL: "https://t.me/FurkVPNbot"},
		},
		Site: SiteConfig{Domain: "furkvpn.info", APIDomain: "api.furkvpn.info"},
		ServerFilters: []ServerFilter{
			{"⚡️", "Надёжный"},
			{"🎮", "Игровой"},
			{"📺", "Ютуб Без рекламы"},
			{"🤖", "Работает Gemini"},
			{"🏴‍☠️", "Можно Торренты"},
		},
		Whitelist: WhitelistConfig{
			BlockedProbes: []string{"https://www.google.com/generate_204", "https://www.gstatic.com/generate_204"},
			AllowedProbes: []string{"https://vk.ru", "https://ya.ru"},
			LTEServer:     []string{"LTE", "🎲"},
			WifiServer:    []string{"Wi-Fi", "🎲"},
			BypassServer:  []string{"Все операторы"},
			PingServers:   []string{"Все операторы", "LTE"},
		},
		DefaultExcludedApps: []string{"ru.rostel", "ru.gosuslugi.goskey", "ru.oneme.app"},
		DelayTestURL:        "https://www.gstatic.com/generate_204",
	}
}

// Decode reads the API answer over the defaults and sanitizes it.
func Decode(data []byte) (Config, error) {
	cfg := Defaults()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Defaults(), err
	}
	return Sanitize(cfg), nil
}

// Sanitize makes sure a broken server response never locks users out: empty lists fall back to
// the defaults, links without an address are dropped, domains become plain host names.
func Sanitize(c Config) Config {
	d := Defaults()
	if len(c.AllowedSubscriptionHosts) == 0 {
		c.AllowedSubscriptionHosts = d.AllowedSubscriptionHosts
	}
	if len(c.Support) == 0 {
		c.Support = d.Support
	}
	links := c.Links[:0:0]
	for _, l := range c.Links {
		if strings.TrimSpace(l.URL) != "" {
			links = append(links, l)
		}
	}
	c.Links = links
	c.Site = c.Site.Sanitized()
	c.DefaultExcludedApps = trimmed(c.DefaultExcludedApps)
	c.Windows.DefaultExcludedApps = programNames(c.Windows.DefaultExcludedApps)
	filters := c.ServerFilters[:0:0]
	for _, f := range c.ServerFilters {
		if strings.TrimSpace(f.Emoji) != "" {
			filters = append(filters, f)
		}
	}
	c.ServerFilters = filters
	proxies := []TelegramProxy{}
	for _, p := range c.TelegramProxies {
		p.URL = strings.TrimSpace(p.URL)
		if p.valid() {
			proxies = append(proxies, p)
		}
	}
	c.TelegramProxies = proxies
	if strings.TrimSpace(c.TelegramBotURL) == "" {
		c.TelegramBotURL = d.TelegramBotURL
	}
	if strings.TrimSpace(c.RenewURL) == "" {
		c.RenewURL = d.RenewURL
	}
	if strings.TrimSpace(c.DelayTestURL) == "" {
		c.DelayTestURL = d.DelayTestURL
	}
	if c.SubscriptionMirrors == nil {
		c.SubscriptionMirrors = []string{}
	}
	if u := c.Windows.Update; u != nil && (u.VersionCode <= 0 || !(strings.HasPrefix(u.URL, "https://") || AllowHTTPUpdates && strings.HasPrefix(u.URL, "http://"))) {
		c.Windows.Update = nil
	}
	return c
}

// programNames takes "chrome", "Chrome.exe" or a whole path as the program's file name.
func programNames(list []string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		if name, ok := transfer.ProgramName(s); ok {
			out = append(out, name)
		}
	}
	return out
}

func trimmed(list []string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

var hostNameRegex = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,62}(:\d{1,5})?$`)

// Sanitized turns "https://furkvpn.info/" into "furkvpn.info"; anything that is not a host
// name falls back to the default.
func (s SiteConfig) Sanitized() SiteConfig {
	d := Defaults().Site
	out := SiteConfig{Domain: d.Domain, APIDomain: d.APIDomain}
	if h, ok := hostName(s.Domain); ok {
		out.Domain = h
	}
	if h, ok := hostName(s.APIDomain); ok {
		out.APIDomain = h
	}
	return out
}

func hostName(value string) (string, bool) {
	host := strings.TrimSpace(value)
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	for _, sep := range []string{"/", "?", "#"} {
		if i := strings.Index(host, sep); i >= 0 {
			host = host[:i]
		}
	}
	host = strings.ToLower(strings.TrimRight(host, "."))
	if host == "" {
		return "", false
	}
	name, port := host, ""
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		name, port = host[:i], host[i:]
	}
	ascii, err := idna.Lookup.ToASCII(name)
	if err != nil {
		return "", false
	}
	h := strings.ToLower(ascii) + port
	if !hostNameRegex.MatchString(h) {
		return "", false
	}
	return h, true
}
