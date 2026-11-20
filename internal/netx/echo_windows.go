//go:build windows

package netx

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi            = windows.NewLazySystemDLL("iphlpapi.dll")
	procIcmpCreateFile  = iphlpapi.NewProc("IcmpCreateFile")
	procIcmpCloseHandle = iphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho2Ex = iphlpapi.NewProc("IcmpSendEcho2Ex")
)

// icmpEchoReply is ICMP_ECHO_REPLY of 64-bit Windows.
type icmpEchoReply struct {
	Address       uint32
	Status        uint32
	RoundTripTime uint32
	DataSize      uint16
	Reserved      uint16
	Data          uintptr
	TTL           uint8
	TOS           uint8
	Flags         uint8
	OptionsSize   uint8
	OptionsData   uintptr
}

// Echo pings ip the way the ping command does and returns the round trip in milliseconds. The
// request leaves from the real network's address, which keeps it out of the VPN while its routes
// take all traffic (Windows sends a packet through the interface its source address belongs to).
func Echo(ip net.IP, timeout time.Duration) (int64, error) {
	ip4 := ip.To4()
	if ip4 == nil {
		return -1, errors.New("ICMP ping needs an IPv4 address")
	}
	var source uint32
	if state := Current(); state != nil {
		for _, a := range state.addresses {
			if addr, err := netip.ParseAddr(a); err == nil && addr.Is4() {
				b := addr.As4()
				source = binary.LittleEndian.Uint32(b[:])
				break
			}
		}
	}
	handle, _, err := procIcmpCreateFile.Call()
	if windows.Handle(handle) == windows.InvalidHandle {
		return -1, err
	}
	defer procIcmpCloseHandle.Call(handle)
	request := []byte("furkvpn ping 0123456789abcdefghi")
	reply := make([]byte, int(unsafe.Sizeof(icmpEchoReply{}))+len(request)+8)
	start := time.Now()
	n, _, err := procIcmpSendEcho2Ex.Call(handle, 0, 0, 0,
		uintptr(source), uintptr(binary.LittleEndian.Uint32(ip4)),
		uintptr(unsafe.Pointer(&request[0])), uintptr(len(request)), 0,
		uintptr(unsafe.Pointer(&reply[0])), uintptr(len(reply)), uintptr(timeout.Milliseconds()))
	elapsed := time.Since(start)
	if n == 0 {
		return -1, fmt.Errorf("no echo: %w", err)
	}
	r := (*icmpEchoReply)(unsafe.Pointer(&reply[0]))
	if r.Status != 0 {
		return -1, fmt.Errorf("echo status %d", r.Status)
	}
	// Windows counts whole milliseconds; the clock tells a nearby server from 0.
	if ms := elapsed.Milliseconds(); r.RoundTripTime == 0 && ms <= 1 {
		return ms, nil
	}
	return int64(r.RoundTripTime), nil
}
