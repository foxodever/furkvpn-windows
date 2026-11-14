//go:build !linux && !windows

package core

func socketBytes(fd uintptr) (in, out uint64, known bool) { return 0, 0, false }

func dropSocket(fd uintptr) {}
