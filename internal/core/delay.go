package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"

	"furkvpn/internal/jsonx"
)

// A delay is the time to connect to the server and fetch the test URL through it. Looking up the
// server's address is not part of it: the domain names of the servers are resolved first and the
// test instance connects to the addresses directly. While the VPN is on, the test instance's
// sockets are bound to the physical interface like the VPN's own, so it measures the server, not
// the tunnel.

// MeasureDelay returns the delay in milliseconds of a subscription config through a throwaway
// instance. A config with one server is measured through that server; an auto-select config
// (balancers) through its own routing, the way the VPN would send the traffic. The test URL is
// the config's own (its observatory's) when it has one, otherwise testURL.
func MeasureDelay(configJSON []byte, testURL string, timeout time.Duration) (int64, error) {
	cfg, err := jsonx.ParseObject(configJSON)
	if err != nil {
		return -1, fmt.Errorf("parse config: %w", err)
	}
	testURL = probeURL(cfg, testURL)
	indexes := proxyOutbounds(cfg)
	if len(indexes) == 0 {
		return -1, errors.New("config has no proxy outbound")
	}
	if hasBalancers(cfg) {
		// A server that cannot be resolved just fails its checks, as it would in the VPN.
		pinServers(cfg, indexes)
		return measureAuto(cfg, testURL, timeout)
	}
	indexes = indexes[:1]
	if err := pinServers(cfg, indexes); err != nil {
		return -1, err
	}
	results, err := measureTags(delayTestConfig(cfg, false), tagOutbounds(cfg, indexes), testURL, timeout)
	if err != nil {
		return -1, err
	}
	return results[0].delay, results[0].err
}

// measureAuto fetches testURL through the routing of an auto-select config. Before its first
// checks the balancer sends traffic to its fallback server; if that one does not answer, the
// second try goes where the checks made by then point.
func measureAuto(cfg jsonx.Object, testURL string, timeout time.Duration) (int64, error) {
	inst, err := startTestInstance(delayTestConfig(cfg, true))
	if err != nil {
		return -1, err
	}
	defer inst.Close()
	ms, err := httpDelay(inst, "", testURL, timeout)
	if err != nil {
		ms, err = httpDelay(inst, "", testURL, timeout)
	}
	return ms, err
}

var proxyProtocols = map[string]bool{
	"vless": true, "vmess": true, "trojan": true, "shadowsocks": true,
	"hysteria": true, "wireguard": true, "socks": true, "http": true,
}

// proxyOutbounds returns the indexes of the outbounds that lead to servers.
func proxyOutbounds(cfg jsonx.Object) []int {
	var indexes []int
	for i, o := range jsonx.Arr(cfg["outbounds"]) {
		if proxyProtocols[jsonx.S(jsonx.Obj(o), "protocol")] {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

// tagOutbounds returns the tags of the outbounds at indexes, naming the untagged ones.
func tagOutbounds(cfg jsonx.Object, indexes []int) []string {
	outbounds := jsonx.Arr(cfg["outbounds"])
	tags := make([]string, len(indexes))
	for n, idx := range indexes {
		ob := jsonx.Obj(outbounds[idx])
		tag := jsonx.S(ob, "tag")
		if tag == "" {
			tag = fmt.Sprintf("furk-test-%d", idx)
			ob["tag"] = tag
		}
		tags[n] = tag
	}
	return tags
}

func hasBalancers(cfg jsonx.Object) bool {
	return len(jsonx.Arr(jsonx.Obj(cfg["routing"])["balancers"])) > 0
}

// probeURL is the URL the config's own server checks use, or fallback when it has none.
func probeURL(cfg jsonx.Object, fallback string) string {
	var candidates []any
	if ping := jsonx.Obj(jsonx.Obj(cfg["burstObservatory"])["pingConfig"]); ping != nil {
		candidates = append(candidates, ping["destination"])
	}
	if obs := jsonx.Obj(cfg["observatory"]); obs != nil {
		candidates = append(candidates, obs["probeURL"], obs["probeUrl"])
	}
	for _, c := range candidates {
		s, _ := c.(string)
		if u, err := url.Parse(strings.TrimSpace(s)); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			return u.String()
		}
	}
	return fallback
}

// delayTestConfig is what a delay test runs: the servers (with pinned addresses) and, for
// auto-select, its routing, balancers and checks. No DNS: the servers' names are already resolved,
// and routing matches the test URL by its name only.
func delayTestConfig(cfg jsonx.Object, withBalancers bool) jsonx.Object {
	out := jsonx.Object{"log": jsonx.Object{"loglevel": "none"}}
	for _, key := range []string{"outbounds", "transport"} {
		if v, ok := cfg[key]; ok {
			out[key] = v
		}
	}
	if !withBalancers {
		return out
	}
	for _, key := range []string{"observatory", "burstObservatory"} {
		if v, ok := cfg[key]; ok {
			out[key] = v
		}
	}
	if routing := jsonx.Obj(cfg["routing"]); routing != nil {
		kept := jsonx.Object{}
		for k, v := range routing {
			kept[k] = v
		}
		kept["domainStrategy"] = "AsIs"
		out["routing"] = kept
	}
	return out
}

func startTestInstance(cfg jsonx.Object) (*core.Instance, error) {
	b, err := jsonx.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	config, err := loadConfig(b)
	if err != nil {
		return nil, err
	}
	createMu.Lock()
	inst, err := core.New(config)
	restoreRunningLocked()
	createMu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := inst.Start(); err != nil {
		_ = inst.Close()
		return nil, err
	}
	return inst, nil
}

type tagDelay struct {
	delay int64
	err   error
}

// measureTags fetches testURL through each of the outbounds named by tags, all at once in one
// instance.
func measureTags(cfg jsonx.Object, tags []string, testURL string, timeout time.Duration) ([]tagDelay, error) {
	inst, err := startTestInstance(cfg)
	if err != nil {
		return nil, err
	}
	defer inst.Close()
	results := make([]tagDelay, len(tags))
	var wg sync.WaitGroup
	for n, tag := range tags {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ms, err := httpDelay(inst, tag, testURL, timeout)
			results[n] = tagDelay{ms, err}
		}()
	}
	wg.Wait()
	return results, nil
}

// httpDelay fetches testURL through inst: through the outbound tag, or by the instance's routing
// when tag is empty.
func httpDelay(inst *core.Instance, tag string, testURL string, timeout time.Duration) (int64, error) {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dest, err := xnet.ParseDestination(network + ":" + addr)
			if err != nil {
				return nil, err
			}
			if tag != "" {
				ctx = session.SetForcedOutboundTagToContext(ctx, tag)
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
