// Package engine is the service's brain: it keeps the key, the subscription and the settings,
// runs the VPN with Xray-core, watches the network, white lists and auto-select, and tells the
// app's windows what is going on. It is the Windows counterpart of the Android app's VPN service,
// repositories and view model together.
package engine

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"furkvpn/internal/connlog"
	"furkvpn/internal/core"
	"furkvpn/internal/ipc"
	"furkvpn/internal/keys"
	"furkvpn/internal/netx"
	"furkvpn/internal/remote"
	"furkvpn/internal/settings"
	"furkvpn/internal/subs"
	"furkvpn/internal/sysinfo"
	"furkvpn/internal/version"
	"furkvpn/internal/whitelist"
)

// APIBase is furkapp.net, shared with the Android app.
var APIBase = "https://furkapp.net"

// testTLS lets tests trust their own HTTPS server; nil (the system's roots) otherwise.
var testTLS *tls.Config

const (
	configMaxAge      = 6 * time.Hour
	configStaleOnOpen = time.Hour
	accountMaxAge     = 10 * time.Minute
)

// Options of the engine.
type Options struct {
	// Where the service keeps its data (C:\ProgramData\FurkVPN).
	DataDir string
	Log     *log.Logger
	// Finds the program behind a connection for the connection log.
	Resolver connlog.Resolver
	// Turns the VPN into a local proxy for development on other systems (never on Windows).
	DevProxyOnly bool
}

// Engine runs everything.
type Engine struct {
	opt      Options
	log      *log.Logger
	settings *settings.Store
	subs     *subs.Repo
	connlog  *connlog.Log
	server   *ipc.Server
	ctx      context.Context

	mu              sync.Mutex
	config          remote.Config
	configFetchedAt time.Time
	account         *Account
	vpn             vpnView
	traffic         Traffic
	lan             []netx.LANAddress
	wl              whitelistView
	pings           map[string]Ping
	pinging         bool
	upd             updateView
	refreshing      bool

	vpnLock    sync.Mutex // start and stop one at a time
	run        *session   // the running VPN, guarded by vpnLock
	healthLock sync.Mutex // one auto-select check at a time
	best       autoBest   // guarded by mu
	applyTimer *time.Timer

	plain  *http.Client
	direct *http.Client

	dirty       chan struct{}
	pubMu       sync.Mutex
	published   map[string]string
	dirtySlices map[string]bool

	whitelistKick  chan time.Duration
	networkChanged chan struct{}
	checkedAt      time.Time // last white-list check, guarded by mu
	checkedNetwork string    // its network, guarded by mu
	probeOnce      sync.Once
	probes         *http.Client
}

// New opens the stored data; ctx is the service's lifetime.
func New(ctx context.Context, opt Options) *Engine {
	if opt.Log == nil {
		opt.Log = log.New(os.Stderr, "", log.LstdFlags)
	}
	e := &Engine{
		ctx:            ctx,
		opt:            opt,
		log:            opt.Log,
		connlog:        connlog.New(),
		pings:          map[string]Ping{},
		dirty:          make(chan struct{}, 1),
		published:      map[string]string{},
		dirtySlices:    map[string]bool{},
		whitelistKick:  make(chan time.Duration, 4),
		networkChanged: make(chan struct{}, 1),
	}
	_ = os.MkdirAll(opt.DataDir, 0o700)
	core.InitEnv(filepath.Join(opt.DataDir, "xray"))
	e.settings = settings.Open(filepath.Join(opt.DataDir, "settings.json"))
	e.config = e.loadConfig()
	e.account = e.loadAccount()
	e.plain = e.newClient(false)
	e.direct = e.newClient(true)
	e.subs = subs.Open(subs.Options{
		Dir:      opt.DataDir,
		Do:       e.do,
		Headers:  e.deviceHeaders,
		Allowed:  func() []string { return e.cfg().AllowedSubscriptionHosts },
		Mirrors:  func() []string { return e.cfg().SubscriptionMirrors },
		OnChange: func() { e.changed("subscription", "settings", "pings") },
	})
	e.settings.OnChange(func(old, new settings.Settings) {
		e.changed("settings", "exclusions", "routing", "subscription")
		e.settingsChanged(old, new)
	})
	e.wl = whitelistView{Status: whitelist.Unknown}
	e.vpn = vpnView{Status: StatusIdle}
	return e
}

// Attach connects the engine to the windows' server.
func (e *Engine) Attach(s *ipc.Server) { e.server = s }

// Run works until the service's context is done; then the VPN is stopped.
func (e *Engine) Run() {
	ctx := e.ctx
	go e.publisher(ctx)
	go e.watchNetwork(ctx)
	go e.whitelistLoop(ctx)
	go e.backgroundLoop(ctx)
	e.lanChanged()
	s := e.settings.Get()
	if s.AutoConnect && e.subs.Info() != nil {
		e.log.Print("connecting at start (auto-connect)")
		go e.Connect()
	} else if s.WasConnected && e.subs.Info() != nil && consumeMarker(e.opt.DataDir, reconnectMarker) {
		e.log.Print("connecting again after the update")
		go e.Connect()
	}
	<-ctx.Done()
	e.vpnLock.Lock()
	e.stopLocked(false)
	e.vpnLock.Unlock()
}

func (e *Engine) cfg() remote.Config {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.config
}

// backgroundLoop keeps the settings from furkapp.net and the subscription fresh: while the VPN is
// on or a window is open (on Windows the window usually lives in the tray), nothing otherwise.
func (e *Engine) backgroundLoop(ctx context.Context) {
	first := time.NewTimer(5 * time.Second)
	defer first.Stop()
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
		case <-ticker.C:
		}
		if !e.active() {
			continue
		}
		e.refreshConfigIfOlder(configMaxAge)
		e.refreshSubscriptionIfStale()
	}
}

// active: somebody can use what the background work finds.
func (e *Engine) active() bool {
	return e.connected() || (e.server != nil && e.server.Count() > 0)
}

// --- HTTP ---------------------------------------------------------------------------------

func userAgent() string {
	return "FurkVPN/" + version.Name + " Windows/" + sysinfo.OSVersion()
}

func (e *Engine) newClient(bypass bool) *http.Client {
	base := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	dialer := &base
	if bypass {
		dialer = netx.BypassDialer(base)
	}
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSClientConfig:       testTLS,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 25 * time.Second,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if allowed := subs.AllowedHosts(req.Context()); allowed != nil && !keys.HostAllowed(req.URL.Hostname(), allowed) {
				return subs.ErrForeign
			}
			return nil
		},
	}
}

// do sends a request through the VPN when it is on (the service's own connections follow the
// system routes into it), and directly when that fails or the VPN is off.
func (e *Engine) do(ctx context.Context, req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", userAgent())
	if e.connected() {
		resp, err := e.plain.Do(req.WithContext(ctx))
		if err == nil || ctx.Err() != nil || errors.Is(err, subs.ErrForeign) {
			return resp, err
		}
		retry := req.Clone(ctx)
		if req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			retry.Body = body
		}
		return e.direct.Do(retry)
	}
	return e.plain.Do(req.WithContext(ctx))
}

func (e *Engine) deviceHeaders() http.Header {
	return http.Header{
		"X-Hwid":         {e.deviceID()},
		"X-Device-Os":    {"Windows"},
		"X-Ver-Os":       {sysinfo.OSVersion()},
		"X-Device-Model": {sysinfo.DeviceModel()},
	}
}

// --- settings from furkapp.net --------------------------------------------------------------

func (e *Engine) configPath() string { return filepath.Join(e.opt.DataDir, "remote_config.json") }

// cachedConfig is remote_config.json: the API's answer as it came, and the version of the app that
// fetched it. A newer version may read fields the older one had no use for, so for it the cache
// is stale and the settings are fetched again at the first chance.
type cachedConfig struct {
	AppVersion string          `json:"appVersion"`
	Config     json.RawMessage `json:"config"`
}

func (e *Engine) loadConfig() remote.Config {
	data, err := os.ReadFile(e.configPath())
	if err != nil {
		return remote.Defaults()
	}
	var cached cachedConfig
	if json.Unmarshal(data, &cached) != nil || len(cached.Config) == 0 {
		// Before 1.2.0 the file was the decoded settings themselves.
		cached = cachedConfig{Config: data}
	}
	cfg, err := remote.Decode(cached.Config)
	if err != nil {
		return remote.Defaults()
	}
	if st, err := os.Stat(e.configPath()); err == nil && cached.AppVersion == version.Name {
		e.configFetchedAt = st.ModTime()
	}
	return cfg
}

func (e *Engine) refreshConfigIfOlder(age time.Duration) {
	e.mu.Lock()
	fresh := time.Since(e.configFetchedAt) < age
	e.mu.Unlock()
	if !fresh {
		_ = e.refreshConfig(e.ctx)
	}
}

func (e *Engine) refreshConfig(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, APIBase+"/api/v1/config", nil)
	if err != nil {
		return err
	}
	resp, err := e.do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	cfg, err := remote.Decode(data)
	if err != nil {
		return err
	}
	stored, _ := json.Marshal(cachedConfig{AppVersion: version.Name, Config: data})
	_ = settings.WriteAtomically(e.configPath(), stored)
	e.mu.Lock()
	old := e.config
	e.config = cfg
	e.configFetchedAt = time.Now()
	e.mu.Unlock()
	if !sameStrings(old.Windows.DefaultExcludedApps, cfg.Windows.DefaultExcludedApps) {
		e.applySoon()
	}
	e.updateAvailability()
	// The subscription's renewal link is on the site, whose domain comes with the config.
	e.changed("config", "subscription", "exclusions", "pings", "update")
	return nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- the subscription owner's plan ---------------------------------------------------------

// Account is the plan and expiry of the subscription owner, from furkapp.net.
type Account struct {
	Found        bool   `json:"found"`
	PlanID       *int   `json:"planId"`
	PlanName     string `json:"planName,omitempty"`
	ExpiresAt    *int64 `json:"expiresAt"`
	IsFree       bool   `json:"isFree"`
	Status       string `json:"status,omitempty"`
	TrafficUsed  *int64 `json:"trafficUsed"`
	TrafficLimit *int64 `json:"trafficLimit"`
	FetchedAt    int64  `json:"fetchedAt"`
}

func (e *Engine) accountPath() string { return filepath.Join(e.opt.DataDir, "account.json") }

func (e *Engine) loadAccount() *Account {
	data, err := os.ReadFile(e.accountPath())
	if err != nil {
		return nil
	}
	var a Account
	if json.Unmarshal(data, &a) != nil {
		return nil
	}
	return &a
}

func (e *Engine) refreshAccount(force bool) {
	info := e.subs.Info()
	if info == nil {
		return
	}
	e.mu.Lock()
	cached := e.account
	e.mu.Unlock()
	if !force && cached != nil && time.Since(time.UnixMilli(cached.FetchedAt)) < accountMaxAge {
		return
	}
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"url": info.URL})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, APIBase+"/api/v1/subscription/status", strings.NewReader(string(body)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(string(body))), nil }
	resp, err := e.do(ctx, req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var a Account
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&a) != nil {
		return
	}
	a.FetchedAt = time.Now().UnixMilli()
	stored, _ := json.Marshal(a)
	_ = settings.WriteAtomically(e.accountPath(), stored)
	e.mu.Lock()
	e.account = &a
	e.mu.Unlock()
	e.changed("account")
}

func (e *Engine) clearAccount() {
	_ = os.Remove(e.accountPath())
	e.mu.Lock()
	e.account = nil
	e.mu.Unlock()
	e.changed("account")
}

// --- device id ---------------------------------------------------------------------------------

// deviceID is sent as x-hwid, so the panel's device limit works when it is enabled: derived from
// Windows' own id of this installation, so it survives reinstalling the app.
func (e *Engine) deviceID() string {
	if id := e.settings.Get().DeviceID; id != "" {
		return id
	}
	id := hashedMachineID()
	_, _ = e.settings.Update(func(s *settings.Settings) {
		if s.DeviceID == "" {
			s.DeviceID = id
		}
	})
	return e.settings.Get().DeviceID
}
