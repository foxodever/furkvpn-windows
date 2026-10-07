package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"furkvpn/internal/core"
	"furkvpn/internal/ipc"
	"furkvpn/internal/settings"
)

func vlessConfig(remarks, address string) string {
	return fmt.Sprintf(`{"remarks":%q,"outbounds":[{"tag":"proxy","protocol":"vless","settings":{"vnext":[{"address":%q,"port":443,"users":[{"id":"6b1e7a3c-0d64-4c7e-9f00-000000000001","encryption":"none"}]}]}},{"tag":"direct","protocol":"freedom"}],
		"routing":{"rules":[{"type":"field","network":"tcp,udp","outboundTag":"proxy"}]},"dns":{"servers":["1.1.1.1"]}}`, remarks, address)
}

// testServer is furkapp.net and the subscription host at once.
func testServer(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"allowedSubscriptionHosts":["127.0.0.1"],"subscriptionMirrors":[],"windows":{"defaultExcludedApps":["max.exe"]}}`)
	})
	mux.HandleFunc("/api/v1/subscription/status", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "/abcDEF123456") {
			http.Error(w, "bad", 400)
			return
		}
		fmt.Fprint(w, `{"found":true,"planName":"Тест","expiresAt":2000000000,"isFree":false}`)
	})
	mux.HandleFunc("/abcDEF123456/json", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hwid") == "" || r.Header.Get("X-Device-Os") != "Windows" {
			http.Error(w, "no device headers", 400)
			return
		}
		w.Header().Set("profile-update-interval", "3")
		fmt.Fprint(w, "["+vlessConfig("🇳🇱 Нидерланды", "192.0.2.1")+","+vlessConfig("🇩🇪 Германия", "192.0.2.2")+"]")
	})
	s := httptest.NewTLSServer(mux)
	testTLS = s.Client().Transport.(*http.Transport).TLSClientConfig
	t.Cleanup(s.Close)
	return s
}

// newEngine starts an engine against server; setup runs before the engine does.
func newEngine(t *testing.T, server *httptest.Server, setup ...func(e *Engine)) (*Engine, context.CancelFunc) {
	APIBase = server.URL
	ctx, cancel := context.WithCancel(context.Background())
	e := New(ctx, Options{DataDir: t.TempDir(), Log: log.New(io.Discard, "", 0), Resolver: nil, DevProxyOnly: true})
	if err := e.refreshConfig(ctx); err != nil {
		t.Fatal(err)
	}
	for _, f := range setup {
		f(e)
	}
	done := make(chan struct{})
	go func() { e.Run(); close(done) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return e, cancel
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestImportConnectRebuildDisconnect(t *testing.T) {
	server := testServer(t)
	e, _ := newEngine(t, server)

	if _, err := e.SubmitKey("ключ: " + server.URL + "/abcDEF123456"); err != nil {
		t.Fatal(err)
	}
	info := e.subs.Info()
	if info == nil || len(info.Servers) != 2 || info.UpdateIntervalHours != 3 {
		t.Fatalf("%+v", info)
	}
	if got := e.settings.Get().SelectedServer; got != "🇳🇱 Нидерланды#1" {
		t.Fatalf("selected %q", got)
	}
	waitFor(t, "the account", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.account != nil && e.account.PlanName == "Тест"
	})
	// The same key again just refreshes.
	if r, err := e.SubmitKey(server.URL + "/abcDEF123456/json"); err != nil || r.Result != "refreshed" {
		t.Fatalf("%+v %v", r, err)
	}

	if err := e.Connect(); err != nil {
		t.Fatal(err)
	}
	if !e.connected() || !core.Running() {
		t.Fatal("not connected")
	}
	e.vpnLock.Lock()
	first := e.run
	e.vpnLock.Unlock()
	if len(first.applied.excluded) != 1 || first.applied.excluded[0] != "max.exe" {
		t.Fatalf("recommended exclusions from the API: %v", first.applied.excluded)
	}

	// A routing change rebuilds the tunnel by itself.
	_, _ = e.settings.Update(func(s *settings.Settings) { s.Routing.AddRules("block", []string{"domain:ads.example"}) })
	waitFor(t, "the rebuild", func() bool {
		e.vpnLock.Lock()
		defer e.vpnLock.Unlock()
		return e.run != nil && e.run != first && len(e.run.applied.routing.Block) == 1
	})

	// Switching servers keeps the session.
	e.mu.Lock()
	since := e.vpn.Since
	e.mu.Unlock()
	_, _ = e.settings.Update(func(s *settings.Settings) { s.SelectedServer = "🇩🇪 Германия#1" })
	e.Reconnect()
	e.mu.Lock()
	view := e.vpn
	e.mu.Unlock()
	if view.ServerKey != "🇩🇪 Германия#1" || view.Since != since || view.Status != StatusConnected {
		t.Fatalf("%+v (since %d)", view, since)
	}

	state := e.fullState()
	var vpn vpnView
	if err := json.Unmarshal(state["vpn"], &vpn); err != nil || vpn.Status != StatusConnected {
		t.Fatalf("%s %v", state["vpn"], err)
	}

	e.Disconnect()
	if e.connected() || core.Running() {
		t.Fatal("still connected")
	}
	if e.settings.Get().WasConnected {
		t.Fatal("a disconnect by the user is remembered")
	}
}

func TestForeignKeysAreRefused(t *testing.T) {
	server := testServer(t)
	e, _ := newEngine(t, server)
	for _, key := range []string{"https://evil.example/abcDEF123456", "просто текст", ""} {
		if _, err := e.SubmitKey(key); err == nil {
			t.Errorf("%q accepted", key)
		}
	}
	if err := e.Connect(); err == nil && e.connected() {
		t.Fatal("connected without a key")
	}
}

// The auto-select window is refreshed for the windows: when the last one goes away (the app quit
// with it open), the service stops.
func TestAutoCheckClosesWithTheLastWindow(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var windows *ipc.Server
	e, _ := newEngine(t, testServer(t), func(e *Engine) {
		windows = ipc.NewServer(listener, e)
		e.Attach(windows)
	})
	go windows.Serve(context.Background())
	defer windows.Close()
	dial := func() *ipc.Client {
		nc, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		return ipc.NewClient(nc, func(ipc.Event) {})
	}
	first, second := dial(), dial()
	waitFor(t, "two windows", func() bool { return windows.Count() == 2 })

	stopped := make(chan struct{})
	e.mu.Lock()
	e.autoCheck = &AutoCheck{Key: "auto", cancel: func() { close(stopped) }}
	e.mu.Unlock()

	_ = first.Close()
	waitFor(t, "one window", func() bool { return windows.Count() == 1 })
	select {
	case <-stopped:
		t.Fatal("closed while a window is still there")
	case <-time.After(200 * time.Millisecond):
	}
	_ = second.Close()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("still refreshed with no window")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.autoCheck != nil {
		t.Fatal("the window is still open")
	}
}
