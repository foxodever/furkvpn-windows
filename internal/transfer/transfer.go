// Package transfer reads and writes the settings files that can simply be sent to someone:
// JSON with a "type". Routing files are the same as the Android app's, so they move between
// phone and computer; the Windows program list has its own type. Everything read from a file is
// validated like typed entries, because one bad routing entry would stop the whole core.
package transfer

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"furkvpn/internal/routing"
	"furkvpn/internal/settings"
)

const (
	TypeRouting = "furkvpn-routing"
	TypeApps    = "furkvpn-windows-apps"
	MaxBytes    = 5 * 1024 * 1024
	MaxEntries  = 10_000

	RoutingFileName = "furkvpn-routing.json"
	AppsFileName    = "furkvpn-windows-apps.json"
)

// ErrNotASettingsFile: the text is not one of the app's settings files.
var ErrNotASettingsFile = errors.New("not a FurkVPN settings file")

type routingFile struct {
	Type      string   `json:"type"`
	Version   int      `json:"version"`
	Enabled   bool     `json:"enabled"`
	LANDirect bool     `json:"lanDirect"`
	RUDirect  bool     `json:"ruDirect"`
	Direct    []string `json:"direct"`
	Proxy     []string `json:"proxy"`
	Block     []string `json:"block"`
}

type appsFile struct {
	Type           string   `json:"type"`
	Version        int      `json:"version"`
	UseRecommended bool     `json:"useRecommended"`
	Excluded       []string `json:"excluded"`
	ReturnedToVPN  []string `json:"returnedToVpn"`
	// Only the programs in VPNApps go through the VPN. Files of older versions have neither, and
	// older versions read new files without them.
	OnlySelected bool     `json:"onlySelected"`
	VPNApps      []string `json:"vpnApps"`
}

// Import is a checked settings file, ready to apply. Skipped counts the entries that were not
// valid and were left out.
type Import struct {
	Kind    string               `json:"kind"` // "routing" or "apps"
	Routing *settings.Routing    `json:"routing,omitempty"`
	Apps    *settings.AppsImport `json:"apps,omitempty"`
	Skipped int                  `json:"skipped"`
}

// ExportRouting writes the routing settings.
func ExportRouting(r settings.Routing) []byte {
	f := routingFile{
		Type: TypeRouting, Version: 1, Enabled: r.Enabled, LANDirect: r.LANDirect, RUDirect: r.RUDirect,
		Direct: sorted(r.Direct), Proxy: sorted(r.Proxy), Block: sorted(r.Block),
	}
	data, _ := json.MarshalIndent(f, "", "    ")
	return data
}

// ExportApps writes the programs without VPN, and those through it.
func ExportApps(e settings.Exclusions) []byte {
	f := appsFile{
		Type: TypeApps, Version: 1, UseRecommended: e.UseRecommended, Excluded: sorted(e.Manual), ReturnedToVPN: sorted(e.Overrides),
		OnlySelected: e.OnlySelected, VPNApps: sorted(e.Selected),
	}
	data, _ := json.MarshalIndent(f, "", "    ")
	return data
}

func sorted(list []string) []string {
	out := slices.Clone(list)
	if out == nil {
		out = []string{}
	}
	slices.Sort(out)
	return out
}

// Parse checks a file.
func Parse(data []byte) (*Import, error) {
	if len(data) > MaxBytes {
		return nil, ErrNotASettingsFile
	}
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &head) != nil {
		return nil, ErrNotASettingsFile
	}
	switch head.Type {
	case TypeRouting:
		f := routingFile{Enabled: true, LANDirect: true}
		if json.Unmarshal(data, &f) != nil {
			return nil, ErrNotASettingsFile
		}
		return parseRouting(f), nil
	case TypeApps:
		f := appsFile{UseRecommended: true}
		if json.Unmarshal(data, &f) != nil {
			return nil, ErrNotASettingsFile
		}
		return parseApps(f), nil
	}
	return nil, ErrNotASettingsFile
}

func parseRouting(f routingFile) *Import {
	skipped := 0
	taken := map[string]bool{}
	// An address in several lists stays where it would take effect: block, then direct, then VPN.
	clean := func(entries []string) []string {
		out := []string{}
		for _, raw := range entries {
			entry, ok := routing.Normalize(raw)
			switch {
			case !ok || len(out) >= MaxEntries:
				skipped++
			case !taken[entry]:
				taken[entry] = true
				out = append(out, entry)
			}
		}
		return out
	}
	block := clean(f.Block)
	direct := clean(f.Direct)
	proxy := clean(f.Proxy)
	return &Import{
		Kind:    "routing",
		Routing: &settings.Routing{Enabled: f.Enabled, LANDirect: f.LANDirect, RUDirect: f.RUDirect, Direct: direct, Proxy: proxy, Block: block},
		Skipped: skipped,
	}
}

func parseApps(f appsFile) *Import {
	skipped := 0
	clean := func(names []string) []string {
		out := []string{}
		for _, raw := range names {
			name, ok := ProgramName(raw)
			if !ok || len(out) >= MaxEntries {
				skipped++
				continue
			}
			if !slices.ContainsFunc(out, func(x string) bool { return strings.EqualFold(x, name) }) {
				out = append(out, name)
			}
		}
		return out
	}
	excluded := clean(f.Excluded)
	returned := slices.DeleteFunc(clean(f.ReturnedToVPN), func(x string) bool {
		return slices.ContainsFunc(excluded, func(e string) bool { return strings.EqualFold(e, x) })
	})
	vpnApps := clean(f.VPNApps)
	return &Import{
		Kind: "apps",
		Apps: &settings.AppsImport{
			UseRecommended: f.UseRecommended, Excluded: excluded, ReturnedToVPN: returned,
			OnlySelected: f.OnlySelected, VPNApps: vpnApps,
		},
		Skipped: skipped,
	}
}

var badNameChars = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)

// ProgramName checks a program's file name ("Telegram.exe"); a path gives its file name, and
// ".exe" is added when it is missing.
func ProgramName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if i := strings.LastIndexAny(name, `\/`); i >= 0 {
		name = strings.TrimSpace(name[i+1:])
	}
	if !strings.HasSuffix(strings.ToLower(name), ".exe") {
		name += ".exe"
	}
	base := name[:len(name)-4]
	if strings.Trim(base, ". ") == "" || len(name) > 255 || badNameChars.MatchString(name) {
		return "", false
	}
	return name, true
}
