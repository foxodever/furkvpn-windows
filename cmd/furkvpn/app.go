//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"furkvpn/internal/ipc"
)

// App is bound to the interface: its exported methods are what the interface may call.
type App struct {
	ctx         context.Context
	lang        string
	ui          uiConfig
	startHidden bool
	servicePath string

	mu     sync.Mutex
	client *ipc.Client
	online bool
	ready  bool
	queued []func()
	vpn    vpnSlice
	names  map[string]string
	// Everything the service has said about its state, merged: a window that starts listening
	// (or reloads) gets it at once.
	state    map[string]json.RawMessage
	quitting bool
	tray     *tray
}

type vpnSlice struct {
	Status    string `json:"status"`
	ServerKey string `json:"serverKey"`
}

func newApp(args []string) *App {
	exe, _ := os.Executable()
	a := &App{
		lang:        uiLanguage(),
		ui:          loadUIConfig(),
		servicePath: filepath.Join(filepath.Dir(exe), "FurkVPNService.exe"),
		names:       map[string]string{},
		state:       map[string]json.RawMessage{},
	}
	for _, arg := range args {
		if arg == "--tray" {
			a.startHidden = true
		}
	}
	a.handleArgs(args)
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	logf("started %v", os.Args[1:])
	a.firstRun()
	go a.serviceLoop()
	a.tray = startTray(a)
}

func (a *App) domReady(ctx context.Context) {}

// beforeClose hides the window instead of quitting: the app keeps running in the tray. Only the
// tray's "Выход" really quits.
func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	quitting := a.quitting
	a.mu.Unlock()
	if quitting {
		return false
	}
	a.saveWindowSize()
	runtime.WindowHide(ctx)
	if !a.ui.TrayHintShown {
		a.ui.TrayHintShown = true
		a.ui.save()
		if a.tray != nil {
			a.tray.balloon("FurkVPN", a.text("tray_hint"))
		}
	}
	return true
}

func (a *App) shutdown(ctx context.Context) {
	if a.tray != nil {
		a.tray.close()
	}
}

func (a *App) saveWindowSize() {
	if a.ctx == nil {
		return
	}
	if runtime.WindowIsMaximised(a.ctx) || runtime.WindowIsMinimised(a.ctx) {
		return
	}
	w, h := runtime.WindowGetSize(a.ctx)
	if w >= 400 && h >= 560 {
		a.ui.Width, a.ui.Height = w, h
		a.ui.save()
	}
}

// show brings the window up from the tray.
func (a *App) show() {
	if a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
	bringToFront()
}

// quit disconnects the VPN and leaves.
func (a *App) quit() {
	a.mu.Lock()
	a.quitting = true
	client := a.client
	a.mu.Unlock()
	a.saveWindowSize()
	if client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = client.Call(ctx, "disconnect", nil, nil)
		cancel()
	}
	runtime.Quit(a.ctx)
}

// secondInstance: FurkVPN.exe was started again (from the Start menu, a furkvpn:// link, a
// settings file): show this window and pass the link or the file on.
func (a *App) secondInstance(data options.SecondInstanceData) {
	a.handleArgs(data.Args)
	tray := false
	for _, arg := range data.Args {
		if arg == "--tray" {
			tray = true
		}
	}
	if !tray {
		a.show()
	}
}

func (a *App) handleArgs(args []string) {
	for _, arg := range args {
		lower := strings.ToLower(arg)
		switch {
		case strings.HasPrefix(lower, "furkvpn:"):
			link := arg
			a.emitWhenReady("deeplink", link)
		case strings.HasSuffix(lower, ".json"):
			if data, err := readSmallFile(arg, 5<<20); err == nil {
				a.emitWhenReady("file", string(data))
			}
		}
	}
}

// emitWhenReady sends an event once the interface listens.
func (a *App) emitWhenReady(name string, data any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ready && a.ctx != nil {
		runtime.EventsEmit(a.ctx, name, data)
		return
	}
	a.queued = append(a.queued, func() { runtime.EventsEmit(a.ctx, name, data) })
}

// Ready is called by the interface once it listens to events.
func (a *App) Ready() {
	a.mu.Lock()
	a.ready = true
	queued := a.queued
	a.queued = nil
	online := a.online
	full := make(map[string]json.RawMessage, len(a.state))
	for k, v := range a.state {
		full[k] = v
	}
	a.mu.Unlock()
	runtime.EventsEmit(a.ctx, "service", map[string]bool{"online": online})
	if online && len(full) > 0 {
		runtime.EventsEmit(a.ctx, "state", full)
	}
	for _, f := range queued {
		f()
	}
}

// --- the service ------------------------------------------------------------------------

// serviceLoop keeps the connection to the service, reconnecting when it restarts.
func (a *App) serviceLoop() {
	lastErr := ""
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, err := ipc.Dial(ctx, a.servicePath)
		cancel()
		if err != nil {
			if err.Error() != lastErr {
				lastErr = err.Error()
				logf("service: %v", err)
			}
			a.setOnline(nil)
			time.Sleep(time.Second)
			continue
		}
		lastErr = ""
		logf("service: connected")
		client := ipc.NewClient(conn, a.onEvent)
		a.setOnline(client)
		go a.hello(client)
		<-client.Done()
		a.setOnline(nil)
		time.Sleep(500 * time.Millisecond)
	}
}

func (a *App) hello(client *ipc.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = client.Call(ctx, "hello", nil, nil)
}

func (a *App) setOnline(client *ipc.Client) {
	a.mu.Lock()
	changed := a.online != (client != nil)
	a.client = client
	a.online = client != nil
	if client == nil {
		a.state = map[string]json.RawMessage{}
	}
	ready := a.ready
	a.mu.Unlock()
	if changed && ready && a.ctx != nil {
		runtime.EventsEmit(a.ctx, "service", map[string]bool{"online": client != nil})
	}
	if changed && a.tray != nil {
		a.tray.update()
	}
}

func (a *App) onEvent(e ipc.Event) {
	if e.Event == "state" {
		a.watchState(e.Data)
		var parts map[string]json.RawMessage
		if json.Unmarshal(e.Data, &parts) == nil {
			a.mu.Lock()
			for k, v := range parts {
				a.state[k] = v
			}
			a.mu.Unlock()
		}
	}
	a.mu.Lock()
	ready := a.ready
	a.mu.Unlock()
	if ready && a.ctx != nil {
		runtime.EventsEmit(a.ctx, e.Event, e.Data)
	}
}

// watchState keeps what the tray shows: the VPN's state and the server's name.
func (a *App) watchState(data json.RawMessage) {
	var parts struct {
		VPN          *vpnSlice `json:"vpn"`
		Subscription *struct {
			Info *struct {
				Servers []struct {
					Key   string `json:"key"`
					Title string `json:"title"`
				} `json:"servers"`
			} `json:"info"`
		} `json:"subscription"`
	}
	if json.Unmarshal(data, &parts) != nil {
		return
	}
	a.mu.Lock()
	if parts.VPN != nil {
		a.vpn = *parts.VPN
	}
	if parts.Subscription != nil && parts.Subscription.Info != nil {
		a.names = map[string]string{}
		for _, s := range parts.Subscription.Info.Servers {
			a.names[s.Key] = s.Title
		}
	}
	a.mu.Unlock()
	if a.tray != nil && (parts.VPN != nil || parts.Subscription != nil) {
		a.tray.update()
	}
}

// trayState is what the tray icon shows.
func (a *App) trayState() (status, server string, online bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.vpn.Status, a.names[a.vpn.ServerKey], a.online
}

func (a *App) toggleVPN() {
	a.mu.Lock()
	client := a.client
	status := a.vpn.Status
	a.mu.Unlock()
	if client == nil {
		a.show()
		return
	}
	method := "connect"
	if status == "connected" || status == "connecting" {
		method = "disconnect"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Call(ctx, method, nil, nil); err != nil {
		// No key yet, for example: the window explains.
		a.show()
	}
}

// Call sends a request from the interface to the service; params and the result are JSON.
func (a *App) Call(method string, params string) (string, error) {
	a.mu.Lock()
	client := a.client
	a.mu.Unlock()
	if client == nil {
		return "", ipc.Errorf("offline", "the service is not running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var raw json.RawMessage
	if params != "" {
		raw = json.RawMessage(params)
	}
	out, err := client.CallRaw(ctx, method, raw)
	if err != nil {
		var ie *ipc.Error
		if errors.As(err, &ie) {
			return "", ie
		}
		return "", ipc.Errorf("offline", err.Error())
	}
	return string(out), nil
}

func readSmallFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("file too large")
	}
	return data, nil
}
