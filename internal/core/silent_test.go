//go:build linux || windows

package core

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"reflect"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"
)

func TestTLSServerPorts(t *testing.T) {
	cfg := mustParse(t, `{"outbounds": [
	  {"protocol": "vless", "settings": {"vnext": [{"address": "a.net", "port": 443}]}, "streamSettings": {"network": "ws", "security": "tls"}},
	  {"protocol": "vless", "settings": {"address": "b.net", "port": 8443}, "streamSettings": {"network": "xhttp", "security": "reality"}},
	  {"protocol": "trojan", "settings": {"servers": [{"address": "c.net", "port": 443}]}, "streamSettings": {"security": "tls"}},
	  {"protocol": "vless", "settings": {"vnext": [{"address": "d.net", "port": 80}]}, "streamSettings": {"network": "ws"}},
	  {"protocol": "hysteria", "settings": {"address": "e.net", "port": 444}, "streamSettings": {"network": "hysteria", "security": "tls"}},
	  {"protocol": "freedom", "settings": {"port": 1}, "streamSettings": {"security": "tls"}}]}`)
	if got := tlsServerPorts(cfg); !reflect.DeepEqual(got, []string{"443", "8443"}) {
		t.Fatalf("ports = %v", got)
	}
}

// A server that takes the connection and never answers must not keep a handshake waiting for
// good, whatever started it (here nothing ever cancels: the way auto-select's checks dial).
func TestSilentServerIsDropped(t *testing.T) {
	silentMu.Lock()
	old := silentAfter
	silentAfter = 200 * time.Millisecond
	silentMu.Unlock()
	defer func() {
		silentMu.Lock()
		silentAfter = old
		silentMu.Unlock()
	}()
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
			go func() {
				defer c.Close()
				_, _ = io.Copy(io.Discard, c)
			}()
		}
	}()
	server := func(tag, network string) string {
		return fmt.Sprintf(`{"tag": %q, "protocol": "vless", "settings": {"vnext": [{"address": "127.0.0.1", "port": %d,
		  "users": [{"id": "6b1e7a3c-0d64-4c7e-9f00-000000000001", "encryption": "none"}]}]},
		  "streamSettings": {"network": %q, "security": "tls", "tlsSettings": {"serverName": "silent.furk.test", "fingerprint": "chrome"}}}`,
			tag, l.Addr().(*net.TCPAddr).Port, network)
	}
	tags := []string{"tcp", "xhttp"}
	client, err := startTestInstance(mustParse(t, `{"log": {"loglevel": "none"}, "outbounds": [`+server("tcp", "tcp")+`,`+server("xhttp", "xhttp")+`]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer closeInstance(client)
	for _, tag := range tags {
		ctx := session.SetForcedOutboundTagToContext(context.Background(), tag)
		conn, err := core.Dial(ctx, client, xnet.TCPDestination(xnet.DomainAddress("example.com"), 80))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		go conn.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
	}
	handshakes := func() int {
		var stacks bytes.Buffer
		_ = pprof.Lookup("goroutine").WriteTo(&stacks, 2)
		return strings.Count(stacks.String(), ".clientHandshake(")
	}
	waitFor := func(what string, want int, limit time.Duration) {
		t.Helper()
		deadline := time.Now().Add(limit)
		for handshakes() != want {
			if time.Now().After(deadline) {
				t.Fatalf("%s: %d handshakes are waiting, want %d", what, handshakes(), want)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitFor("both servers are being connected to", len(tags), 3*time.Second)
	waitFor("the silent connections are dropped", 0, 5*time.Second)
}
