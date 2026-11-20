//go:build !windows

package netx

import (
	"errors"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// Echo pings ip and returns the round trip in milliseconds (for development: the app is for
// Windows).
func Echo(ip net.IP, timeout time.Duration) (int64, error) {
	ip4 := ip.To4()
	if ip4 == nil {
		return -1, errors.New("ICMP ping needs an IPv4 address")
	}
	conn, err := icmp.ListenPacket("udp4", "0.0.0.0")
	if err != nil {
		return -1, err
	}
	defer conn.Close()
	message := icmp.Message{Type: ipv4.ICMPTypeEcho, Body: &icmp.Echo{ID: os.Getpid() & 0xffff, Seq: 1, Data: []byte("furkvpn ping")}}
	packet, err := message.Marshal(nil)
	if err != nil {
		return -1, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	start := time.Now()
	if _, err := conn.WriteTo(packet, &net.UDPAddr{IP: ip4}); err != nil {
		return -1, err
	}
	buf := make([]byte, 1500)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return -1, err
		}
		reply, err := icmp.ParseMessage(1, buf[:n])
		if err == nil && reply.Type == ipv4.ICMPTypeEchoReply && from.(*net.UDPAddr).IP.Equal(ip4) {
			return time.Since(start).Milliseconds(), nil
		}
	}
}
