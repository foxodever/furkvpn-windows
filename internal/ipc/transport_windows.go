//go:build windows

package ipc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// The service and the system have full access; signed-in users may read and write, but not
// create instances of the pipe (FILE_CREATE_PIPE_INSTANCE), so nobody can pose as the service
// while it runs.
const pipeSDDL = "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x12019b;;;IU)"

// Listen opens the service's pipe.
func Listen() (net.Listener, error) {
	return winio.ListenPipe(PipeName, &winio.PipeConfig{
		SecurityDescriptor: pipeSDDL,
		InputBufferSize:    64 * 1024,
		OutputBufferSize:   64 * 1024,
	})
}

// Dial connects to the service and checks that the other end really is the installed service:
// while it is stopped, any program could open a pipe with this name.
func Dial(ctx context.Context, servicePath string) (net.Conn, error) {
	timeout := 2 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}
	conn, err := winio.DialPipe(PipeName, &timeout)
	if err != nil {
		return nil, err
	}
	if servicePath != "" {
		if err := checkServer(conn, servicePath); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return conn, nil
}

type fdConn interface{ Fd() uintptr }

var procGetNamedPipeServerSessionId = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetNamedPipeServerSessionId")

// checkServer makes sure the pipe was created by the system: its owner is LocalSystem or the
// Administrators group, which an ordinary program can't make itself, and its server runs in
// session 0, where services live. (The service's process itself can't be looked into from a
// non-elevated program.)
func checkServer(conn net.Conn, _ string) error {
	f, ok := conn.(fdConn)
	if !ok {
		return errors.New("cannot check the pipe's server")
	}
	h := windows.Handle(f.Fd())
	var session uint32
	if r, _, err := procGetNamedPipeServerSessionId.Call(uintptr(h), uintptr(unsafe.Pointer(&session))); r == 0 {
		return fmt.Errorf("pipe server session: %w", err)
	}
	if session != 0 {
		return fmt.Errorf("the pipe is served from session %d, not by a service", session)
	}
	sd, err := windows.GetSecurityInfo(h, windows.SE_KERNEL_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("pipe owner: %w", err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("pipe owner: %w", err)
	}
	if !owner.IsWellKnown(windows.WinLocalSystemSid) && !owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
		return fmt.Errorf("the pipe belongs to %s, not to the system", owner.String())
	}
	return nil
}
