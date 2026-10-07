package routing

import (
	"reflect"
	"testing"
)

func norm(t *testing.T, raw string) string {
	t.Helper()
	n, ok := Normalize(raw)
	if !ok {
		return "<invalid>"
	}
	return n
}

func TestWhatPeopleTypeBecomesARule(t *testing.T) {
	cases := map[string]string{
		"youtube.com":                    "domain:youtube.com",
		"https://youtube.com/watch?v=1":  "domain:youtube.com",
		"*.site.ru":                      "domain:.site.ru",
		"site.ru:443":                    "domain:site.ru",
		"кремль.рф":                      "domain:xn--e1ajeds9e.xn--p1ai",
		"1.2.3.4":                        "1.2.3.4",
		"10.0.0.0/8":                     "10.0.0.0/8",
		"2001:DB8::/32":                  "2001:db8::/32",
		"keyword:TikTok":                 "keyword:tiktok",
		"full:api.example.com":           "full:api.example.com",
		"YouTube.COM.":                   "domain:youtube.com",
	}
	for in, want := range cases {
		if got := norm(t, in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if d := Display("domain:xn--e1ajeds9e.xn--p1ai"); d != "кремль.рф" {
		t.Errorf("display %q", d)
	}
}

func TestLeadingDotIsKeptForTheListButNotForXray(t *testing.T) {
	if got := norm(t, ".com"); got != "domain:.com" {
		t.Fatal(got)
	}
	if got := Display("domain:.com"); got != ".com" {
		t.Fatal(got)
	}
	if got, _ := ToXray("domain:.com"); got != "domain:com" {
		t.Fatal(got)
	}
	if got, _ := ToXray("domain:youtube.com"); got != "domain:youtube.com" {
		t.Fatal(got)
	}
	if got := norm(t, ".рф"); got != "domain:.xn--p1ai" {
		t.Fatal(got)
	}
	if got := Display("domain:.xn--p1ai"); got != ".рф" {
		t.Fatal(got)
	}
	// Saved entries go through Normalize again when imported or built.
	if got := norm(t, "domain:.com"); got != "domain:.com" {
		t.Fatal(got)
	}
}

func TestKeywordsAndClashRules(t *testing.T) {
	cases := map[string]string{
		"KEYWORD:yandex":                "keyword:yandex",
		"DOMAIN-KEYWORD,yandex":         "keyword:yandex",
		"domain-keyword:Yandex":         "keyword:yandex",
		"DOMAIN-SUFFIX,ya.ru,DIRECT":    "domain:ya.ru",
		"DOMAIN,api.example.com":        "full:api.example.com",
		"IP-CIDR,1.2.3.0/24,no-resolve": "1.2.3.0/24",
		"IP-CIDR,300.1.1.0/24":          "<invalid>",
		// Our own "domain:" prefix is not Clash's DOMAIN (exact match).
		"domain:example.com": "domain:example.com",
	}
	for in, want := range cases {
		if got := norm(t, in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if KindOf("keyword:yandex") != KindKeyword || Display("keyword:yandex") != "yandex" || Editable("keyword:yandex") != "keyword:yandex" {
		t.Fatal("keyword display")
	}
	if Editable("domain:.com") != ".com" || Editable("full:a.ru") != "full:a.ru" || Editable("regexp:^a$") != "regexp:^a$" {
		t.Fatal("editable")
	}
	valid, invalid := ParseList("DOMAIN-KEYWORD,yandex\nDOMAIN-SUFFIX,vk.com,DIRECT\r\nkeyword:ozon, .su")
	if !reflect.DeepEqual(valid, []string{"keyword:yandex", "domain:vk.com", "keyword:ozon", "domain:.su"}) || len(invalid) != 0 {
		t.Fatalf("%q %q", valid, invalid)
	}
}

func TestBadEntriesAreRejected(t *testing.T) {
	for _, bad := range []string{"geosite:google", "300.1.1.1/8", "300.1.1.1", "1.2.3.4/33", "not a domain!", "regexp:(", "regexp:(?<=a)b", "fe80::1%eth0"} {
		if got, ok := Normalize(bad); ok {
			t.Errorf("%q accepted as %q", bad, got)
		}
	}
	if got := norm(t, "http://1.2.3.4/page"); got != "1.2.3.4" {
		t.Fatal(got)
	}
	if IsIPOrCIDR("1:2:3") || !IsIPOrCIDR("::ffff:1.2.3.4") {
		t.Fatal("ip check")
	}
	valid, invalid := ParseList("youtube.com, 1.1.1.1\n bad_host!, x.org, youtube.com")
	if !reflect.DeepEqual(valid, []string{"domain:youtube.com", "1.1.1.1", "domain:x.org"}) || !reflect.DeepEqual(invalid, []string{"bad_host!"}) {
		t.Fatalf("%q %q", valid, invalid)
	}
}
