package core

import (
	"fmt"
	"net"
	"testing"
	"time"
)

// TCP ping measures the server itself: it needs nothing to answer through it, takes the fastest
// server of an auto-select, and leaves a server on UDP to the usual ping through it.
func TestTCPPing(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	port := l.Addr().(*net.TCPAddr).Port
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	closedPort := closed.Addr().(*net.TCPAddr).Port
	closed.Close()
	vless := func(port int) string {
		return fmt.Sprintf(`{"protocol": "vless", "settings": {"vnext": [{"address": "127.0.0.1", "port": %d, "users": [{"id": "5783a3e7-e373-51cd-8642-c83782b807c5", "encryption": "none"}]}]}}`, port)
	}
	direct := `{"protocol": "freedom", "tag": "direct"}`

	one := `{"outbounds": [` + vless(port) + `,` + direct + `]}`
	if ms, err := MeasureDelayBy(TCP, []byte(one), "http://127.0.0.1:1/", time.Second); err != nil || ms < 0 {
		t.Errorf("a listening server: %d, %v", ms, err)
	}
	down := `{"outbounds": [` + vless(closedPort) + `,` + direct + `]}`
	if _, err := MeasureDelayBy(TCP, []byte(down), "http://127.0.0.1:1/", time.Second); err == nil {
		t.Error("a server that doesn't listen has no ping")
	}
	auto := `{"outbounds": [` + vless(closedPort) + `,` + vless(port) + `,` + direct + `],
	  "routing": {"balancers": [{"tag": "b", "selector": [""]}], "rules": []}}`
	if ms, err := MeasureDelayBy(TCP, []byte(auto), "http://127.0.0.1:1/", time.Second); err != nil || ms < 0 {
		t.Errorf("auto-select with one server up: %d, %v", ms, err)
	}

	cfg := map[string]any{"outbounds": []any{
		map[string]any{"protocol": "hysteria", "settings": map[string]any{"address": "192.0.2.1", "port": 443}},
		map[string]any{"protocol": "vless", "streamSettings": map[string]any{"network": "kcp"}, "settings": map[string]any{"address": "192.0.2.2", "port": 443}},
	}}
	list := endpoints(cfg, []int{0, 1})
	if len(list) != 2 || !list[0].udp || !list[1].udp {
		t.Fatalf("servers on UDP: %+v", list)
	}
	if _, err := measureDirect(list, TCP, time.Second); err != errNotApplicable {
		t.Errorf("TCP ping of UDP servers: %v", err)
	}
}
