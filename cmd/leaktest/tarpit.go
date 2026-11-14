package main

import (
	"fmt"
	"io"
	"net"
	"time"

	"furkvpn/internal/core"
)

// runTarpit runs, as the VPN, an auto-select over two servers that accept the connection and never
// answer the TLS handshake, and sends some traffic of its own through it.
func runTarpit(out string, minutes int) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(io.Discard, c) }()
		}
	}()
	port := l.Addr().(*net.TCPAddr).Port
	server := func(tag, network string) string {
		return fmt.Sprintf(`{"tag": %q, "protocol": "vless", "settings": {"vnext": [{"address": "127.0.0.1", "port": %d,
		  "users": [{"id": "6b1e7a3c-0d64-4c7e-9f00-000000000001", "encryption": "none"}]}]},
		  "streamSettings": {"network": %q, "security": "tls", "tlsSettings": {"serverName": "example.com", "fingerprint": "chrome"}}}`, tag, port, network)
	}
	cfg := `{"log": {"loglevel": "none"},
	  "inbounds": [{"protocol": "socks", "listen": "127.0.0.1", "port": 10898}],
	  "outbounds": [` + server("proxy", "tcp") + `,` + server("proxy-2", "ws") + `,` + server("proxy-3", "xhttp") + `,` + server("proxy-4", "grpc") + `, {"tag": "direct", "protocol": "freedom"}],
	  "routing": {"rules": [{"type": "field", "network": "tcp,udp", "balancerTag": "auto"}],
	    "balancers": [{"tag": "auto", "selector": ["proxy"], "strategy": {"type": "leastLoad"}}]},
	  "burstObservatory": {"subjectSelector": ["proxy"], "pingConfig": {"destination": "http://www.gstatic.com/generate_204", "interval": "4s", "sampling": 2, "timeout": "3s"}}}`
	report("before start")
	if err := core.Start([]byte(cfg)); err != nil {
		panic(err)
	}
	for m := 1; m <= minutes*3; m++ {
		time.Sleep(20 * time.Second)
		report(fmt.Sprintf("running %d s", m*20))
	}
	dump(out, "tarpit")
	_ = core.Stop()
	for _, wait := range []int{5, 35} {
		time.Sleep(time.Duration(wait) * time.Second)
		report(fmt.Sprintf("stopped +%d s", wait))
	}
	dump(out, "tarpit-stopped")
}
