// Package core runs Xray-core inside the service process: the VPN instance (with its TUN inbound),
// throwaway instances that measure server delays (delay.go), traffic counters and auto-select's
// checks.
// It is the Windows counterpart of the Android app's gomobile wrapper (core/libxray).
package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/xtls/xray-core/app/observatory"
	"github.com/xtls/xray-core/common/platform"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/dns"
	"github.com/xtls/xray-core/features/extension"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/main/distro/all"
	"github.com/xtls/xray-core/transport/internet"
)

var (
	mu       sync.Mutex
	instance *core.Instance
)

// Xray keeps some state per process rather than per instance: the log handler, and the DNS client
// and outbound manager its dialer uses (servers with a domain name are resolved through that DNS
// client). Every core.New takes them over, so a throwaway delay-test instance would leave the
// running VPN resolving its servers through a closed instance: lookups time out after 4 seconds
// and auto-select, which gives a server 3 seconds, sees its servers as down. createMu serializes
// core.New with putting the state back; running is the instance it belongs to.
var (
	createMu sync.Mutex
	running  atomic.Pointer[core.Instance]
)

// restoreRunningLocked gives the process-wide state back to the running instance. Hold createMu.
func restoreRunningLocked() {
	inst := running.Load()
	if inst == nil {
		return
	}
	installLogHandler(inst)
	if dnsClient, ok := inst.GetFeature(dns.ClientType()).(dns.Client); ok {
		obm, _ := inst.GetFeature(outbound.ManagerType()).(outbound.Manager)
		internet.InitSystemDialer(dnsClient, obm)
	}
}

// Version of the embedded Xray-core.
func Version() string { return core.Version() }

// InitEnv sets the directory where Xray looks for geoip.dat / geosite.dat and other assets.
func InitEnv(assetDir string) {
	_ = os.Setenv(platform.AssetLocation, assetDir)
}

// Start launches the VPN instance with a complete config.
func Start(configJSON []byte) error {
	mu.Lock()
	defer mu.Unlock()
	if instance != nil {
		return errors.New("xray is already running")
	}
	cfg, err := loadConfig(withServersResolvedDirectly(configJSON))
	if err != nil {
		return err
	}
	createMu.Lock()
	inst, err := core.New(cfg)
	createMu.Unlock()
	if err != nil {
		return fmt.Errorf("create xray instance: %w", err)
	}
	if err := inst.Start(); err != nil {
		_ = inst.Close()
		forgetDialerControllers()
		return fmt.Errorf("start xray: %w", err)
	}
	instance = inst
	createMu.Lock()
	running.Store(inst)
	// A delay test may have created its instance after this one and taken over the process-wide state.
	restoreRunningLocked()
	createMu.Unlock()
	return nil
}

// Stop shuts the VPN instance down; the TUN adapter, its routes and filters go with it.
func Stop() error {
	mu.Lock()
	defer mu.Unlock()
	if instance == nil {
		return nil
	}
	running.Store(nil)
	err := instance.Close()
	instance = nil
	forgetDialerControllers()
	return err
}

// forgetDialerControllers drops what the TUN inbound registered to bind Xray's sockets to the
// physical interface. Xray never takes it back, and it would keep binding the delay tests, and the
// next instance's sockets, to whatever interface was current when the VPN stopped.
func forgetDialerControllers() {
	internet.ControllersLock.Lock()
	internet.Controllers = nil
	internet.ControllersLock.Unlock()
}

// Running reports whether the VPN instance is up.
func Running() bool {
	mu.Lock()
	defer mu.Unlock()
	return instance != nil
}

func current() *core.Instance {
	mu.Lock()
	defer mu.Unlock()
	return instance
}

// Counter returns a traffic counter of the running instance, e.g.
// "inbound>>>tun-in>>>traffic>>>downlink"; 0 when it does not exist.
func Counter(name string, reset bool) int64 {
	inst := current()
	if inst == nil {
		return 0
	}
	sm, ok := inst.GetFeature(stats.ManagerType()).(stats.Manager)
	if !ok || sm == nil {
		return 0
	}
	c := sm.GetCounter(name)
	if c == nil {
		return 0
	}
	if reset {
		return c.Set(0)
	}
	return c.Value()
}

func loadConfig(configJSON []byte) (*core.Config, error) {
	cfg, err := serial.LoadJSONConfig(strings.NewReader(string(configJSON)))
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// Validate parses and builds a config without starting anything.
func Validate(configJSON []byte) error {
	_, err := loadConfig(configJSON)
	return err
}

// ServerCheck is one server of auto-select as its last check saw it; Delay is in milliseconds.
type ServerCheck struct {
	Tag   string `json:"tag"`
	Alive bool   `json:"alive"`
	Delay int64  `json:"delay"`
	All   int64  `json:"all"`
	Fail  int64  `json:"fail"`
}

// Observatory returns what the running auto-select knows about its servers; nil when the config
// has no observatory.
func Observatory() []ServerCheck {
	inst := current()
	if inst == nil {
		return nil
	}
	obs, ok := inst.GetFeature(extension.ObservatoryType()).(extension.Observatory)
	if !ok || obs == nil {
		return nil
	}
	msg, err := obs.GetObservation(context.Background())
	if err != nil {
		return nil
	}
	result, ok := msg.(*observatory.ObservationResult)
	if !ok {
		return nil
	}
	checks := make([]ServerCheck, 0, len(result.Status))
	for _, s := range result.Status {
		c := ServerCheck{Tag: s.OutboundTag, Alive: s.Alive, Delay: s.Delay}
		if s.HealthPing != nil {
			c.All, c.Fail = s.HealthPing.All, s.HealthPing.Fail
		}
		checks = append(checks, c)
	}
	return checks
}

// RecheckServers makes auto-select check all its servers right now instead of waiting for the
// next round. It returns when the checks are done.
func RecheckServers() {
	inst := current()
	if inst == nil {
		return
	}
	feature := inst.GetFeature(extension.ObservatoryType())
	checker, ok := feature.(interface{ Check([]string) })
	obs, ok2 := feature.(extension.Observatory)
	if !ok || !ok2 {
		return
	}
	msg, err := obs.GetObservation(context.Background())
	if err != nil {
		return
	}
	result, ok := msg.(*observatory.ObservationResult)
	if !ok || len(result.Status) == 0 {
		return
	}
	tags := make([]string, 0, len(result.Status))
	for _, s := range result.Status {
		tags = append(tags, s.OutboundTag)
	}
	checker.Check(tags)
}
