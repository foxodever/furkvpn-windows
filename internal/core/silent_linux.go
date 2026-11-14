package core

import "golang.org/x/sys/unix"

// socketBytes tells how many bytes a TCP socket has received and sent.
func socketBytes(fd uintptr) (in, out uint64, known bool) {
	info, err := unix.GetsockoptTCPInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_INFO)
	if err != nil {
		return 0, 0, false
	}
	return info.Bytes_received, info.Bytes_sent, true
}

// dropSocket ends whatever waits on the socket; its owner closes it on the error.
func dropSocket(fd uintptr) {
	_ = unix.Shutdown(int(fd), unix.SHUT_RDWR)
}
