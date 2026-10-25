package subs

import (
	"fmt"
	"strings"
	"testing"
)

func config(remarks string, balancer bool, addresses ...string) string {
	var outbounds []string
	for i, address := range addresses {
		tag := "proxy"
		if i > 0 {
			tag = fmt.Sprintf("proxy-%d", i)
		}
		outbounds = append(outbounds, fmt.Sprintf(`{"tag":%q,"protocol":"vless","settings":{"vnext":[{"address":%q,"port":443,"users":[{"id":"x"}]}]},
			"streamSettings":{"network":"xhttp","security":"reality"}}`, tag, address))
	}
	outbounds = append(outbounds, `{"tag":"direct","protocol":"freedom"}`)
	routing := ""
	if balancer {
		routing = `,"routing":{"balancers":[{"tag":"b","selector":["proxy"]}]}`
	}
	return fmt.Sprintf(`{"remarks":%q,"outbounds":[%s]%s}`, remarks, strings.Join(outbounds, ","), routing)
}

func TestEntriesFromRemnawaveJSON(t *testing.T) {
	body := "[" + strings.Join([]string{
		config("⬇️ Автовыбор ⬇️", false, "0.0.0.0"),
		config("🇪🇺 Автовыбор Wi-Fi 🎲", true, "a.net", "b.net"),
		config("⬇️ Внизу Домашние (Wi-Fi) ⬇️", false, "0.0.0.0"),
		config("🇳🇱 Нидерланды ⚡", false, "nl.net"),
		config("🇳🇱 Нидерланды ⚡", false, "nl2.net"),
		config("Без флага", false, "x.net"),
		`{"remarks":"not a config"}`,
	}, ",") + "]"
	configs, err := ParseConfigs([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	entries := Entries(configs)
	kinds := []Kind{KindSeparator, KindAuto, KindSeparator, KindServer, KindServer, KindServer}
	if len(entries) != len(kinds) {
		t.Fatalf("got %d entries", len(entries))
	}
	for i, k := range kinds {
		if entries[i].Kind != k {
			t.Errorf("entry %d: kind %s, want %s", i, entries[i].Kind, k)
		}
	}
	check := func(got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	check(entries[0].Title, "Автовыбор")
	check(entries[2].Title, "Домашние (Wi-Fi)")
	check(entries[1].Flag, "🇪🇺")
	check(entries[1].Title, "Автовыбор Wi-Fi 🎲")
	check(entries[3].Summary, "VLESS · XHTTP · REALITY")
	// Duplicate names get distinct, stable keys.
	check(entries[3].Key, "🇳🇱 Нидерланды ⚡#1")
	check(entries[4].Key, "🇳🇱 Нидерланды ⚡#2")
	check(entries[5].Flag, "")
	check(entries[5].Title, "Без флага")
	check(entries[3].DisplayName(), "🇳🇱 Нидерланды ⚡")
}

func TestHeaders(t *testing.T) {
	if got := DecodeHeader("base64:RnVya1ZQTiB8INCf0L7QtNC/0LjRgdC60LA="); got != "FurkVPN | Подписка" {
		t.Fatal(got)
	}
	if DecodeHeader("plain") != "plain" || DecodeHeader("") != "" {
		t.Fatal("plain headers")
	}
	info := ParseUserInfo("upload=1; download=2; total=0; expire=1790000000")
	if info["upload"] != 1 || info["download"] != 2 || info["total"] != 0 || info["expire"] != 1790000000 || len(info) != 4 {
		t.Fatalf("%v", info)
	}
}
