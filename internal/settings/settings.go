// Package settings keeps the app's settings in one JSON file next to the service's other data.
// The VPN is machine-wide, and so are its settings.
package settings

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"furkvpn/internal/routing"
)

// How often the subscription is refreshed automatically: hours, or one of these.
const (
	UpdateFromSubscription = 0
	UpdateManual           = -1
)

const (
	DefaultHTTPPort  = 8080
	DefaultSocksPort = 1080
	MinPort          = 1024
	MaxPort          = 65535
)

const (
	DefaultMTU       = 1500
	MinMTU           = 1280 // the least IPv6 allows
	MaxMTU           = 9000
	DefaultProxyPort = 10808
)

// Tun is Settings → Расширенные настройки: how the computer's traffic gets into the VPN.
type Tun struct {
	// The VPN adapter takes all of the computer's traffic. Without it the VPN is a proxy on
	// 127.0.0.1 for the programs set up to use it: through Windows' proxy settings or their own.
	Enabled bool `json:"enabled"`
	MTU     int  `json:"mtu"`
	// IPv6 goes into the adapter too; otherwise it is blocked past it, and programs use IPv4.
	IPv6 bool `json:"ipv6"`
	// DNS only through the adapter: Windows asks the DNS servers of every network it is on.
	StrictDNS bool `json:"strictDns"`
	// Without the adapter: the proxy's port, and whether the app points Windows' proxy at it.
	ProxyPort   int  `json:"proxyPort"`
	SystemProxy bool `json:"systemProxy"`
}

// DefaultTun is the adapter as it was before these settings existed.
func DefaultTun() Tun {
	return Tun{Enabled: true, MTU: DefaultMTU, IPv6: true, StrictDNS: true, ProxyPort: DefaultProxyPort, SystemProxy: true}
}

// Share is "Раздача интернета": an HTTP and a SOCKS5 proxy for other devices on the network.
type Share struct {
	Enabled   bool   `json:"enabled"`
	HTTPPort  int    `json:"httpPort"`
	SocksPort int    `json:"socksPort"`
	Auth      bool   `json:"auth"`
	User      string `json:"user"`
	Password  string `json:"password"`
}

// Routing is Settings → Маршрутизация.
type Routing struct {
	Enabled   bool     `json:"enabled"`
	LANDirect bool     `json:"lanDirect"`
	RUDirect  bool     `json:"ruDirect"`
	Direct    []string `json:"direct"`
	Proxy     []string `json:"proxy"`
	Block     []string `json:"block"`
}

// Rules is one of the lists.
func (r *Routing) Rules(a routing.Action) []string {
	switch a {
	case routing.Direct:
		return r.Direct
	case routing.Proxy:
		return r.Proxy
	}
	return r.Block
}

func (r *Routing) set(a routing.Action, list []string) {
	switch a {
	case routing.Direct:
		r.Direct = list
	case routing.Proxy:
		r.Proxy = list
	default:
		r.Block = list
	}
}

// Settings of the app.
type Settings struct {
	SelectedServer string `json:"selectedServer,omitempty"`
	// Programs the user took out of the VPN (exe names).
	ExcludedApps []string `json:"excludedApps"`
	// Programs from the server's recommended list go around the VPN unless this is off.
	UseRecommendedExclusions bool `json:"useRecommendedExclusions"`
	// Recommended programs the user put back into the VPN one by one.
	RecommendedOverrides []string `json:"recommendedOverrides"`
	// Only the programs in VPNApps go through the VPN and everything else around it. The lists
	// above are kept for when this is turned off again.
	OnlySelectedApps bool `json:"onlySelectedApps"`
	// Programs that go through the VPN while OnlySelectedApps is on (exe names).
	VPNApps []string `json:"vpnApps"`
	Share   Share    `json:"share"`
	// Two-pane layout: the server list on the left.
	ServersOnLeft           bool    `json:"serversOnLeft"`
	SubscriptionUpdateHours int     `json:"subscriptionUpdateHours"`
	Routing                 Routing `json:"routing"`
	// Detect operator white lists and switch to bypass servers automatically.
	WhitelistDetect bool `json:"whitelistDetect"`
	// Switch to the bypass servers when white lists turn on (and back); off, they are only shown.
	WhitelistAutoSwitch bool `json:"whitelistAutoSwitch"`
	// How the ping button measures the servers: "proxy" (a request through the server), "tcp"
	// or "icmp" (the server itself).
	PingType string `json:"pingType"`
	// Server the app switched to because of white lists, to know what may be switched back.
	WhitelistAutoSwitched string `json:"whitelistAutoSwitched,omitempty"`
	// Debug log of the programs' connections.
	ConnectionLog bool `json:"connectionLog"`
	// Version code of the update the user chose to skip.
	SkippedUpdate int `json:"skippedUpdate"`
	// Connect when the computer starts (the service is started by Windows before anyone signs in).
	AutoConnect bool `json:"autoConnect"`
	// Whether the VPN was on when the service stopped, for AutoConnect.
	WasConnected bool `json:"wasConnected"`
	// Stable per-device id sent as x-hwid, so the panel's device limit works when it is enabled.
	DeviceID string `json:"deviceId,omitempty"`
	Tun      Tun    `json:"tun"`
}

// Defaults of a fresh install.
// Kinds of ping (Settings.PingType).
const (
	PingViaProxy = "proxy"
	PingTCP      = "tcp"
	PingICMP     = "icmp"
)

func Defaults() Settings {
	return Settings{
		ExcludedApps:             []string{},
		UseRecommendedExclusions: true,
		RecommendedOverrides:     []string{},
		VPNApps:                  []string{},
		Share:                    Share{HTTPPort: DefaultHTTPPort, SocksPort: DefaultSocksPort},
		SubscriptionUpdateHours:  UpdateFromSubscription,
		Routing:                  Routing{Enabled: true, LANDirect: true, Direct: []string{}, Proxy: []string{}, Block: []string{}},
		WhitelistDetect:          true,
		WhitelistAutoSwitch:      true,
		PingType:                 PingViaProxy,
		Tun:                      DefaultTun(),
	}
}

// Store keeps the settings in memory and in a file; every change is saved at once.
type Store struct {
	mu       sync.Mutex
	path     string
	s        Settings
	onChange func(old, new Settings)
}

// Open reads the file, or starts with the defaults.
func Open(path string) *Store {
	st := &Store{path: path, s: Defaults()}
	if data, err := os.ReadFile(path); err == nil {
		loaded := Defaults()
		if json.Unmarshal(data, &loaded) == nil {
			st.s = clean(loaded)
		}
	}
	return st
}

// OnChange is called after every change, outside the lock.
func (st *Store) OnChange(f func(old, new Settings)) {
	st.mu.Lock()
	st.onChange = f
	st.mu.Unlock()
}

// Get returns a copy of the settings.
func (st *Store) Get() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	return clone(st.s)
}

// Update changes the settings and saves them.
func (st *Store) Update(change func(*Settings)) (Settings, error) {
	st.mu.Lock()
	old := clone(st.s)
	next := clone(st.s)
	change(&next)
	next = clean(next)
	st.s = next
	err := st.save()
	f := st.onChange
	st.mu.Unlock()
	if f != nil {
		f(old, clone(next))
	}
	return clone(next), err
}

func (st *Store) save() error {
	data, err := json.MarshalIndent(st.s, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomically(st.path, data)
}

// WriteAtomically writes a file so that it is either the old one or the new one, never half.
func WriteAtomically(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func clone(s Settings) Settings {
	s.ExcludedApps = slices.Clone(s.ExcludedApps)
	s.RecommendedOverrides = slices.Clone(s.RecommendedOverrides)
	s.VPNApps = slices.Clone(s.VPNApps)
	s.Routing.Direct = slices.Clone(s.Routing.Direct)
	s.Routing.Proxy = slices.Clone(s.Routing.Proxy)
	s.Routing.Block = slices.Clone(s.Routing.Block)
	return s
}

// clean keeps lists non-nil and sorted (like the sets on Android) and ports in range.
func clean(s Settings) Settings {
	s.ExcludedApps = sortedSet(s.ExcludedApps)
	s.RecommendedOverrides = sortedSet(s.RecommendedOverrides)
	s.VPNApps = sortedSet(s.VPNApps)
	s.Routing.Direct = sortedSet(s.Routing.Direct)
	s.Routing.Proxy = sortedSet(s.Routing.Proxy)
	s.Routing.Block = sortedSet(s.Routing.Block)
	if s.Share.HTTPPort < MinPort || s.Share.HTTPPort > MaxPort {
		s.Share.HTTPPort = DefaultHTTPPort
	}
	if s.Share.SocksPort < MinPort || s.Share.SocksPort > MaxPort {
		s.Share.SocksPort = DefaultSocksPort
	}
	if s.SubscriptionUpdateHours < UpdateManual || s.SubscriptionUpdateHours > 48 {
		s.SubscriptionUpdateHours = UpdateFromSubscription
	}
	if s.PingType != PingTCP && s.PingType != PingICMP {
		s.PingType = PingViaProxy
	}
	if s.Tun.MTU < MinMTU || s.Tun.MTU > MaxMTU {
		s.Tun.MTU = DefaultMTU
	}
	if s.Tun.ProxyPort < MinPort || s.Tun.ProxyPort > MaxPort {
		s.Tun.ProxyPort = DefaultProxyPort
	}
	return s
}

func sortedSet(list []string) []string {
	out := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, s := range list {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}

// AddRules puts entries into one routing list, removing them from the other two.
func (r *Routing) AddRules(action routing.Action, entries []string) {
	for _, a := range routing.Actions {
		if a == action {
			r.set(a, append(r.Rules(a), entries...))
		} else {
			r.set(a, without(r.Rules(a), entries))
		}
	}
}

// RemoveRule deletes one entry from a list.
func (r *Routing) RemoveRule(action routing.Action, entry string) {
	r.set(action, without(r.Rules(action), []string{entry}))
}

// ReplaceRule replaces one entry with what it was edited into, in the same list (moving it out
// of the others).
func (r *Routing) ReplaceRule(action routing.Action, old string, entries []string) {
	for _, a := range routing.Actions {
		if a == action {
			r.set(a, append(without(r.Rules(a), []string{old}), entries...))
		} else {
			r.set(a, without(r.Rules(a), entries))
		}
	}
}

// ClearRules empties one list.
func (r *Routing) ClearRules(action routing.Action) { r.set(action, []string{}) }

// Import takes routing from a file: replace takes everything from it, otherwise its addresses are
// added to the user's lists (moving out of the other lists, like adding them by hand).
func (r *Routing) Import(from Routing, replace bool) {
	if replace {
		*r = from
		return
	}
	for _, a := range routing.Actions {
		var elsewhere []string
		for _, b := range routing.Actions {
			if b != a {
				elsewhere = append(elsewhere, from.Rules(b)...)
			}
		}
		r.set(a, append(without(r.Rules(a), elsewhere), from.Rules(a)...))
	}
}

func without(list, remove []string) []string {
	drop := map[string]bool{}
	for _, s := range remove {
		drop[s] = true
	}
	out := make([]string, 0, len(list))
	for _, s := range list {
		if !drop[s] {
			out = append(out, s)
		}
	}
	return out
}

// Exclusions are the programs that go around the VPN: the user's own choice plus the server's
// recommended list. With OnlySelected, it is the other way round: only the programs in Selected
// go through the VPN.
type Exclusions struct {
	Manual         []string `json:"manual"`
	Recommended    []string `json:"recommended"`
	UseRecommended bool     `json:"useRecommended"`
	Overrides      []string `json:"overrides"`
	OnlySelected   bool     `json:"onlySelected"`
	Selected       []string `json:"selected"`
}

// ExclusionsOf combines the settings with the server's list.
func ExclusionsOf(s Settings, recommended []string) Exclusions {
	return Exclusions{
		Manual:         slices.Clone(s.ExcludedApps),
		Recommended:    sortedSet(recommended),
		UseRecommended: s.UseRecommendedExclusions,
		Overrides:      slices.Clone(s.RecommendedOverrides),
		OnlySelected:   s.OnlySelectedApps,
		Selected:       slices.Clone(s.VPNApps),
	}
}

// Effective is the final list of programs without VPN (when not OnlySelected), compared
// case-insensitively like Windows file names.
func (e Exclusions) Effective() []string {
	out := slices.Clone(e.Manual)
	if e.UseRecommended {
		for _, r := range e.Recommended {
			if !containsFold(e.Overrides, r) && !containsFold(out, r) {
				out = append(out, r)
			}
		}
	}
	return out
}

// IsRecommended reports whether the program is on the server's list and the list is on.
func (e Exclusions) IsRecommended(name string) bool {
	return e.UseRecommended && containsFold(e.Recommended, name)
}

// SetAppExcluded includes or excludes one program, keeping manual choices and recommended-list
// overrides consistent.
func (s *Settings) SetAppExcluded(name string, excluded bool, recommended []string) {
	isRecommended := s.UseRecommendedExclusions && containsFold(recommended, name)
	if excluded {
		s.RecommendedOverrides = withoutFold(s.RecommendedOverrides, name)
		if !isRecommended && !containsFold(s.ExcludedApps, name) {
			s.ExcludedApps = append(s.ExcludedApps, name)
		}
	} else {
		s.ExcludedApps = withoutFold(s.ExcludedApps, name)
		if isRecommended && !containsFold(s.RecommendedOverrides, name) {
			s.RecommendedOverrides = append(s.RecommendedOverrides, name)
		}
	}
}

// SetAppSelected puts one program into the list of those that go through the VPN while only
// selected programs do, or takes it out.
func (s *Settings) SetAppSelected(name string, selected bool) {
	s.VPNApps = withoutFold(s.VPNApps, name)
	if selected {
		s.VPNApps = append(s.VPNApps, name)
	}
}

// AppsImport is "Программы без VPN" (or "через VPN") from a file.
type AppsImport struct {
	UseRecommended bool     `json:"useRecommended"`
	Excluded       []string `json:"excluded"`
	ReturnedToVPN  []string `json:"returnedToVpn"`
	OnlySelected   bool     `json:"onlySelected"`
	VPNApps        []string `json:"vpnApps"`
}

// ImportApps applies a file: replace takes the whole setup, otherwise its programs are added to
// both lists and the mode stays as it is.
func (s *Settings) ImportApps(apps AppsImport, replace bool) {
	if replace {
		s.ExcludedApps = slices.Clone(apps.Excluded)
		s.UseRecommendedExclusions = apps.UseRecommended
		s.RecommendedOverrides = slices.Clone(apps.ReturnedToVPN)
		s.OnlySelectedApps = apps.OnlySelected
		s.VPNApps = slices.Clone(apps.VPNApps)
		return
	}
	for _, name := range apps.Excluded {
		if !containsFold(s.ExcludedApps, name) {
			s.ExcludedApps = append(s.ExcludedApps, name)
		}
		s.RecommendedOverrides = withoutFold(s.RecommendedOverrides, name)
	}
	for _, name := range apps.VPNApps {
		if !containsFold(s.VPNApps, name) {
			s.VPNApps = append(s.VPNApps, name)
		}
	}
}

// Program names are file names, which Windows compares without case.
func containsFold(list []string, s string) bool {
	return slices.IndexFunc(list, func(x string) bool { return strings.EqualFold(x, s) }) >= 0
}

func withoutFold(list []string, s string) []string {
	return slices.DeleteFunc(slices.Clone(list), func(x string) bool { return strings.EqualFold(x, s) })
}

// NewShareUser makes a login for the share proxy.
func NewShareUser() string { return "furk_" + token(8, "0123456789abcdef") }

// NewSharePassword makes a password for the share proxy.
func NewSharePassword() string {
	return token(14, "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789")
}

func token(n int, alphabet string) string {
	out := make([]byte, 0, n)
	// Bytes past the last whole multiple of the alphabet are skipped, so every letter is equally likely.
	limit := 256 - 256%len(alphabet)
	buf := make([]byte, 32)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			panic(errors.New("no randomness: " + err.Error()))
		}
		for _, b := range buf {
			if int(b) < limit && len(out) < n {
				out = append(out, alphabet[int(b)%len(alphabet)])
			}
		}
	}
	return string(out)
}
