package keys

import "testing"

var allowed = []string{"*.furkvpn.com", "furkconnection.net"}

func ok(t *testing.T, input string) Key {
	t.Helper()
	r := Parse(input, allowed)
	if r.Status != OK {
		t.Fatalf("expected OK for %q, got %v", input, r.Status)
	}
	return r.Key
}

func TestPlainSubscriptionLink(t *testing.T) {
	key := ok(t, "https://sub.furkvpn.com/abcDEF123456XYZ0")
	if key.URL != "https://sub.furkvpn.com/abcDEF123456XYZ0" || key.Host != "sub.furkvpn.com" || key.Path != "/abcDEF123456XYZ0" {
		t.Fatalf("got %+v", key)
	}
	if key.ShortID() != "abcDEF123456XYZ0" {
		t.Fatalf("short id %q", key.ShortID())
	}
}

func TestClientSuffixAndSlashAreRemoved(t *testing.T) {
	cases := map[string]string{
		"https://sub.furkvpn.com/abcDEF123456/json":         "https://sub.furkvpn.com/abcDEF123456",
		"https://sub.furkvpn.com/abcDEF123456/":             "https://sub.furkvpn.com/abcDEF123456",
		"http://furkconnection.net/abcDEF123456/v2ray-json": "https://furkconnection.net/abcDEF123456",
		"https://sub.furkvpn.com:8443/abcDEF123456":         "https://sub.furkvpn.com:8443/abcDEF123456",
		"http://sub.furkvpn.com:443/abcDEF123456":           "https://sub.furkvpn.com/abcDEF123456",
		"https://SUB.FurkVPN.com/abcDEF123456/JSON":         "https://sub.furkvpn.com/abcDEF123456",
	}
	for in, want := range cases {
		if got := ok(t, in).URL; got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}

func TestBotImportLinks(t *testing.T) {
	cases := map[string]string{
		"https://furkapp.net/import?url=https%3A%2F%2Fsub.furkvpn.com%2FabcDEF123456": "https://sub.furkvpn.com/abcDEF123456",
		"https://furkapp.net/import?url=https://sub.furkvpn.com/abcDEF123456":         "https://sub.furkvpn.com/abcDEF123456",
		"furkvpn://import?url=https%3A%2F%2Ffurkconnection.net%2FabcDEF123456":        "https://furkconnection.net/abcDEF123456",
		"furkvpn://add/https%3A%2F%2Fsub.furkvpn.com%2FabcDEF123456":                  "https://sub.furkvpn.com/abcDEF123456",
		"\"furkvpn://import?url=https%3A%2F%2Ffurkconnection.net%2FabcDEF123456\"":    "https://furkconnection.net/abcDEF123456",
	}
	for in, want := range cases {
		if got := ok(t, in).URL; got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}

func TestLinkInsideMessageText(t *testing.T) {
	text := "Ваш ключ:\nhttps://sub.furkvpn.com/abcDEF123456.\nПриятного пользования!"
	if got := ok(t, text).URL; got != "https://sub.furkvpn.com/abcDEF123456" {
		t.Fatal(got)
	}
	if got := ok(t, "sub.furkvpn.com/abcDEF123456").URL; got != "https://sub.furkvpn.com/abcDEF123456" {
		t.Fatal(got)
	}
}

func TestForeignAndBrokenKeys(t *testing.T) {
	cases := map[string]Status{
		"https://sub.fgnconnection.com/abcDEF123456":  Foreign,
		"https://furkvpn.com.evil.net/abcDEF123456":   Foreign,
		"https://evilfurkconnection.net/abcDEF123456": Foreign,
		"vless://uuid@1.2.3.4:443?security=reality#x": NotALink,
		"просто текст":                                NotALink,
		"https://sub.furkvpn.com/":                    NotALink,
		"https://furkapp.net/import":                  NotALink,
		"   ":                                         Empty,
	}
	for in, want := range cases {
		if got := Parse(in, allowed).Status; got != want {
			t.Errorf("%q: got %v, want %v", in, got, want)
		}
	}
}

func TestWildcardMatching(t *testing.T) {
	yes := []string{"sub.furkvpn.com", "a.b.furkvpn.com", "SUB.FURKVPN.COM.", "furkconnection.net"}
	no := []string{"furkvpn.com", "sub.furkconnection.net"}
	for _, h := range yes {
		if !HostAllowed(h, allowed) {
			t.Errorf("%s should be allowed", h)
		}
	}
	for _, h := range no {
		if HostAllowed(h, allowed) {
			t.Errorf("%s should not be allowed", h)
		}
	}
}

func TestKeyAfterOtherLinksInAMessage(t *testing.T) {
	message := "Бот: https://t.me/FurkVPNbot\nСайт: furkvpn.info\nВаш ключ: https://sub.furkvpn.com/abcDEF123456XYZ0"
	if got := ok(t, message).URL; got != "https://sub.furkvpn.com/abcDEF123456XYZ0" {
		t.Fatal(got)
	}
	if r := Parse("https://t.me/FurkVPNbot https://example.com/x", allowed); r.Status != Foreign {
		t.Fatalf("%+v", r)
	}
}

func TestTVCodes(t *testing.T) {
	for in, want := range map[string]string{
		"https://furkapp.net/tv/ABC123": "ABC123", " furkvpn://tv/abc-123/ ": "ABC123",
		"https://furkapp.net/tv/": "", "https://evil.example/tv/ABC123": "", "https://sub.furkvpn.com/abcDEF123456": "",
	} {
		if got := TVCode(in); got != want {
			t.Errorf("TVCode(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"abc 123": "ABC123", "ABC-123": "ABC123", "ABC12": "", "ABC1234": "", "AB!123": "",
		"3РГ ЯОА": "3HUZJF", "3рг-яоа": "3HUZJF"} {
		if got := NormalizeTVCode(in); got != want {
			t.Errorf("NormalizeTVCode(%q) = %q, want %q", in, got, want)
		}
	}
}
