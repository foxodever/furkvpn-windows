package core

import (
	"encoding/json"
	"reflect"
	"testing"

	"furkvpn/internal/jsonx"
)

func TestUpgradeHysteria(t *testing.T) {
	cfg := mustParse(t, `{"outbounds": [
	  {"tag": "hy", "protocol": "hysteria", "streamSettings": {"network": "hysteria", "finalmask": {
	    "udp": [{"type": "salamander", "settings": {"password": "obfs"}}],
	    "quicParams": {"brutalUp": "50 mbps", "udpHop": {"ports": "20000-30000", "interval": "5-10"}}}}},
	  {"tag": "no-interval", "protocol": "hysteria", "streamSettings": {"finalmask": {
	    "quicParams": {"udpHop": {"ports": 443}}}}},
	  {"tag": "no-ports", "protocol": "hysteria", "streamSettings": {"finalmask": {
	    "quicParams": {"udpHop": {"interval": 30}}}}},
	  {"tag": "new-form", "protocol": "hysteria", "streamSettings": {"finalmask": {
	    "udp": [{"type": "udphop", "settings": {"mode": "intervalRemote", "remotePorts": "1-2", "interval": 5}}]}}},
	  {"tag": "vless", "protocol": "vless", "streamSettings": {"network": "tcp"}}
	]}`)
	if !upgradeHysteria(cfg) {
		t.Fatal("nothing changed")
	}
	mask := func(n int) map[string]any {
		return jsonx.Obj(jsonx.Obj(jsonx.Obj(cfg["outbounds"].([]any)[n])["streamSettings"])["finalmask"])
	}
	want := []any{
		map[string]any{"type": "salamander", "settings": map[string]any{"password": "obfs"}},
		map[string]any{"type": "udphop", "settings": map[string]any{
			"mode": "intervalLocal,intervalRemote", "remotePorts": "20000-30000", "interval": "5-10"}},
	}
	if got := mask(0)["udp"]; !reflect.DeepEqual(got, want) {
		t.Errorf("udp = %v", got)
	}
	if got := mask(0)["quicParams"]; !reflect.DeepEqual(got, jsonx.Object{"brutalUp": "50 mbps"}) {
		t.Errorf("quicParams = %v", got)
	}
	settings := jsonx.Obj(jsonx.Obj(mask(1)["udp"].([]any)[0])["settings"])
	if settings["interval"] != defaultHopInterval || settings["remotePorts"] != json.Number("443") {
		t.Errorf("without an interval: %v", settings)
	}
	if _, ok := mask(2)["udp"]; ok {
		t.Errorf("without ports: %v", mask(2))
	}
	if got := len(mask(3)["udp"].([]any)); got != 1 {
		t.Errorf("new form: %d masks", got)
	}
	if upgradeHysteria(cfg) {
		t.Error("changed again")
	}
}
