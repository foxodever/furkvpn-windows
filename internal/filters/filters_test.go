package filters

import (
	"reflect"
	"testing"

	"furkvpn/internal/remote"
	"furkvpn/internal/subs"
)

func server(remarks string) subs.Entry {
	return subs.Entry{Key: remarks, Remarks: remarks, Title: remarks, Kind: subs.KindServer}
}

func header(title string) subs.Entry {
	return subs.Entry{Key: title, Remarks: title, Title: title, Kind: subs.KindSeparator}
}

var servers = []subs.Entry{
	header("Автовыбор"),
	{Key: "auto", Remarks: "🇪🇺 Автовыбор Wi-Fi 🎲", Flag: "🇪🇺", Title: "Автовыбор Wi-Fi 🎲", Kind: subs.KindAuto},
	header("Домашние (Wi-Fi)"),
	server("🇳🇱 Нидерланды ⚡🤖📺🏴‍☠️"), // no variation selectors
	server("🇩🇪 Германия ⚡🤖🎮"),
	server("🇺🇸 США 🤖"),
	header("Обходы (LTE)"),
	server("🇷🇺🦊 Все операторы ⚡️ (-Йота)"), // with a variation selector
}

func remarks(list []subs.Entry) []string {
	out := []string{}
	for _, e := range list {
		out = append(out, e.Remarks)
	}
	return out
}

func TestEmojiVariantsMatch(t *testing.T) {
	want := []string{"Домашние (Wi-Fi)", "🇳🇱 Нидерланды ⚡🤖📺🏴‍☠️", "🇩🇪 Германия ⚡🤖🎮", "Обходы (LTE)", "🇷🇺🦊 Все операторы ⚡️ (-Йота)"}
	if got := remarks(Apply(servers, "⚡️")); !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
	if got := remarks(Apply(servers, "🏴‍☠️")); !reflect.DeepEqual(got, []string{"Домашние (Wi-Fi)", "🇳🇱 Нидерланды ⚡🤖📺🏴‍☠️"}) {
		t.Fatalf("%q", got)
	}
	if got := Apply(servers, ""); !reflect.DeepEqual(got, servers) {
		t.Fatal("no filter must keep everything")
	}
}

func TestOnlyTabsWithServersAreShown(t *testing.T) {
	list := []remote.ServerFilter{{Emoji: "⚡️", Title: "Надёжный"}, {Emoji: "🎮", Title: "Игровой"}, {Emoji: "🦄", Title: "Нет таких"}}
	got := Available(list, servers)
	if len(got) != 2 || got[0].Emoji != "⚡️" || got[1].Emoji != "🎮" {
		t.Fatalf("%+v", got)
	}
}
