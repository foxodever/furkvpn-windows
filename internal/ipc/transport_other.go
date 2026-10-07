//go:build !windows

package ipc

import (
	"context"
	"net"
	"os"
	"path/filepath"
)

// On other systems (development only) the service listens on a Unix socket.
func socketPath() string {
	if p := os.Getenv("FURKVPN_SOCKET"); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "furkvpn.sock")
}

// Listen opens the service's socket.
func Listen() (net.Listener, error) {
	_ = os.Remove(socketPath())
	l, err := net.Listen("unix", socketPath())
	if err == nil {
		_ = os.Chmod(socketPath(), 0o600)
	}
	return l, err
}

// Dial connects to the service.
func Dial(ctx context.Context, _ string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", socketPath())
}
