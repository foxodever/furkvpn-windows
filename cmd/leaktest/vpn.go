package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"furkvpn/internal/core"
)

// runVPN starts the config whose name contains name as the VPN's own instance (a socks inbound
// instead of the TUN) and reports while its health checks run.
func runVPN(configs []json.RawMessage, name string, minutes int, out string) {
	for _, c := range configs {
		var cfg map[string]any
		_ = json.Unmarshal(c, &cfg)
		remarks, _ := cfg["remarks"].(string)
		if !strings.Contains(remarks, name) {
			continue
		}
		cfg["inbounds"] = []any{map[string]any{"protocol": "socks", "listen": "127.0.0.1", "port": 10899}}
		cfg["log"] = map[string]any{"loglevel": "none"}
		b, _ := json.Marshal(cfg)
		fmt.Println("running", remarks)
		report("before start")
		if err := core.Start(b); err != nil {
			panic(err)
		}
		for m := 1; m <= minutes*2; m++ {
			time.Sleep(30 * time.Second)
			report(fmt.Sprintf("running %d s", m*30))
		}
		dump(out, "vpn")
		_ = core.Stop()
		for _, wait := range []int{5, 35} {
			time.Sleep(time.Duration(wait) * time.Second)
			report(fmt.Sprintf("stopped +%d s", wait))
		}
		dump(out, "vpn-stopped")
		return
	}
	panic("no such config")
}
