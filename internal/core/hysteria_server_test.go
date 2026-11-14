//go:build !race

// Xray's Hysteria server, which only this test runs, has a data race of its own (its
// udpSessionManager, hub.go/conn.go); the client side the app uses is not part of it.

package core

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The servers come from the panel the way Remnawave writes them (obfuscation and the old port
// hopping); the app connects to a Hysteria 2 server of the same core through them.
func TestHysteriaServer(t *testing.T) {
	certPEM, keyPEM, pin := selfSignedCert(t, "hy.furk.test")
	port := freeUDPPort(t)
	lines := func(b []byte) string {
		out, _ := json.Marshal(strings.Split(strings.TrimSpace(string(b)), "\n"))
		return string(out)
	}
	server, err := startTestInstance(mustParse(t, fmt.Sprintf(`{
	  "log": {"loglevel": "none"},
	  "inbounds": [{"protocol": "hysteria", "listen": "127.0.0.1", "port": %d,
	    "settings": {"version": 2, "clients": [{"auth": "secret-auth"}]},
	    "streamSettings": {"network": "hysteria", "hysteriaSettings": {"version": 2},
	      "security": "tls", "tlsSettings": {"alpn": ["h3"], "certificates": [{"certificate": %s, "key": %s}]},
	      "finalmask": {"udp": [{"type": "salamander", "settings": {"password": "obfs-secret"}}]}}}],
	  "outbounds": [{"protocol": "freedom", "settings": {"finalRules": [{"action": "allow"}]}}]}`, port, lines(certPEM), lines(keyPEM))))
	if err != nil {
		t.Fatal(err)
	}
	defer closeInstance(server)

	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer site.Close()
	client := func(auth string) string {
		return fmt.Sprintf(`{"outbounds": [{"tag": "proxy", "protocol": "hysteria",
	    "settings": {"version": 2, "address": "127.0.0.1", "port": %d},
	    "streamSettings": {"network": "hysteria", "hysteriaSettings": {"version": 2, "auth": %q},
	      "security": "tls", "tlsSettings": {"serverName": "hy.furk.test", "alpn": ["h3"], "pinnedPeerCertSha256": %q},
	      "finalmask": {"udp": [{"type": "salamander", "settings": {"password": "obfs-secret"}}],
	        "quicParams": {"udpHop": {"ports": "%d", "interval": "5"}}}}},
	    {"tag": "direct", "protocol": "freedom"}]}`, port, auth, pin, port)
	}
	if ms, err := MeasureDelay([]byte(client("secret-auth")), site.URL, 3*time.Second); err != nil || ms < 0 {
		t.Fatalf("delay: %d, %v", ms, err)
	}
	if _, err := MeasureDelay([]byte(client("wrong-auth")), site.URL, 3*time.Second); err == nil {
		t.Fatal("a wrong password connected")
	}
}

func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}
