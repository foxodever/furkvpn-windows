//go:build windows

package netx

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

const (
	ifTypeEthernet = 6
	ifTypeWiFi     = 71
	ifTypeWWANPP   = 243
	ifTypeWWANPP2  = 244
	ipUnicastIf    = 31
	ipv6UnicastIf  = 31
)

// Current is the network the system sends traffic through when the VPN is off: the default route
// with the lowest metric, the VPN's adapter aside. nil when there is none.
func Current() *State {
	rows, err := winipcfg.GetIPForwardTable2(windows.AF_UNSPEC)
	if err != nil {
		return nil
	}
	type candidate struct {
		luid   winipcfg.LUID
		index  uint32
		metric uint32
		ifrow  *winipcfg.MibIfRow2
	}
	var best *candidate
	for i := range rows {
		r := &rows[i]
		if r.DestinationPrefix.PrefixLength != 0 {
			continue
		}
		ifrow, err := r.InterfaceLUID.Interface()
		if err != nil || ifrow.OperStatus != winipcfg.IfOperStatusUp || ifrow.Alias() == TunAlias {
			continue
		}
		family := winipcfg.AddressFamily(windows.AF_INET)
		if r.DestinationPrefix.Prefix().Addr().Is6() {
			family = windows.AF_INET6
		}
		ipif, err := r.InterfaceLUID.IPInterface(family)
		if err != nil {
			continue
		}
		metric := r.Metric + ipif.Metric
		if best == nil || metric < best.metric {
			best = &candidate{luid: r.InterfaceLUID, index: r.InterfaceIndex, metric: metric, ifrow: ifrow}
		}
	}
	if best == nil {
		return nil
	}
	return &State{
		Key:   fmt.Sprintf("%d %s", best.luid, strings.Join(addressesOf(best.luid), ",")),
		Kind:  kindOf(best.ifrow),
		Index: int(best.index),
		Name:  best.ifrow.Alias(),
	}
}

// addressesOf lists an interface's lasting addresses; rotating IPv6 privacy addresses and
// link-local ones don't break connections.
func addressesOf(luid winipcfg.LUID) []string {
	rows, err := winipcfg.GetUnicastIPAddressTable(windows.AF_UNSPEC)
	if err != nil {
		return nil
	}
	var out []string
	for i := range rows {
		r := &rows[i]
		if r.InterfaceLUID != luid || r.SuffixOrigin == winipcfg.SuffixOriginRandom {
			continue
		}
		addr := r.Address.Addr()
		if !addr.IsValid() || addr.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, addr.String())
	}
	slices.Sort(out)
	return out
}

func kindOf(row *winipcfg.MibIfRow2) Kind {
	switch row.Type {
	case ifTypeWiFi:
		return WiFi
	case ifTypeEthernet:
		return Ethernet
	case ifTypeWWANPP, ifTypeWWANPP2:
		return Mobile
	}
	return Other
}

// Watch calls changed (from a system thread; it must be quick) whenever routes, interfaces or
// addresses change, until ctx is done.
func Watch(ctx context.Context, changed func()) error {
	routes, err := winipcfg.RegisterRouteChangeCallback(func(winipcfg.MibNotificationType, *winipcfg.MibIPforwardRow2) { changed() })
	if err != nil {
		return err
	}
	ifaces, err := winipcfg.RegisterInterfaceChangeCallback(func(winipcfg.MibNotificationType, *winipcfg.MibIPInterfaceRow) { changed() })
	if err != nil {
		routes.Unregister()
		return err
	}
	addrs, err := winipcfg.RegisterUnicastAddressChangeCallback(func(winipcfg.MibNotificationType, *winipcfg.MibUnicastIPAddressRow) { changed() })
	if err != nil {
		routes.Unregister()
		ifaces.Unregister()
		return err
	}
	go func() {
		<-ctx.Done()
		routes.Unregister()
		ifaces.Unregister()
		addrs.Unregister()
	}()
	return nil
}

var errNoNetwork = errors.New("no network besides the VPN")

// BypassDialer makes connections through the real network even while the VPN's routes take all
// traffic: their sockets are bound to its interface, like Xray's own.
func BypassDialer(base net.Dialer) *net.Dialer {
	d := base
	d.Control = func(network, address string, c syscall.RawConn) error {
		if ap, err := netip.ParseAddrPort(address); err == nil && ap.Addr().IsLoopback() {
			return nil
		}
		// Without a real network the connection would follow the VPN's routes: better none.
		state := Current()
		if state == nil {
			return errNoNetwork
		}
		var sockErr error
		err := c.Control(func(fd uintptr) { sockErr = bindToInterface(network, windows.Handle(fd), state.Index) })
		if err != nil {
			return err
		}
		return sockErr
	}
	return &d
}

func bindToInterface(network string, fd windows.Handle, index int) error {
	switch network {
	case "tcp6", "udp6":
		if err := windows.SetsockoptInt(fd, windows.IPPROTO_IPV6, ipv6UnicastIf, index); err != nil {
			return err
		}
		// Dual-stack sockets may still send IPv4.
		_ = setIPv4Interface(fd, index)
		return nil
	default:
		return setIPv4Interface(fd, index)
	}
}

// IP_UNICAST_IF takes the index in network byte order.
func setIPv4Interface(fd windows.Handle, index int) error {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(index))
	return windows.SetsockoptInt(fd, windows.IPPROTO_IP, ipUnicastIf, *(*int)(unsafe.Pointer(&b[0])))
}

// LANAddresses are this computer's IPv4 addresses on Wi-Fi, Ethernet and its own hotspot, for
// devices that want to use the share proxy.
func LANAddresses() []LANAddress {
	adapters, err := winipcfg.GetAdaptersAddresses(windows.AF_INET, winipcfg.GAAFlagSkipAnycast|winipcfg.GAAFlagSkipMulticast|winipcfg.GAAFlagSkipDNSServer)
	if err != nil {
		return nil
	}
	var out []LANAddress
	for _, a := range adapters {
		if a.OperStatus != winipcfg.IfOperStatusUp || a.FriendlyName() == TunAlias {
			continue
		}
		var kind Kind
		switch a.IfType {
		case ifTypeWiFi:
			kind = WiFi
		case ifTypeEthernet:
			kind = Ethernet
		default:
			continue
		}
		desc := strings.ToLower(a.Description())
		for u := a.FirstUnicastAddress; u != nil; u = u.Next {
			addr, ok := netip.AddrFromSlice(u.Address.IP())
			if !ok {
				continue
			}
			addr = addr.Unmap()
			if !addr.Is4() || addr.IsLinkLocalUnicast() || addr.IsLoopback() {
				continue
			}
			k := kind
			// Windows' own Mobile hotspot runs on a virtual Wi-Fi Direct adapter, 192.168.137.1 by default.
			if strings.Contains(desc, "wi-fi direct") || strings.Contains(desc, "virtual") && kind == WiFi {
				k = Hotspot
			}
			out = append(out, LANAddress{IP: addr.String(), Kind: k, Name: a.FriendlyName()})
		}
	}
	return out
}
