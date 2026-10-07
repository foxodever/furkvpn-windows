//go:build !windows

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"furkvpn/internal/connlog"
)

type resolverT = connlog.Resolver

type devResolver struct{}

func (devResolver) Owner(network, srcIP string, srcPort int, dstIP string, dstPort int) connlog.Owner {
	return connlog.Owner{Key: "unknown", Kind: "unknown"}
}

func setResumeHook(func(time.Duration)) {}

// On other systems the service runs in the foreground for development, with a local SOCKS proxy
// on 127.0.0.1:10808 instead of a TUN adapter.
func main() {
	dir := os.Getenv("FURKVPN_DATA")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "furkvpn-data")
	}
	logger := log.New(os.Stderr, "", log.LstdFlags)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	logger.Printf("development service, data in %s", dir)
	if err := runEngine(ctx, dir, logger, devResolver{}, true); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
