// FurkVPNService.exe is the FurkVPN Windows service (LocalSystem): it runs Xray-core with its TUN
// adapter and everything that needs administrator rights, and serves the app's window over a named
// pipe. The installer registers it; "FurkVPNService.exe run" starts it in a console for debugging.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"furkvpn/internal/engine"
	"furkvpn/internal/ipc"
	"furkvpn/internal/remote"
)

// runEngine runs the engine and the windows' pipe until ctx is done.
func runEngine(ctx context.Context, dataDir string, logger *log.Logger, resolver resolverT, dev bool) error {
	log.SetOutput(libraryLog{logger.Writer()})
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("lib: ")
	// A test API instead of furkapp.net (set in the service's Environment value in the registry).
	if api := os.Getenv("FURKVPN_API"); api != "" {
		engine.APIBase = strings.TrimRight(api, "/")
		remote.AllowHTTPUpdates = true
		logger.Printf("API: %s", engine.APIBase)
	}
	if err := engine.SecureDirs(dataDir, filepath.Join(filepath.Dir(dataDir), "FurkVPN Updates")); err != nil {
		logger.Printf("folders: %v", err)
	}
	engine.ResetFirewall()
	e := engine.New(ctx, engine.Options{DataDir: dataDir, Log: logger, Resolver: resolver, DevProxyOnly: dev})
	listener, err := ipc.Listen()
	if err != nil {
		return fmt.Errorf("pipe: %w", err)
	}
	server := ipc.NewServer(listener, e)
	e.Attach(server)
	go func() {
		<-ctx.Done()
		server.Close()
	}()
	go func() {
		if err := server.Serve(ctx); err != nil {
			logger.Printf("pipe: %v", err)
		}
	}()
	setResumeHook(e.Resumed)
	e.Run()
	return nil
}

// libraryLog puts into the service log what libraries write to Go's default logger, which a
// service has nowhere to show. Wintun's messages are there: why an adapter did not come up, as in
// "Failed to setup adapter (problem code: 0x27, ntstatus: 0xC0000034)". Left out is its report, at
// every start and worded as a failure, that no adapter was left from before.
type libraryLog struct{ to io.Writer }

func (l libraryLog) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("Failed to find matching adapter name")) {
		return len(p), nil
	}
	_, err := l.to.Write(p)
	return len(p), err
}

// rotatingLog keeps the service log small: one file of up to 1 MB plus the previous one.
type rotatingLog struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

func openLog(path string) io.Writer {
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	l := &rotatingLog{path: path}
	l.open()
	return l
}

func (l *rotatingLog) open() {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	l.f = f
	if st, err := f.Stat(); err == nil {
		l.size = st.Size()
	}
}

func (l *rotatingLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return len(p), nil
	}
	if l.size+int64(len(p)) > 1<<20 {
		l.f.Close()
		_ = os.Rename(l.path, l.path+".1")
		l.size = 0
		l.open()
		if l.f == nil {
			return len(p), nil
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}
