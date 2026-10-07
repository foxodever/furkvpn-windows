//go:build !windows

package netx

import (
	"context"
	"net"
	"sort"
	"strings"
	"time"
)

// Current is a stand-in for development on other systems: the first interface with a global
// address.
func Current() *State {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		var list []string
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.IsGlobalUnicast() {
				list = append(list, ipnet.IP.String())
			}
		}
		if len(list) > 0 {
			sort.Strings(list)
			return &State{Key: iface.Name + " " + strings.Join(list, ","), Kind: Ethernet, Index: iface.Index, Name: iface.Name}
		}
	}
	return nil
}

// Watch polls on other systems.
func Watch(ctx context.Context, changed func()) error {
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				changed()
			}
		}
	}()
	return nil
}

// BypassDialer is the plain dialer on other systems.
func BypassDialer(base net.Dialer) *net.Dialer { return &base }

// LANAddresses lists private IPv4 addresses.
func LANAddresses() []LANAddress {
	ifaces, _ := net.Interfaces()
	var out []LANAddress
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && ipnet.IP.IsPrivate() {
				out = append(out, LANAddress{IP: ipnet.IP.String(), Kind: Ethernet, Name: iface.Name})
			}
		}
	}
	return out
}
