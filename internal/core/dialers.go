package core

import (
	"context"
	"net/http"
	"reflect"
	"sync"
	"time"
	_ "unsafe" // go:linkname

	"github.com/xtls/xray-core/common"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/transport/internet"
	"github.com/xtls/xray-core/transport/internet/splithttp"
	"google.golang.org/grpc"
)

// Xray keeps the client connections of its gRPC and XHTTP transports in two tables that belong to
// the process, filed under the stream settings of the outbound that opened them, and never takes
// anything out: it is written for one instance per process. Here every ping is a throwaway
// instance and the VPN's own one is replaced at every change of network or settings, so the
// connections of closed instances stayed open (with their goroutines and buffers) and every entry
// kept its closed instance in memory, router and all: the service grew with every ping and every
// restart of the core. closeInstance takes an instance's entries out when it closes.
//
// The tables are not exported, so they are reached by name, and three unexported fields by
// reflection. An Xray that renames any of it would leave this doing nothing, silently:
// TestClosedInstanceLeavesNoConnections is there to say so. Run it after updating Xray.

// dialerKey is dialerConf of both transports.
type dialerKey struct {
	xnet.Destination
	*internet.MemoryStreamConfig
}

//go:linkname grpcDialers github.com/xtls/xray-core/transport/internet/grpc.globalDialerMap
var grpcDialers map[dialerKey]*grpc.ClientConn

//go:linkname grpcDialersLock github.com/xtls/xray-core/transport/internet/grpc.globalDialerAccess
var grpcDialersLock sync.Mutex

//go:linkname xhttpDialers github.com/xtls/xray-core/transport/internet/splithttp.globalDialerMap
var xhttpDialers map[dialerKey]*splithttp.XmuxManager

//go:linkname xhttpDialersLock github.com/xtls/xray-core/transport/internet/splithttp.globalDialerAccess
var xhttpDialersLock sync.Mutex

// The later sweeps take out what a request of the closed instance still on its way filed after
// the first one, and close the XHTTP connections that were not idle yet: a request takes a few
// seconds to end after its client is gone.
var dialerSweeps = []time.Duration{3 * time.Second, 30 * time.Second}

// closeInstance closes an instance and the connections Xray would keep for it.
func closeInstance(inst *core.Instance) error {
	settings := streamSettingsOf(inst)
	err := inst.Close()
	unwatchSilentServers(inst)
	if len(settings) == 0 {
		return err
	}
	var mu sync.Mutex
	var clients []*http.Client
	sweep := func() {
		mu.Lock()
		defer mu.Unlock()
		clients = append(clients, sweepDialers(settings)...)
		for _, c := range clients {
			c.CloseIdleConnections()
		}
	}
	sweep()
	for _, after := range dialerSweeps {
		time.AfterFunc(after, sweep)
	}
	return err
}

// streamSettingsOf lists the stream settings of the instance's outbounds.
func streamSettingsOf(inst *core.Instance) []*internet.MemoryStreamConfig {
	manager, ok := inst.GetFeature(outbound.ManagerType()).(outbound.Manager)
	if !ok || manager == nil {
		return nil
	}
	var list []*internet.MemoryStreamConfig
	for _, h := range manager.ListHandlers(context.Background()) {
		if s, ok := hiddenField[*internet.MemoryStreamConfig](h, "streamSettings"); ok && s != nil {
			list = append(list, s)
		}
	}
	return list
}

// hiddenField reads an unexported field of the struct ptr points to.
func hiddenField[T any](ptr any, name string) (T, bool) {
	var zero T
	v := reflect.ValueOf(ptr)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return zero, false
	}
	f := v.Elem().FieldByName(name)
	if !f.IsValid() || f.Type() != reflect.TypeFor[T]() {
		return zero, false
	}
	return *(*T)(f.Addr().UnsafePointer()), true
}

// sweepDialers forgets the connections filed under settings and closes the gRPC ones. Of XHTTP
// it returns the HTTP clients, whose connections close once they are idle.
func sweepDialers(settings []*internet.MemoryStreamConfig) []*http.Client {
	dead := make(map[*internet.MemoryStreamConfig]bool, len(settings))
	for _, s := range settings {
		dead[s] = true
	}

	var managers []*splithttp.XmuxManager
	xhttpDialersLock.Lock()
	// An XHTTP outbound with its own download settings files a second entry under them.
	for _, s := range settings {
		if s.DownloadSettings != nil {
			dead[s.DownloadSettings] = true
		}
	}
	for key, m := range xhttpDialers {
		if dead[key.MemoryStreamConfig] {
			managers = append(managers, m)
			delete(xhttpDialers, key)
		}
	}
	xhttpDialersLock.Unlock()

	var conns []*grpc.ClientConn
	grpcDialersLock.Lock()
	for key, c := range grpcDialers {
		if dead[key.MemoryStreamConfig] {
			conns = append(conns, c)
			delete(grpcDialers, key)
		}
	}
	grpcDialersLock.Unlock()

	for _, c := range conns {
		_ = c.Close()
	}
	var clients []*http.Client
	for _, m := range managers {
		muxes, _ := hiddenField[[]*splithttp.XmuxClient](m, "xmuxClients")
		for _, c := range muxes {
			if c == nil || c.XmuxConn == nil {
				continue
			}
			// Xray's own Close ends HTTP/3 only and leaves the rest to their idle timeout (5 minutes).
			_ = common.Close(c.XmuxConn)
			if client, ok := hiddenField[*http.Client](c.XmuxConn, "client"); ok && client != nil {
				clients = append(clients, client)
			}
		}
	}
	return clients
}
