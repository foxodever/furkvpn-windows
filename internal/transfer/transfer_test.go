package transfer

import (
	"errors"
	"reflect"
	"testing"

	"furkvpn/internal/settings"
)

func TestRoutingRoundTrip(t *testing.T) {
	r := settings.Routing{
		Enabled: false, LANDirect: false, RUDirect: true,
		Direct: []string{"10.0.0.0/8", "domain:superhub.host"},
		Proxy:  []string{"domain:youtube.com"},
		Block:  []string{"full:ads.example.com", "keyword:tracker"},
	}
	parsed, err := Parse(ExportRouting(r))
	if err != nil || parsed.Kind != "routing" || parsed.Skipped != 0 {
		t.Fatalf("%v %+v", err, parsed)
	}
	if !reflect.DeepEqual(*parsed.Routing, r) {
		t.Fatalf("%+v", *parsed.Routing)
	}
}

func TestAndroidRoutingFileIsValidated(t *testing.T) {
	text := `
		{"type": "furkvpn-routing", "version": 1, "something": "new",
		 "direct": ["superhub.host", "https://кремль.рф/page", "300.1.1.1", "geosite:ru"],
		 "proxy": ["1.2.3.4", "1.2.3.4"],
		 "block": ["SuperHub.host"]}`
	parsed, err := Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	r := parsed.Routing
	// Block wins over direct for the same address; duplicates are not errors.
	if !reflect.DeepEqual(r.Block, []string{"domain:superhub.host"}) ||
		!reflect.DeepEqual(r.Direct, []string{"domain:xn--e1ajeds9e.xn--p1ai"}) ||
		!reflect.DeepEqual(r.Proxy, []string{"1.2.3.4"}) {
		t.Fatalf("%+v", r)
	}
	if parsed.Skipped != 2 {
		t.Fatalf("skipped %d", parsed.Skipped)
	}
	// Missing fields keep the defaults.
	if !r.Enabled || !r.LANDirect {
		t.Fatal("defaults")
	}
}

func TestAppsRoundTrip(t *testing.T) {
	e := settings.Exclusions{
		Manual:         []string{"Telegram.exe", "Discord.exe"},
		Recommended:    []string{"max.exe"},
		UseRecommended: false,
		Overrides:      []string{"max.exe"},
		OnlySelected:   true,
		Selected:       []string{"firefox.exe", "Discord.exe"},
	}
	parsed, err := Parse(ExportApps(e))
	if err != nil || parsed.Kind != "apps" {
		t.Fatal(err)
	}
	want := settings.AppsImport{
		UseRecommended: false, Excluded: []string{"Discord.exe", "Telegram.exe"}, ReturnedToVPN: []string{"max.exe"},
		OnlySelected: true, VPNApps: []string{"Discord.exe", "firefox.exe"},
	}
	if !reflect.DeepEqual(*parsed.Apps, want) {
		t.Fatalf("%+v", *parsed.Apps)
	}
}

// Files from before "only the chosen programs through VPN" are still the usual mode.
func TestOldAppsFile(t *testing.T) {
	parsed, err := Parse([]byte(`{"type":"furkvpn-windows-apps","version":1,"useRecommended":true,"excluded":["Telegram.exe"],"returnedToVpn":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Apps.OnlySelected || parsed.Apps.VPNApps == nil || len(parsed.Apps.VPNApps) != 0 {
		t.Fatalf("%+v", parsed.Apps)
	}
}

func TestAppsFromFileAreValidated(t *testing.T) {
	text := `{"type":"furkvpn-windows-apps","excluded":["Telegram.exe"," steam ","C:\\Games\\game.EXE","bad|name.exe",".exe","TELEGRAM.EXE"],"returnedToVpn":["telegram.exe","max.exe"]}`
	parsed, err := Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed.Apps.Excluded, []string{"Telegram.exe", "steam.exe", "game.EXE"}) {
		t.Fatalf("%q", parsed.Apps.Excluded)
	}
	if !reflect.DeepEqual(parsed.Apps.ReturnedToVPN, []string{"max.exe"}) {
		t.Fatalf("%q", parsed.Apps.ReturnedToVPN)
	}
	if parsed.Skipped != 2 || !parsed.Apps.UseRecommended {
		t.Fatalf("%+v", parsed)
	}
}

func TestForeignFilesAreRejected(t *testing.T) {
	for _, text := range []string{"", "not json", "[]", "{}", `{"type":"other"}`, `{"type":"furkvpn-routing","direct":"x"}`, `{"type":"furkvpn-apps","excluded":["org.telegram"]}`} {
		if _, err := Parse([]byte(text)); !errors.Is(err, ErrNotASettingsFile) {
			t.Errorf("%q: %v", text, err)
		}
	}
}
