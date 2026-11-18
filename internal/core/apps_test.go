package core

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/proxy"

	"furkvpn/internal/xrayconf"
)

// "Только выбранные программы через VPN" in a real Xray instance, built the way the app builds it:
// programs reach it through the local proxy port (as with TUN off), "the VPN server" and "the
// site" are two local servers that answer who they are, and the program is this test itself.
func TestOnlyChosenProgramsGoThroughTheVPN(t *testing.T) {
	answer := func(who string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, who) }))
	}
	site, vpn := answer("direct"), answer("vpn")
	defer site.Close()
	defer vpn.Close()
	// Like the panel's templates: BitTorrent blocked, a list of sites directly, the rest through
	// the server. The BitTorrent rule only works if the chosen programs' connections are looked
	// into again after they come back.
	template := fmt.Sprintf(`{
	  "inbounds": [{"tag": "socks", "protocol": "socks", "port": 10808,
	    "sniffing": {"enabled": true, "destOverride": ["http", "tls", "quic"], "routeOnly": true}}],
	  "outbounds": [
	    {"tag": "direct", "protocol": "freedom"},
	    {"tag": "proxy", "protocol": "freedom", "settings": {"redirect": %q}},
	    {"tag": "block", "protocol": "blackhole"}
	  ],
	  "routing": {"rules": [
	    {"type": "field", "protocol": ["bittorrent"], "outboundTag": "block"},
	    {"type": "field", "domain": ["full:ru.test"], "outboundTag": "direct"},
	    {"type": "field", "network": "tcp,udp", "outboundTag": "proxy"}
	  ]}
	}`, vpn.Listener.Addr().String())
	self := filepath.Base(os.Args[0])

	run := func(t *testing.T, opt xrayconf.Options) proxy.Dialer {
		t.Helper()
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		opt.LocalPort = l.Addr().(*net.TCPAddr).Port
		l.Close()
		opt.LogLevel = "none"
		config, err := xrayconf.Build([]byte(template), opt)
		if err != nil {
			t.Fatal(err)
		}
		if err := Start(config); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = Stop() })
		dialer, err := proxy.SOCKS5("tcp", l.Addr().String(), nil, proxy.Direct)
		if err != nil {
			t.Fatal(err)
		}
		return dialer
	}
	// send writes request to the site through the proxy and returns everything that comes back.
	send := func(t *testing.T, dialer proxy.Dialer, request string) string {
		t.Helper()
		conn, err := dialer.Dial("tcp", site.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := io.WriteString(conn, request); err != nil {
			t.Fatal(err)
		}
		reply, _ := io.ReadAll(conn)
		return string(reply)
	}
	get := func(host string) string {
		return "GET / HTTP/1.1\r\nHost: " + host + "\r\nConnection: close\r\n\r\n"
	}
	// A BitTorrent handshake; the line breaks make the site answer it with an error when it gets it.
	bitTorrent := "\x13BitTorrent protocol" + strings.Repeat("\x00", 8) + strings.Repeat("a", 40) + "\r\n\r\n"
	body := func(reply string) string {
		_, b, _ := strings.Cut(reply, "\r\n\r\n")
		return b
	}

	t.Run("chosen", func(t *testing.T) {
		d := run(t, xrayconf.Options{OnlyApps: true, VPNApps: []string{self + ".exe"}, Excluded: []string{self}})
		if got := body(send(t, d, get("example.com"))); got != "vpn" {
			t.Errorf("a chosen program goes through the VPN: %q", got)
		}
		if got := body(send(t, d, get("ru.test"))); got != "direct" {
			t.Errorf("the server's rules still apply to it: %q", got)
		}
		if got := send(t, d, bitTorrent); got != "" {
			t.Errorf("BitTorrent is blocked for it: %q", got)
		}
	})
	t.Run("others", func(t *testing.T) {
		d := run(t, xrayconf.Options{OnlyApps: true, VPNApps: []string{"Telegram.exe"}})
		if got := body(send(t, d, get("example.com"))); got != "direct" {
			t.Errorf("any other program goes directly: %q", got)
		}
		if got := send(t, d, bitTorrent); got == "" {
			t.Error("and the server's rules don't touch it")
		}
	})
	t.Run("none chosen", func(t *testing.T) {
		d := run(t, xrayconf.Options{OnlyApps: true})
		if got := body(send(t, d, get("example.com"))); got != "direct" {
			t.Errorf("everything goes directly: %q", got)
		}
	})
	t.Run("usual mode", func(t *testing.T) {
		d := run(t, xrayconf.Options{VPNApps: []string{"Telegram.exe"}})
		if got := body(send(t, d, get("example.com"))); got != "vpn" {
			t.Errorf("the list of programs through VPN is not used: %q", got)
		}
	})
}
