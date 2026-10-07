package remote

import (
	"strings"
	"testing"
)

func TestDefaultsWhenAPIHasNoSite(t *testing.T) {
	cfg, err := Decode([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Site != (SiteConfig{"furkvpn.info", "api.furkvpn.info"}) {
		t.Fatalf("site %+v", cfg.Site)
	}
	if cfg.Site.InstallURL() != "https://furkvpn.info/dashboard/install" {
		t.Fatal(cfg.Site.InstallURL())
	}
	if len(cfg.Whitelist.PingServers) != 2 || cfg.Windows.Update != nil {
		t.Fatalf("%+v", cfg)
	}
}

func TestDomainFromAPI(t *testing.T) {
	cfg, _ := Decode([]byte(`{"site":{"domain":"furkvpn2.info"}}`))
	if cfg.Site != (SiteConfig{"furkvpn2.info", "api.furkvpn.info"}) {
		t.Fatalf("site %+v", cfg.Site)
	}
}

func TestURLsBecomeHostNames(t *testing.T) {
	got := SiteConfig{" https://FurkVPN.info/ ", "https://api.furkvpn.info/v1?x=1"}.Sanitized()
	if got != (SiteConfig{"furkvpn.info", "api.furkvpn.info"}) {
		t.Fatalf("%+v", got)
	}
	if d := (SiteConfig{Domain: "lk.furkvpn.info:8443"}).Sanitized().Domain; d != "lk.furkvpn.info:8443" {
		t.Fatal(d)
	}
	if d := (SiteConfig{Domain: "фурк.рф"}).Sanitized().Domain; d != "xn--j1alid.xn--p1ai" {
		t.Fatal(d)
	}
}

func TestGarbageFallsBackToDefaults(t *testing.T) {
	for _, s := range []string{"", "  ", "furkvpn", "javascript:alert(1)", "evil.com\"><script>", "user@furkvpn.info", "1.2.3.4"} {
		if got := (SiteConfig{s, s}).Sanitized(); got != Defaults().Site {
			t.Errorf("%q: %+v", s, got)
		}
	}
}

func TestBrokenListsFallBack(t *testing.T) {
	cfg, _ := Decode([]byte(`{"allowedSubscriptionHosts":[],"support":[],"links":[{"type":"website","url":" "}],
		"serverFilters":[{"emoji":"","title":"x"},{"emoji":"🎮","title":"Игровой"}],"delayTestUrl":"",
		"whitelist":{"lteServer":["LTE"]},
		"windows":{"update":{"versionCode":3,"versionName":"1.0.2","url":"http://insecure"},"defaultExcludedApps":[" max.exe ",""]}}`))
	if len(cfg.AllowedSubscriptionHosts) != 2 || len(cfg.Support) != 3 || len(cfg.Links) != 0 {
		t.Fatalf("%+v", cfg)
	}
	if len(cfg.ServerFilters) != 1 || cfg.DelayTestURL == "" {
		t.Fatalf("%+v", cfg)
	}
	// Nested fields the API leaves out keep their defaults.
	if len(cfg.Whitelist.LTEServer) != 1 || len(cfg.Whitelist.WifiServer) != 2 {
		t.Fatalf("%+v", cfg.Whitelist)
	}
	if cfg.Windows.Update != nil {
		t.Fatal("an update over plain http must be ignored")
	}
	if len(cfg.Windows.DefaultExcludedApps) != 1 || cfg.Windows.DefaultExcludedApps[0] != "max.exe" {
		t.Fatalf("%q", cfg.Windows.DefaultExcludedApps)
	}
}

func TestWindowsProgramsAreFileNames(t *testing.T) {
	cfg, _ := Decode([]byte(`{"windows":{"defaultExcludedApps":["chrome", "Telegram.exe", "C:\\Program Files\\Max\\Max.exe", " ", "a|b"]}}`))
	want := []string{"chrome.exe", "Telegram.exe", "Max.exe"}
	if strings.Join(cfg.Windows.DefaultExcludedApps, ",") != strings.Join(want, ",") {
		t.Fatalf("%q", cfg.Windows.DefaultExcludedApps)
	}
}
