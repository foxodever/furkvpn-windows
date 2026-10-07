// Package core runs Xray-core inside the service process: the VPN instance (with its TUN inbound),
// throwaway instances that measure server delays, traffic counters and auto-select's checks.
// It is the Windows counterpart of the Android app's gomobile wrapper (core/libxray).
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/xtls/xray-core/app/observatory"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/platform"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/extension"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/infra/conf/serial"
	"github.com/xtls/xray-core/transport/internet"
	_ "github.com/xtls/xray-core/main/distro/all"
)

var (
	mu       sync.Mutex
	instance *core.Instance
)

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
	cfg, err := loadConfig(configJSON)
	if err != nil {
		return err
	}
	inst, err := core.New(cfg)
	if err != nil {
		return fmt.Errorf("create xray instance: %w", err)
	}
	if err := inst.Start(); err != nil {
		_ = inst.Close()
		forgetDialerControllers()
		return fmt.Errorf("start xray: %w", err)
	}
	instance = inst
	installLogHandler(inst)
	return nil
}

// Stop shuts the VPN instance down; the TUN adapter, its routes and filters go with it.
func Stop() error {
	mu.Lock()
	defer mu.Unlock()
	if instance == nil {
		return nil
	}
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

var proxyProtocols = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "shadowsocks": true,
	"hysteria": true, "wireguard": true, "socks": true, "http": true,
}

// MeasureDelay starts a throwaway instance whose only route is the first server of the config
// and returns the time in milliseconds needed to fetch testURL through it. While the VPN is on,
// its sockets are bound to the physical interface like the VPN's own, so it measures the server,
// not the tunnel.
func MeasureDelay(configJSON []byte, testURL string, timeout time.Duration) (int64, error) {
	var cfg map[string]any
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return -1, fmt.Errorf("parse config: %w", err)
	}
	indexes := proxyOutbounds(cfg)
	if len(indexes) == 0 {
		return -1, errors.New("config has no proxy outbound")
	}
	stripped, err := stripForOutbound(cfg, indexes[0])
	if err != nil {
		return -1, err
	}
	return measureStripped(stripped, testURL, timeout)
}

// OutboundDelay is one server of an auto-select config; Delay is -1 when it did not answer.
type OutboundDelay struct {
	Tag   string `json:"tag"`
	Delay int64  `json:"delay"`
}

// MeasureOutbounds measures every server of a config separately (the servers of an auto-select
// config).
func MeasureOutbounds(configJSON []byte, testURL string, timeout time.Duration) ([]OutboundDelay, error) {
	var cfg map[string]any
	if err := json.Unmarshal(configJSON, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	outbounds, _ := cfg["outbounds"].([]any)
	indexes := proxyOutbounds(cfg)
	results := make([]OutboundDelay, len(indexes))
	var wg sync.WaitGroup
	for n, idx := range indexes {
		ob, _ := outbounds[idx].(map[string]any)
		tag, _ := ob["tag"].(string)
		results[n] = OutboundDelay{Tag: tag, Delay: -1}
		wg.Add(1)
		go func(n, idx int) {
			defer wg.Done()
			stripped, err := stripForOutbound(cfg, idx)
			if err != nil {
				return
			}
			if ms, err := measureStripped(stripped, testURL, timeout); err == nil {
				results[n].Delay = ms
			}
		}(n, idx)
	}
	wg.Wait()
	return results, nil
}

func measureStripped(stripped []byte, testURL string, timeout time.Duration) (int64, error) {
	cfg, err := loadConfig(stripped)
	if err != nil {
		return -1, err
	}
	inst, err := core.New(cfg)
	// The throwaway instance has replaced the process-wide logger; give it back to the running one.
	restoreLogHandler()
	if err != nil {
		return -1, err
	}
	if err := inst.Start(); err != nil {
		_ = inst.Close()
		return -1, err
	}
	defer inst.Close()
	return httpDelay(inst, testURL, timeout)
}

// proxyOutbounds returns the indexes of the outbounds that lead to servers.
func proxyOutbounds(cfg map[string]any) []int {
	outbounds, _ := cfg["outbounds"].([]any)
	var indexes []int
	for i, o := range outbounds {
		ob, _ := o.(map[string]any)
		if p, _ := ob["protocol"].(string); proxyProtocols[p] {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

// stripForOutbound makes a config whose only route is the outbound at proxyIdx.
func stripForOutbound(cfg map[string]any, proxyIdx int) ([]byte, error) {
	outbounds, _ := cfg["outbounds"].([]any)
	// The first outbound is the default route; keep the others so dialerProxy chains still resolve.
	ordered := make([]any, 0, len(outbounds))
	ordered = append(ordered, outbounds[proxyIdx])
	ordered = append(ordered, outbounds[:proxyIdx]...)
	ordered = append(ordered, outbounds[proxyIdx+1:]...)
	out := map[string]any{
		"log":       map[string]any{"loglevel": "none"},
		"outbounds": ordered,
	}
	if dns, ok := cfg["dns"]; ok {
		out["dns"] = dns
	}
	return json.Marshal(out)
}

func httpDelay(inst *core.Instance, testURL string, timeout time.Duration) (int64, error) {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dest, err := xnet.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			return core.Dial(ctx, inst, dest)
		},
		DisableKeepAlives:   true,
		TLSHandshakeTimeout: timeout,
	}
	client := &http.Client{Transport: tr, Timeout: timeout}
	defer tr.CloseIdleConnections()

	start := time.Now()
	req, err := http.NewRequest(http.MethodGet, testURL, nil)
	if err != nil {
		return -1, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return -1, err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return -1, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return time.Since(start).Milliseconds(), nil
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
