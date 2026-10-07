//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"furkvpn/internal/connlog"
)

const serviceName = "FurkVPN"

type resolverT = connlog.Resolver

func dataDir() string {
	if dir, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0); err == nil {
		return filepath.Join(dir, "FurkVPN")
	}
	return filepath.Join(os.Getenv("ProgramData"), "FurkVPN")
}

func main() {
	if len(os.Args) > 1 {
		var err error
		switch os.Args[1] {
		case "install":
			err = install()
		case "uninstall":
			err = uninstall()
		case "run":
			err = runConsole()
		default:
			err = fmt.Errorf("unknown command %q (install, uninstall, run)", os.Args[1])
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		fmt.Fprintln(os.Stderr, "FurkVPNService.exe is started by Windows; use \"run\" to run it in a console")
		os.Exit(2)
	}
	if err := svc.Run(serviceName, &handler{}); err != nil {
		os.Exit(1)
	}
}

func runConsole() error {
	logger := log.New(io.MultiWriter(os.Stderr, openLog(filepath.Join(dataDir(), "logs", "service.log"))), "", log.LstdFlags)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		var b [1]byte
		_, _ = os.Stdin.Read(b[:])
		cancel()
	}()
	logger.Print("running in the console; press Enter to stop")
	return runEngine(ctx, dataDir(), logger, connlog.WindowsResolver{}, false)
}

type handler struct{}

var resume struct {
	sync.Mutex
	hook      func(time.Duration)
	suspended time.Time
}

func setResumeHook(f func(time.Duration)) {
	resume.Lock()
	resume.hook = f
	resume.Unlock()
}

// Power broadcast events (pbt.h).
const (
	pbtAPMSuspend         = 4
	pbtAPMResumeSuspend   = 7
	pbtAPMResumeAutomatic = 18
)

func (h *handler) Execute(args []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPowerEvent
	status <- svc.Status{State: svc.StartPending}
	logger := log.New(openLog(filepath.Join(dataDir(), "logs", "service.log")), "", log.LstdFlags)
	logger.Print("service starting")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runEngine(ctx, dataDir(), logger, connlog.WindowsResolver{}, false) }()
	status <- svc.Status{State: svc.Running, Accepts: accepts}
	for {
		select {
		case err := <-done:
			if err != nil {
				logger.Printf("service failed: %v", err)
				status <- svc.Status{State: svc.StopPending}
				return false, 1
			}
			return false, 0
		case c := <-requests:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				logger.Print("service stopping")
				status <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(20 * time.Second):
				}
				return false, 0
			case svc.PowerEvent:
				switch c.EventType {
				case pbtAPMSuspend:
					resume.Lock()
					resume.suspended = time.Now()
					resume.Unlock()
				case pbtAPMResumeAutomatic, pbtAPMResumeSuspend:
					resume.Lock()
					slept := time.Duration(0)
					if !resume.suspended.IsZero() {
						slept = time.Since(resume.suspended)
						resume.suspended = time.Time{}
					}
					hook := resume.hook
					resume.Unlock()
					if hook != nil && c.EventType == pbtAPMResumeAutomatic {
						go hook(slept)
					}
				}
				status <- svc.Status{State: svc.Running, Accepts: accepts}
			}
		}
	}
}

// install registers the service: started by Windows at boot, restarted if it ever crashes.
func install() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	if s, err := m.OpenService(serviceName); err == nil {
		// Reinstalling: point the existing service at this program.
		defer s.Close()
		cfg, err := s.Config()
		if err != nil {
			return err
		}
		cfg.BinaryPathName = `"` + exe + `"`
		cfg.StartType = mgr.StartAutomatic
		if err := s.UpdateConfig(cfg); err != nil {
			return err
		}
		return setRecovery(s)
	}
	s, err := m.CreateService(serviceName, exe, mgr.Config{
		DisplayName:  "FurkVPN",
		Description:  "VPN FurkVPN: подключение, маршрутизация и раздача интернета.",
		StartType:    mgr.StartAutomatic,
		Dependencies: []string{"Nsi", "Tcpip"},
		ErrorControl: mgr.ErrorNormal,
	})
	if err != nil {
		return err
	}
	defer s.Close()
	return setRecovery(s)
}

func setRecovery(s *mgr.Service) error {
	return s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 2 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, 24*60*60)
}

// uninstall stops and removes the service.
func uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return nil // not installed
	}
	defer s.Close()
	if st, err := s.Query(); err == nil && st.State != svc.Stopped {
		_, _ = s.Control(svc.Stop)
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
			if st, err := s.Query(); err != nil || st.State == svc.Stopped {
				break
			}
		}
	}
	if err := s.Delete(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
		return err
	}
	return nil
}
