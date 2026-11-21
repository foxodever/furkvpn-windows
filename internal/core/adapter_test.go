package core

import (
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"

	xerrors "github.com/xtls/xray-core/common/errors"
)

// The adapter's own failure is a bare Windows error right under the inbound manager's "failed to
// start proxy". Setting the adapter up, a busy port and a missing file are other matters, and a
// new driver is no answer to them.
func TestAdapterFailed(t *testing.T) {
	started := func(inner error) error {
		return fmt.Errorf("start xray: %w", xerrors.New("failed to start proxy").Base(inner))
	}
	for name, c := range map[string]struct {
		err  error
		want bool
	}{
		"the adapter did not come up": {started(syscall.Errno(2)), true},
		"no adapter at all":           {started(syscall.Errno(4319)), true},
		"routes":                      {started(xerrors.New("unable to set routes").Base(syscall.Errno(1168))), false},
		"a busy port":                 {fmt.Errorf("start xray: %w", &net.OpError{Op: "listen", Err: os.NewSyscallError("bind", syscall.Errno(98))}), false},
		"a missing file":              {fmt.Errorf("create xray instance: %w", &os.PathError{Op: "open", Path: "geoip.dat", Err: syscall.Errno(2)}), false},
		"no Windows error":            {started(xerrors.New("iface not found")), false},
		"no error":                    {nil, false},
	} {
		if got := AdapterFailed(c.err); got != c.want {
			t.Errorf("%s (%v): %v", name, c.err, got)
		}
	}
}
