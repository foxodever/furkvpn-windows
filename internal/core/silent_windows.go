package core

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// SIO_TCP_INFO (Windows 10 1703 and later) fills TCP_INFO_v0.
const sioTCPInfo = 0xD8000027

type tcpInfoV0 struct {
	State             uint32
	Mss               uint32
	ConnectionTimeMs  uint64
	TimestampsEnabled bool
	RttUs             uint32
	MinRttUs          uint32
	BytesInFlight     uint32
	Cwnd              uint32
	SndWnd            uint32
	RcvWnd            uint32
	RcvBuf            uint32
	BytesOut          uint64
	BytesIn           uint64
	BytesReordered    uint32
	BytesRetrans      uint32
	FastRetrans       uint32
	DupAcksIn         uint32
	TimeoutEpisodes   uint32
	SynRetrans        uint8
}

// socketBytes tells how many bytes a TCP socket has received and sent.
func socketBytes(fd uintptr) (in, out uint64, known bool) {
	var version, returned uint32
	var info tcpInfoV0
	err := windows.WSAIoctl(windows.Handle(fd), sioTCPInfo, (*byte)(unsafe.Pointer(&version)), uint32(unsafe.Sizeof(version)),
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), &returned, nil, 0)
	if err != nil {
		return 0, 0, false
	}
	return info.BytesIn, info.BytesOut, true
}

// dropSocket ends whatever waits on the socket; its owner closes it on the error. A shutdown alone
// leaves a read that is already waiting where it is, so that is cancelled too.
func dropSocket(fd uintptr) {
	_ = windows.Shutdown(windows.Handle(fd), windows.SHUT_RDWR)
	_ = windows.CancelIoEx(windows.Handle(fd), nil)
}
