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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"furkvpn/internal/core"
	"furkvpn/internal/settings"
	"furkvpn/internal/version"
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

// waitForAccount waits out the account request an import starts: it reads APIBase, which the next
// test's engine sets.
func waitForAccount(t *testing.T, e *Engine) {
	t.Helper()
	waitFor(t, "the account", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.account != nil
	})
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
	var sub struct{ RenewURL string }
	if err := json.Unmarshal(state["subscription"], &sub); err != nil || sub.RenewURL != "https://furkvpn.info/renewal?sub=abcDEF123456" {
		t.Fatalf("renewal link %s %v", state["subscription"], err)
	}
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

// Programs → «Только выбранные через VPN» from the window: the mode and the list reach the window's
// state and rebuild the running tunnel; the programs without VPN stay for the usual mode.
func TestOnlyChosenProgramsFromTheWindow(t *testing.T) {
	server := testServer(t)
	e, _ := newEngine(t, server)
	if _, err := e.SubmitKey(server.URL + "/abcDEF123456"); err != nil {
		t.Fatal(err)
	}
	waitForAccount(t, e)
	if err := e.Connect(); err != nil {
		t.Fatal(err)
	}
	defer e.Disconnect()
	call := func(method, params string) error {
		_, err := e.Handle(context.Background(), nil, method, json.RawMessage(params))
		return err
	}
	if err := call("setAppExcluded", `{"name": "bank.exe", "excluded": true}`); err != nil {
		t.Fatal(err)
	}
	if err := call("set", `{"key": "onlySelectedApps", "value": true}`); err != nil {
		t.Fatal(err)
	}
	if err := call("setAppSelected", `{"name": "C:\\Apps\\Telegram.exe", "selected": true}`); err != nil {
		t.Fatal(err)
	}
	if err := call("setAppSelected", `{"name": "bad|name", "selected": true}`); err == nil {
		t.Fatal("a name that can't be a program's")
	}
	waitFor(t, "the rebuild", func() bool {
		e.vpnLock.Lock()
		defer e.vpnLock.Unlock()
		a := e.run.applied
		return a.onlyApps && len(a.excluded) == 0 && slices.Equal(a.vpnApps, []string{"Telegram.exe"})
	})
	var x struct {
		OnlySelected bool
		Selected     []string
		Effective    []string
	}
	if err := json.Unmarshal(e.fullState()["exclusions"], &x); err != nil {
		t.Fatal(err)
	}
	if !x.OnlySelected || !slices.Equal(x.Selected, []string{"Telegram.exe"}) || !slices.Contains(x.Effective, "bank.exe") {
		t.Fatalf("%+v", x)
	}

	if err := call("set", `{"key": "onlySelectedApps", "value": false}`); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the usual mode", func() bool {
		e.vpnLock.Lock()
		defer e.vpnLock.Unlock()
		a := e.run.applied
		return !a.onlyApps && len(a.vpnApps) == 0 && slices.Contains(a.excluded, "bank.exe")
	})
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

// Without the TUN (always so off Windows) the VPN is a proxy on 127.0.0.1: its port follows the
// settings, and what doesn't change the core (the window's system proxy, the adapter's MTU) leaves
// the tunnel alone.
func TestProxyModeFollowsItsSettings(t *testing.T) {
	server := testServer(t)
	e, _ := newEngine(t, server)
	if _, err := e.SubmitKey(server.URL + "/abcDEF123456"); err != nil {
		t.Fatal(err)
	}
	if err := e.Connect(); err != nil {
		t.Fatal(err)
	}
	defer e.Disconnect()
	e.vpnLock.Lock()
	first := e.run
	e.vpnLock.Unlock()
	e.mu.Lock()
	port := e.vpn.LocalPort
	e.mu.Unlock()
	if port < settings.DefaultProxyPort || port >= settings.DefaultProxyPort+20 {
		t.Fatalf("local port %d", port)
	}

	_, _ = e.settings.Update(func(s *settings.Settings) { s.Tun.SystemProxy = false; s.Tun.MTU = 1400 })
	time.Sleep(applyDebounce + 500*time.Millisecond)
	e.vpnLock.Lock()
	same := e.run == first
	e.vpnLock.Unlock()
	if !same {
		t.Fatal("rebuilt for nothing")
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	free := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	_, _ = e.settings.Update(func(s *settings.Settings) { s.Tun.ProxyPort = free })
	waitFor(t, "the new port", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.vpn.LocalPort == free
	})
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", free))
	if err != nil {
		t.Fatalf("the proxy doesn't listen: %v", err)
	}
	_ = conn.Close()
}

// The cached settings from furkapp.net are fresh only for the app version that fetched them: an
// updated app fetches them again, for the fields the old one did not keep.
func TestCachedConfigOfAnotherVersionIsStale(t *testing.T) {
	cases := map[string]bool{
		`{"telegramProxies":[]}`: false, // before 1.2.0: the decoded settings themselves
		`{"appVersion":"` + version.Name + `","config":{"telegramProxies":[{"title":"A","url":"tg://proxy?server=a"}]}}`: true,
		`{"appVersion":"0.0.1","config":{}}`: false,
	}
	for cache, fresh := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "remote_config.json"), []byte(cache), 0o600); err != nil {
			t.Fatal(err)
		}
		e := New(context.Background(), Options{DataDir: dir, Log: log.New(io.Discard, "", 0), DevProxyOnly: true})
		if got := !e.configFetchedAt.IsZero(); got != fresh {
			t.Errorf("%s: fresh %v, want %v", cache, got, fresh)
		}
		if fresh && len(e.cfg().TelegramProxies) != 1 {
			t.Errorf("%s: %+v", cache, e.cfg().TelegramProxies)
		}
	}
}

func TestHeaderSafe(t *testing.T) {
	if got := headerSafe(" Huawei 华为 MateBook™ X\t"); got != "Huawei  MateBook X" {
		t.Fatalf("%q", got)
	}
}

func TestSendKeyToTV(t *testing.T) {
	var sent map[string]string
	server := testServer(t)
	server.Config.Handler.(*http.ServeMux).HandleFunc("/api/v1/tv/send", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		if sent["code"] != "ABC123" {
			http.Error(w, `{"error":"not_found"}`, 404)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	})
	e, _ := newEngine(t, server)
	if r, err := e.SubmitKey("https://furkapp.net/tv/ABC123"); err == nil || !strings.Contains(err.Error(), "tv_need_key") {
		t.Fatalf("without a key: %+v %v", r, err)
	}
	if _, err := e.SubmitKey(server.URL + "/abcDEF123456"); err != nil {
		t.Fatal(err)
	}
	if r, err := e.SubmitKey("furkvpn://tv/abc-123"); err != nil || r.Result != "tv" || r.Code != "ABC123" {
		t.Fatalf("%+v %v", r, err)
	}
	if err := e.SendTVKey("abc 123"); err != nil || !strings.HasSuffix(sent["key"], "/abcDEF123456") {
		t.Fatalf("%v %v", err, sent)
	}
	if err := e.SendTVKey("ZZZ999"); err == nil || !strings.Contains(err.Error(), "tv_no_code") {
		t.Fatalf("expired code: %v", err)
	}
	if err := e.SendTVKey("AB"); err == nil || !strings.Contains(err.Error(), "tv_bad_code") {
		t.Fatalf("bad code: %v", err)
	}
}

// The settings hadn't come yet, or came before the key's domain was added to them: the key fetches
// them again instead of being called foreign, but not more often than once a minute.
func TestKeyOnANewDomainFetchesTheSettingsAgain(t *testing.T) {
	var mu sync.Mutex
	hosts, fetches := `["*.furkvpn.com"]`, 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		fetches++
		fmt.Fprintf(w, `{"allowedSubscriptionHosts":%s}`, hosts)
	})
	mux.HandleFunc("/abcDEF123456/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "["+vlessConfig("🇳🇱 Нидерланды", "192.0.2.1")+"]")
	})
	mux.HandleFunc("/api/v1/subscription/status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"found":true,"planName":"Тест","expiresAt":2000000000,"isFree":false}`)
	})
	server := httptest.NewTLSServer(mux)
	testTLS = server.Client().Transport.(*http.Transport).TLSClientConfig
	t.Cleanup(server.Close)
	e, _ := newEngine(t, server)
	key := server.URL + "/abcDEF123456"

	if _, err := e.SubmitKey(key); err == nil || !strings.Contains(err.Error(), "foreign_key") {
		t.Fatalf("not on the list: %v", err)
	}
	mu.Lock()
	hosts = `["*.furkvpn.com","127.0.0.1"]`
	asked := fetches
	mu.Unlock()
	if asked != 1 {
		t.Fatalf("the settings that just came were fetched again (%d)", asked)
	}
	e.mu.Lock()
	e.configFetchedAt = time.Now().Add(-2 * configRecheck)
	e.mu.Unlock()
	if r, err := e.SubmitKey(key); err != nil || r.Result != "imported" {
		t.Fatalf("%+v %v", r, err)
	}
	waitForAccount(t, e)
}

// A Windows with IPv6 turned off has none for the VPN's adapter, and the core does not start when
// told to set it up: the adapter then goes without IPv6. Where Windows has IPv6 and the adapter
// still comes up without, the core is started again without it, and that is remembered.
func TestAdapterGoesWithoutIPv6WhereWindowsHasNone(t *testing.T) {
	oldHas, oldStart := hasIPv6, startCore
	t.Cleanup(func() { hasIPv6, startCore = oldHas, oldStart })

	var mu sync.Mutex
	system, adapter := false, false // IPv6 in Windows, and for the adapter
	var starts []bool               // each start of the core: with IPv6?
	hasIPv6 = func() bool {
		mu.Lock()
		defer mu.Unlock()
		return system
	}
	startCore = func(config []byte) error {
		mu.Lock()
		defer mu.Unlock()
		with := strings.Contains(string(config), `"fdfe:dcba:9876::1/126"`)
		starts = append(starts, with)
		if with && !adapter {
			return fmt.Errorf("start xray: %w", fmt.Errorf("failed to start proxy > %w", syscall.Errno(1168)))
		}
		return nil
	}
	started := func() []bool {
		mu.Lock()
		defer mu.Unlock()
		out := starts
		starts = nil
		return out
	}

	server := testServer(t)
	e, _ := newEngine(t, server, func(e *Engine) { e.opt.DevProxyOnly = false })
	if _, err := e.SubmitKey(server.URL + "/abcDEF123456"); err != nil {
		t.Fatal(err)
	}
	waitForAccount(t, e)
	applied := func() bool {
		e.vpnLock.Lock()
		defer e.vpnLock.Unlock()
		return e.run.applied.tun.IPv6
	}

	// Windows has no IPv6: the core is not even asked for it.
	if err := e.Connect(); err != nil {
		t.Fatal(err)
	}
	if got := started(); len(got) != 1 || got[0] || applied() {
		t.Fatalf("starts with IPv6: %v", got)
	}
	e.Disconnect()

	// Windows has it and the adapter gets none: the second start is without, and so are the next ones.
	mu.Lock()
	system = true
	mu.Unlock()
	if err := e.Connect(); err != nil {
		t.Fatal(err)
	}
	if got := started(); len(got) != 2 || !got[0] || got[1] || applied() {
		t.Fatalf("starts with IPv6: %v", got)
	}
	if e.wanted().tun.IPv6 {
		t.Fatal("the next rebuild would try IPv6 again")
	}
	e.Reconnect()
	if got := started(); len(got) != 1 || got[0] {
		t.Fatalf("starts with IPv6 after a restart: %v", got)
	}
	e.Disconnect()

	// Both there: IPv6 goes into the adapter as the settings say.
	e.noTunIPv6.Store(false)
	mu.Lock()
	adapter = true
	mu.Unlock()
	if err := e.Connect(); err != nil {
		t.Fatal(err)
	}
	if got := started(); len(got) != 1 || !got[0] || !applied() {
		t.Fatalf("starts with IPv6: %v", got)
	}
	e.Disconnect()
}

// A start that fails without IPv6 as well has another reason: IPv6 is tried again next time.
func TestAdapterFailureIsNotBlamedOnIPv6(t *testing.T) {
	oldHas, oldStart := hasIPv6, startCore
	t.Cleanup(func() { hasIPv6, startCore = oldHas, oldStart })
	hasIPv6 = func() bool { return true }
	var starts atomic.Int32
	startCore = func([]byte) error {
		starts.Add(1)
		return fmt.Errorf("start xray: %w", syscall.Errno(1168))
	}

	server := testServer(t)
	e, _ := newEngine(t, server, func(e *Engine) { e.opt.DevProxyOnly = false })
	if _, err := e.SubmitKey(server.URL + "/abcDEF123456"); err != nil {
		t.Fatal(err)
	}
	waitForAccount(t, e)
	if err := e.Connect(); err == nil || e.connected() {
		t.Fatalf("connected: %v", err)
	}
	if starts.Load() != 2 || e.noTunIPv6.Load() || !e.wanted().tun.IPv6 {
		t.Fatalf("%d starts, without IPv6 from now on: %v", starts.Load(), e.noTunIPv6.Load())
	}
}

// A fresh install that can't reach furkapp.net (another VPN on) shows the built-in settings, whose
// lists must reach the window as arrays (a null froze its main screen); the key, once it is
// added, brings the real settings.
func TestKeyWithoutTheSettingsFetchesThem(t *testing.T) {
	var apiDown atomic.Bool
	apiDown.Store(true)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/config", func(w http.ResponseWriter, r *http.Request) {
		if apiDown.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, `{"allowedSubscriptionHosts":["127.0.0.1"],"telegramProxies":[{"title":"Польша","url":"https://t.me/proxy?server=1.2.3.4&port=443&secret=dd00"}]}`)
	})
	mux.HandleFunc("/abcDEF123456/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "["+vlessConfig("🇳🇱 Нидерланды", "192.0.2.1")+"]")
	})
	mux.HandleFunc("/api/v1/subscription/status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"found":true,"planName":"Тест","expiresAt":2000000000,"isFree":false}`)
	})
	server := httptest.NewTLSServer(mux)
	testTLS = server.Client().Transport.(*http.Transport).TLSClientConfig
	t.Cleanup(server.Close)
	APIBase = server.URL
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e := New(ctx, Options{DataDir: t.TempDir(), Log: log.New(io.Discard, "", 0), DevProxyOnly: true})

	if e.refreshConfig(ctx) == nil {
		t.Fatal("the API is down")
	}
	raw, _ := json.Marshal(e.fullState()["config"])
	if strings.Contains(strings.ReplaceAll(string(raw), `"update":null`, ""), "null") {
		t.Fatalf("a null in the built-in settings: %s", raw)
	}
	// The key's domain is known to the built-in settings (here: the test server's).
	e.mu.Lock()
	e.config.AllowedSubscriptionHosts = []string{"127.0.0.1"}
	e.mu.Unlock()
	apiDown.Store(false)
	if r, err := e.SubmitKey(server.URL + "/abcDEF123456"); err != nil || r.Result != "imported" {
		t.Fatalf("%+v %v", r, err)
	}
	waitFor(t, "the settings", func() bool { return len(e.cfg().TelegramProxies) == 1 })
	waitForAccount(t, e)
}

// White lists can be watched without the app changing servers: with the switching turned off the
// chosen server stays, and the ping type chosen in the settings reaches the window.
func TestWhitelistWithoutSwitchingAndPingType(t *testing.T) {
	server := testServer(t)
	e, _ := newEngine(t, server)
	if _, err := e.SubmitKey(server.URL + "/abcDEF123456"); err != nil {
		t.Fatal(err)
	}
	waitForAccount(t, e)
	e.mu.Lock()
	e.config.Whitelist.LTEServer = []string{"Германия"}
	e.mu.Unlock()
	set := func(key string, value any) error {
		params, _ := json.Marshal(map[string]any{"key": key, "value": value})
		_, err := e.Handle(context.Background(), nil, "set", params)
		return err
	}
	if s := e.settings.Get(); !s.WhitelistAutoSwitch || s.PingType != settings.PingViaProxy {
		t.Fatalf("defaults: %+v", s)
	}

	if err := set("whitelistAutoSwitch", false); err != nil {
		t.Fatal(err)
	}
	e.autoSwitch("all_ok", "whitelist")
	if got := e.settings.Get().SelectedServer; got != "🇳🇱 Нидерланды#1" {
		t.Fatalf("switched to %q with the switching off", got)
	}
	if !e.settings.Get().WhitelistDetect {
		t.Fatal("the white lists are still watched")
	}
	if err := set("whitelistAutoSwitch", true); err != nil {
		t.Fatal(err)
	}
	e.autoSwitch("all_ok", "whitelist")
	if got := e.settings.Get().SelectedServer; got != "🇩🇪 Германия#1" {
		t.Fatalf("selected %q with the switching on", got)
	}

	if err := set("pingType", "tcp"); err != nil {
		t.Fatal(err)
	}
	if set("pingType", "udp") == nil {
		t.Fatal("an unknown ping type is refused")
	}
	var view struct {
		PingType            string
		WhitelistAutoSwitch bool
	}
	if err := json.Unmarshal(e.fullState()["settings"], &view); err != nil || view.PingType != "tcp" || !view.WhitelistAutoSwitch {
		t.Fatalf("%s %v", e.fullState()["settings"], err)
	}
}
