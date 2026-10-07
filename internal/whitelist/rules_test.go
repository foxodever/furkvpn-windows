package whitelist

import (
	"testing"

	"furkvpn/internal/remote"
	"furkvpn/internal/subs"
)

func server(name string, kind subs.Kind) subs.Entry {
	return subs.Entry{Key: name + "#1", Remarks: name, Title: name, Kind: kind}
}

var (
	wifiAuto = server("🇪🇺 Автовыбор Wi-Fi 🎲", subs.KindAuto)
	lteAuto  = server("🇷🇺 Автовыбор LTE 🎲", subs.KindAuto)
	nl       = server("🇳🇱 Нидерланды ⚡", subs.KindServer)
	bypass   = server("🇷🇺🦊 Все операторы ⚡️ (-Йота)", subs.KindServer)
	servers  = []subs.Entry{wifiAuto, lteAuto, nl, bypass}
	config   = remote.Defaults().Whitelist
)

func key(e *subs.Entry) string {
	if e == nil {
		return "<nil>"
	}
	return e.Key
}

func TestOnlyBypassServersArePingedUnderWhiteLists(t *testing.T) {
	if !WorksUnderWhitelist(bypass, config) || !WorksUnderWhitelist(server("🇷🇺 Москва LTE", subs.KindServer), config) {
		t.Fatal("bypass and LTE servers work under white lists")
	}
	if WorksUnderWhitelist(server("🇳🇱 Нидерланды ⚡🤖", subs.KindServer), config) {
		t.Fatal("a normal server does not")
	}
}

func TestClassify(t *testing.T) {
	if Classify(true, true) != AllOK || Classify(false, true) != Whitelist || Classify(false, false) != NoInternet {
		t.Fatal("classify")
	}
}

func TestWhiteListsOnMoveToLTEUnlessBypassChosen(t *testing.T) {
	check := func(got *subs.Entry, want string) {
		t.Helper()
		if key(got) != want {
			t.Errorf("got %s, want %s", key(got), want)
		}
	}
	check(Target(AllOK, Whitelist, nl, servers, config, ""), lteAuto.Key)
	check(Target(Unknown, Whitelist, wifiAuto, servers, config, ""), lteAuto.Key)
	check(Target(AllOK, Whitelist, bypass, servers, config, ""), "<nil>")
	check(Target(AllOK, Whitelist, lteAuto, servers, config, ""), "<nil>")
	check(Target(Whitelist, Whitelist, nl, servers, config, ""), "<nil>") // only on change
}

func TestWhiteListsOffMoveBackOnlyFromBypassOrOwnChoice(t *testing.T) {
	check := func(got *subs.Entry, want string) {
		t.Helper()
		if key(got) != want {
			t.Errorf("got %s, want %s", key(got), want)
		}
	}
	check(Target(Whitelist, AllOK, lteAuto, servers, config, lteAuto.Key), wifiAuto.Key)
	check(Target(Whitelist, AllOK, bypass, servers, config, ""), wifiAuto.Key)
	check(Target(Unknown, AllOK, lteAuto, servers, config, lteAuto.Key), wifiAuto.Key)
	check(Target(Whitelist, AllOK, nl, servers, config, ""), "<nil>")      // picked by hand
	check(Target(Unknown, AllOK, lteAuto, servers, config, ""), "<nil>")   // picked by hand
	check(Target(Unknown, AllOK, bypass, servers, config, ""), "<nil>")    // bypass on a normal network by choice
}
