package engine

import (
	"cmp"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"furkvpn/internal/core"
	"furkvpn/internal/ipc"
	"furkvpn/internal/netx"
	"furkvpn/internal/settings"
	"furkvpn/internal/xrayconf"
)

const (
	networkSettle    = 3 * time.Second
	autoCheckEvery   = 10 * time.Second
	healthRestartMin = 2 * time.Minute
	longSleep        = 5 * time.Minute
	autoBestMaxAge   = 10 * time.Minute
	applyDebounce    = 1500 * time.Millisecond
	connectLogLevel  = "error"
)

// session is the running VPN.
type session struct {
	serverKey string
	raw       []byte
	applied   applied
	cancel    context.CancelFunc
	network   string // guarded by the engine's vpnLock

	mu sync.Mutex
	// Auto-select servers that were carrying traffic at the last look.
	autoInUse         map[string]bool
	lastHealthRestart time.Time
	lastSample        time.Time
}

// applied is what the running tunnel was built with, to notice when it has to be rebuilt.
type applied struct {
	excluded []string
	// Only vpnApps go through the VPN; excluded is empty then.
	onlyApps bool
	vpnApps  []string
	share    *settings.Share
	routing  settings.Routing
	tun      settings.Tun
}

func (a applied) equal(b applied) bool {
	shareEqual := (a.share == nil) == (b.share == nil) && (a.share == nil || *a.share == *b.share)
	return shareEqual && a.tun == b.tun && slices.Equal(a.excluded, b.excluded) &&
		a.onlyApps == b.onlyApps && slices.Equal(a.vpnApps, b.vpnApps) && a.routing.Enabled == b.routing.Enabled &&
		a.routing.LANDirect == b.routing.LANDirect && a.routing.RUDirect == b.routing.RUDirect &&
		slices.Equal(a.routing.Direct, b.routing.Direct) && slices.Equal(a.routing.Proxy, b.routing.Proxy) &&
		slices.Equal(a.routing.Block, b.routing.Block)
}

// Replaced in tests.
var (
	hasIPv6   = netx.HasIPv6
	startCore = core.Start
)

// autoBest is the server auto-select found best a moment ago, to start the next core with it as
// the fallback.
type autoBest struct {
	serverKey, tag string
	at             time.Time
}

func (e *Engine) connected() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.vpn.Status == StatusConnected
}

func (e *Engine) wanted() applied {
	s := e.settings.Get()
	var share *settings.Share
	if s.Share.Enabled {
		sh := s.Share
		share = &sh
	}
	want := applied{share: share, routing: s.Routing, tun: e.coreTun(s.Tun)}
	if x := settings.ExclusionsOf(s, e.cfg().Windows.DefaultExcludedApps); x.OnlySelected {
		want.onlyApps, want.vpnApps = true, x.Selected
	} else {
		want.excluded = x.Effective()
		slices.Sort(want.excluded)
	}
	return want
}

// coreTun is what of Settings → Расширенные настройки the core is built with: the adapter's
// parameters with the adapter, the proxy's port without it. The system proxy is the window's.
func (e *Engine) coreTun(t settings.Tun) settings.Tun {
	if e.opt.DevProxyOnly {
		// Elsewhere than on Windows the TUN would take over the whole machine's routes.
		t.Enabled = false
	}
	t.SystemProxy = false
	if t.Enabled {
		t.ProxyPort = 0
		// A Windows with IPv6 turned off has none for the adapter either, and the core, told to
		// set it up, gives up after 15 seconds of trying ("Element not found").
		if t.IPv6 && (e.noTunIPv6.Load() || !hasIPv6()) {
			t.IPv6 = false
		}
	} else {
		t.MTU, t.IPv6, t.StrictDNS = 0, false, false
	}
	return t
}

// Connect starts the VPN with the selected server.
func (e *Engine) Connect() error {
	e.vpnLock.Lock()
	defer e.vpnLock.Unlock()
	if e.run != nil {
		return nil
	}
	return e.startLocked(false)
}

// Disconnect stops the VPN.
func (e *Engine) Disconnect() {
	e.vpnLock.Lock()
	defer e.vpnLock.Unlock()
	e.stopLocked(true)
}

// Reconnect restarts the core with the currently selected server, keeping the session.
func (e *Engine) Reconnect() {
	e.vpnLock.Lock()
	defer e.vpnLock.Unlock()
	if e.run != nil {
		_ = e.startLocked(true)
	}
}

// startLocked builds the config and starts the core; vpnLock must be held.
func (e *Engine) startLocked(reconnect bool) error {
	prev := e.run
	e.mu.Lock()
	since := e.vpn.Since
	if !reconnect {
		e.vpn = vpnView{Status: StatusConnecting}
	}
	e.mu.Unlock()
	e.changed("vpn")

	fail := func(err error) error {
		e.log.Printf("VPN start failed: %v", err)
		e.message("vpn_error", errText(err))
		e.stopLocked(false)
		return err
	}

	s := e.settings.Get()
	server := e.subs.Selected(s.SelectedServer)
	if server == nil {
		return fail(ipc.Errorf("no_servers", "the subscription has no servers"))
	}
	raw := e.subs.ConfigFor(server.Key)
	if raw == nil {
		return fail(ipc.Errorf("no_servers", "no config for the selected server"))
	}
	want := e.wanted()

	// Stop the old core first: it holds the adapter and the share proxy ports. Keep what it has counted.
	if prev != nil {
		prev.cancelWatchers()
		e.sampleTraffic(prev, false)
		_ = core.Stop()
	}

	var tun *xrayconf.Tun
	localPort := 0
	if want.tun.Enabled {
		tun = &xrayconf.Tun{MTU: want.tun.MTU, IPv6: want.tun.IPv6, StrictDNS: want.tun.StrictDNS}
	} else {
		port, ok := freePort(want.tun.ProxyPort)
		if !ok {
			return fail(ipc.Errorf("port_busy", "ports "+strconv.Itoa(want.tun.ProxyPort)+"+ are busy"))
		}
		localPort = port
	}
	var share *xrayconf.ShareProxy
	if want.share != nil {
		share = sharePorts(*want.share, localPort)
	}
	fallback := ""
	e.mu.Lock()
	if b := e.best; b.serverKey == server.Key && time.Since(b.at) < autoBestMaxAge {
		fallback = b.tag
	}
	e.mu.Unlock()
	r := want.routing
	build := func() ([]byte, error) {
		return xrayconf.Build(raw, xrayconf.Options{
			LogLevel:  connectLogLevel,
			Tun:       tun,
			LocalPort: localPort,
			Share:     share,
			Routing:   &xrayconf.Routing{Enabled: r.Enabled, LANDirect: r.LANDirect, RUDirect: r.RUDirect, Direct: r.Direct, Proxy: r.Proxy, Block: r.Block},
			Excluded:  want.excluded,
			OnlyApps:  want.onlyApps,
			VPNApps:   want.vpnApps,
			Fallback:  fallback,
			ErrorLog:  e.xrayLog(),
		})
	}
	config, err := build()
	if err != nil {
		return fail(ipc.Errorf("bad_config", err.Error()))
	}
	if !reconnect {
		e.connlog.Clear()
	}
	e.connlog.SetOutbounds(xrayconf.OutboundProtocols(config))
	e.applyConnectionLog(s.ConnectionLog)
	err = startCore(config)
	if err != nil && tun != nil && tun.IPv6 && core.NoInterface(err) {
		// Other adapters have IPv6 and this one got none: whatever keeps it from the adapter, the
		// VPN works without. Remembered, as finding it out takes the core its 15 seconds.
		e.log.Printf("the adapter did not come up with IPv6 (%v), starting without it", err)
		e.noTunIPv6.Store(true)
		want.tun.IPv6, tun.IPv6 = false, false
		if config, err = build(); err == nil {
			err = startCore(config)
		}
		if err != nil {
			e.noTunIPv6.Store(false) // not IPv6's fault then
		}
	}
	if err != nil {
		return fail(ipc.Errorf("core", err.Error()))
	}
	if share != nil {
		openShareFirewall(share.HTTPPort, share.SocksPort)
	} else {
		closeShareFirewall()
	}

	ctx, cancel := context.WithCancel(e.ctx)
	run := &session{serverKey: server.Key, raw: raw, applied: want, cancel: cancel, autoInUse: map[string]bool{}, lastSample: time.Now()}
	if st := netx.Current(); st != nil {
		run.network = st.Key
	}
	if prev != nil {
		prev.mu.Lock()
		run.lastHealthRestart = prev.lastHealthRestart
		prev.mu.Unlock()
	}
	e.run = run
	e.mu.Lock()
	if since == 0 || !reconnect {
		since = time.Now().UnixMilli()
	}
	e.vpn = vpnView{Status: StatusConnected, Since: since, ServerKey: server.Key, LocalPort: localPort}
	if share != nil {
		e.vpn.Share = &shareView{HTTPPort: share.HTTPPort, SocksPort: share.SocksPort, Auth: share.User != ""}
	}
	if !reconnect {
		e.traffic = Traffic{}
	}
	e.mu.Unlock()
	e.changed("vpn", "traffic", "pings")
	if !reconnect {
		_, _ = e.settings.Update(func(s *settings.Settings) { s.WasConnected = true })
		e.kickWhitelist(time.Second)
	}
	go e.statsLoop(ctx, run)
	go e.watchdog(ctx, run)
	if tun != nil {
		e.log.Printf("VPN on: %s", server.DisplayName())
	} else {
		e.log.Printf("VPN on: %s, proxy on 127.0.0.1:%d", server.DisplayName(), localPort)
	}
	return nil
}

// xrayLog is the file for Xray's own errors, started afresh when it grows past 1 MB.
func (e *Engine) xrayLog() string {
	if e.opt.DevProxyOnly {
		return ""
	}
	path := filepath.Join(e.opt.DataDir, "logs", "xray.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 1<<20 {
		_ = os.Rename(path, path+".1")
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	return path
}

func (s *session) cancelWatchers() {
	if s.cancel != nil {
		s.cancel()
	}
}

// stopLocked stops the core; vpnLock must be held. byUser forgets that the VPN was on, so it is
// not turned back on at the next start.
func (e *Engine) stopLocked(byUser bool) {
	run := e.run
	e.mu.Lock()
	wasIdle := e.vpn.Status == StatusIdle && run == nil
	if !wasIdle {
		e.vpn = vpnView{Status: StatusStopping}
	}
	e.mu.Unlock()
	if !wasIdle {
		e.changed("vpn")
	}
	if run != nil {
		run.cancelWatchers()
	}
	_ = core.Stop()
	core.SetConnectionListener(nil)
	closeShareFirewall()
	e.run = nil
	e.mu.Lock()
	e.vpn = vpnView{Status: StatusIdle}
	e.traffic = Traffic{}
	e.mu.Unlock()
	e.changed("vpn", "traffic", "pings")
	if byUser {
		_, _ = e.settings.Update(func(s *settings.Settings) { s.WasConnected = false })
	}
	if run != nil {
		e.log.Print("VPN off")
	}
}

func errText(err error) string {
	var ie *ipc.Error
	if errors.As(err, &ie) && ie.Message != "" {
		return ie.Message
	}
	return err.Error()
}

// sharePorts are the ports from the settings (8080 and 1080 by default), or the next free ones.
func sharePorts(s settings.Share, taken int) *xrayconf.ShareProxy {
	httpPort, ok := freePort(s.HTTPPort, taken)
	if !ok {
		return nil
	}
	socksPort, ok := freePort(s.SocksPort, taken, httpPort)
	if !ok {
		return nil
	}
	p := &xrayconf.ShareProxy{HTTPPort: httpPort, SocksPort: socksPort}
	if s.Auth && s.User != "" && s.Password != "" {
		p.User, p.Password = s.User, s.Password
	}
	return p
}

// freePort is the preferred port or one of the next ones that nothing listens on.
func freePort(preferred int, taken ...int) (int, bool) {
	for port := preferred; port < min(preferred+20, settings.MaxPort+1); port++ {
		if slices.Contains(taken, port) {
			continue
		}
		l, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(port))
		if err != nil {
			continue
		}
		_ = l.Close()
		return port, true
	}
	return 0, false
}

// --- watching the running VPN --------------------------------------------------------------

func (e *Engine) statsLoop(ctx context.Context, run *session) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.sampleTraffic(run, true)
		}
	}
}

// sampleTraffic adds what the core has counted since the last call to the session totals (a server
// switch keeps the session).
func (e *Engine) sampleTraffic(run *session, withSpeed bool) {
	run.mu.Lock()
	defer run.mu.Unlock()
	now := time.Now()
	seconds := max(now.Sub(run.lastSample).Seconds(), 0.001)
	run.lastSample = now
	var up, down int64
	for _, c := range xrayconf.Counters("uplink") {
		up += core.Counter(c, true)
	}
	for _, c := range xrayconf.Counters("downlink") {
		down += core.Counter(c, true)
	}
	e.mu.Lock()
	t := e.traffic
	t.TotalUp += up
	t.TotalDown += down
	t.Up, t.Down = 0, 0
	if withSpeed {
		t.Up = int64(float64(up) / seconds)
		t.Down = int64(float64(down) / seconds)
	}
	e.traffic = t
	e.mu.Unlock()
	e.changed("traffic")
}

// watchdog: auto-select switches only new connections, and only after its next check (once a
// minute). When a server it was using stops answering (checked twice), or none answers at all, the
// core restarts so programs reconnect through a live server instead of hanging on the dead one.
func (e *Engine) watchdog(ctx context.Context, run *session) {
	t := time.NewTicker(autoCheckEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.checkAutoSelect(run, false)
		}
	}
}

func (e *Engine) checkAutoSelect(run *session, afterSleep bool) {
	e.healthLock.Lock()
	defer e.healthLock.Unlock()
	if afterSleep {
		core.RecheckServers()
	}
	checks := core.Observatory()
	if len(checks) == 0 {
		return
	}
	lost := lostServers(run, checks)
	if !afterSleep && (len(lost) > 0 || noneAlive(checks)) {
		// One failed check can be a hiccup: look again before dropping connections.
		core.RecheckServers()
		checks = core.Observatory()
		if len(checks) == 0 {
			return
		}
		still := lost[:0]
		for _, tag := range lost {
			for _, c := range checks {
				if c.Tag == tag && !c.Alive {
					still = append(still, tag)
				}
			}
		}
		lost = still
	}
	var alive []core.ServerCheck
	for _, c := range checks {
		if c.Alive {
			alive = append(alive, c)
		}
	}
	slices.SortFunc(alive, func(a, b core.ServerCheck) int { return cmp.Compare(a.Delay, b.Delay) })
	if len(alive) > 0 {
		e.mu.Lock()
		e.best = autoBest{serverKey: run.serverKey, tag: alive[0].Tag, at: time.Now()}
		e.mu.Unlock()
	}
	inUse := map[string]bool{}
	for i := 0; i < len(alive) && i < 2; i++ {
		inUse[alive[i].Tag] = true
	}
	run.mu.Lock()
	run.autoInUse = inUse
	run.mu.Unlock()
	if len(lost) == 0 && len(alive) > 0 {
		return
	}
	if netx.Current() == nil {
		return // without a network a restart can't help
	}
	run.mu.Lock()
	recent := time.Since(run.lastHealthRestart) < healthRestartMin
	if !recent {
		run.lastHealthRestart = time.Now()
	}
	run.mu.Unlock()
	if recent {
		return
	}
	e.log.Printf("auto-select: servers %v stopped answering (alive: %d), restarting the core", lost, len(alive))
	go func() {
		e.vpnLock.Lock()
		defer e.vpnLock.Unlock()
		if e.run == run {
			_ = e.startLocked(true)
		}
	}()
}

func lostServers(run *session, checks []core.ServerCheck) []string {
	run.mu.Lock()
	defer run.mu.Unlock()
	var lost []string
	for tag := range run.autoInUse {
		for _, c := range checks {
			if c.Tag == tag && !c.Alive {
				lost = append(lost, tag)
			}
		}
	}
	return lost
}

func noneAlive(checks []core.ServerCheck) bool {
	for _, c := range checks {
		if c.Alive {
			return false
		}
	}
	return true
}

// Resumed is called when the computer wakes up: after a long sleep the connections to the
// servers may be dead, so auto-select's servers are checked at once.
func (e *Engine) Resumed(slept time.Duration) {
	e.kickWhitelist(3 * time.Second)
	if slept < longSleep {
		return
	}
	e.vpnLock.Lock()
	run := e.run
	e.vpnLock.Unlock()
	if run != nil {
		go e.checkAutoSelect(run, true)
	}
}

// watchNetwork restarts the core when the real network changes (Wi-Fi ↔ Ethernet, a new address):
// connections to the servers made on the old network are dead but would hang for minutes, and
// auto-select's checks with them. It also re-checks white lists and the addresses for sharing.
func (e *Engine) watchNetwork(ctx context.Context) {
	if err := netx.Watch(ctx, func() {
		select {
		case e.networkChanged <- struct{}{}:
		default:
		}
	}); err != nil {
		e.log.Printf("network watch: %v", err)
		return
	}
	var timer *time.Timer
	settle := make(chan struct{}, 1)
	// The network seen at each change: a drop and a return to the same network (Wi-Fi coming back
	// with the same address) also leaves dead connections behind.
	lastSeen := ""
	if st := netx.Current(); st != nil {
		lastSeen = st.Key
	}
	bounced := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.networkChanged:
			key := ""
			if st := netx.Current(); st != nil {
				key = st.Key
			}
			if key != lastSeen {
				lastSeen = key
				bounced = true
			}
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(networkSettle, func() {
				select {
				case settle <- struct{}{}:
				default:
				}
			})
		case <-settle:
			e.lanChanged()
			state := netx.Current()
			e.networkSettled(state)
			changed := bounced
			bounced = false
			if state == nil {
				continue
			}
			lastSeen = state.Key
			e.vpnLock.Lock()
			run := e.run
			if run != nil && (changed || run.network != state.Key) {
				e.log.Printf("network changed (%s), restarting the core", state.Name)
				_ = e.startLocked(true)
			}
			e.vpnLock.Unlock()
		}
	}
}

func (e *Engine) lanChanged() {
	lan := netx.LANAddresses()
	e.mu.Lock()
	e.lan = lan
	e.mu.Unlock()
	e.changed("lan")
}

// --- settings that change the running tunnel ---------------------------------------------

// applySoon rebuilds the tunnel when the programs without VPN, sharing or routing changed;
// changes come in bursts (typing, switching several things), so it waits for them to settle.
func (e *Engine) applySoon() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.applyTimer != nil {
		e.applyTimer.Stop()
	}
	e.applyTimer = time.AfterFunc(applyDebounce, func() {
		want := e.wanted()
		e.vpnLock.Lock()
		defer e.vpnLock.Unlock()
		if e.run != nil && !e.run.applied.equal(want) {
			e.log.Print("settings changed, rebuilding the tunnel")
			_ = e.startLocked(true)
		}
	})
}

func (e *Engine) settingsChanged(old, new settings.Settings) {
	if old.ConnectionLog != new.ConnectionLog {
		e.applyConnectionLog(new.ConnectionLog)
		if !new.ConnectionLog {
			e.connlog.Clear()
		}
	}
	if old.WhitelistDetect != new.WhitelistDetect && new.WhitelistDetect {
		e.kickWhitelist(0)
	}
	e.applySoon()
}

func (e *Engine) applyConnectionLog(on bool) {
	if on {
		core.SetConnectionListener(e.connlog.Listener(e.opt.Resolver))
	} else {
		core.SetConnectionListener(nil)
	}
}

// reconnectIfConfigChanged rebuilds the tunnel when a refreshed subscription changed the selected
// server's config.
func (e *Engine) reconnectIfConfigChanged() {
	e.vpnLock.Lock()
	defer e.vpnLock.Unlock()
	if e.run == nil {
		return
	}
	server := e.subs.Selected(e.settings.Get().SelectedServer)
	if server == nil {
		return
	}
	raw := e.subs.ConfigFor(server.Key)
	if raw != nil && (server.Key != e.run.serverKey || string(raw) != string(e.run.raw)) {
		e.log.Print("the server's config changed, reconnecting")
		_ = e.startLocked(true)
	}
}
