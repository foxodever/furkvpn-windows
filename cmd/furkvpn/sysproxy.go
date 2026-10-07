//go:build windows

package main

import (
	"runtime"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The system proxy. Without the TUN (Settings → Расширенные настройки) the VPN is a proxy on
// 127.0.0.1, and while it runs the app points Windows' proxy settings at it, so browsers and
// most programs use it. Those settings are each user's own, out of the service's reach, so the
// window's process sets them, and puts back what was there before when the VPN goes off or the
// app quits. What it set is remembered in ui.json, so a crash or a power cut is undone on the next
// start.

var (
	wininet                  = windows.NewLazySystemDLL("wininet.dll")
	procInternetSetOptionW   = wininet.NewProc("InternetSetOptionW")
	procInternetQueryOptionW = wininet.NewProc("InternetQueryOptionW")
)

const (
	internetOptionRefresh             = 37
	internetOptionSettingsChanged     = 39
	internetOptionPerConnectionOption = 75

	perConnFlags         = 1
	perConnProxyServer   = 2
	perConnProxyBypass   = 3
	perConnAutoconfigURL = 4
	perConnFlagsUI       = 10

	proxyTypeDirect = 0x1
	proxyTypeProxy  = 0x2
)

// Addresses that never go to the proxy: this computer and the local networks.
const proxyBypass = "localhost;127.*;10.*;172.16.*;172.17.*;172.18.*;172.19.*;172.20.*;172.21.*;172.22.*;" +
	"172.23.*;172.24.*;172.25.*;172.26.*;172.27.*;172.28.*;172.29.*;172.30.*;172.31.*;192.168.*;<local>"

// proxySettings are the proxy settings of the "LAN" connection (Settings → Network → Proxy).
type proxySettings struct {
	Flags         uint32 `json:"flags"`
	Server        string `json:"server,omitempty"`
	Bypass        string `json:"bypass,omitempty"`
	AutoConfigURL string `json:"autoConfigUrl,omitempty"`
}

// perConnOption is INTERNET_PER_CONN_OPTIONW: the value is a DWORD or a string.
type perConnOption struct {
	option uint32
	value  uintptr
}

// perConnOptionList is INTERNET_PER_CONN_OPTION_LISTW; no connection name means the LAN one.
type perConnOptionList struct {
	size        uint32
	connection  *uint16
	optionCount uint32
	optionError uint32
	options     *perConnOption
}

func newPerConnList(opts []perConnOption) *perConnOptionList {
	list := &perConnOptionList{optionCount: uint32(len(opts)), options: &opts[0]}
	list.size = uint32(unsafe.Sizeof(*list))
	return list
}

func queryPerConn(opts []perConnOption) error {
	list := newPerConnList(opts)
	size := list.size
	if r, _, err := procInternetQueryOptionW.Call(0, internetOptionPerConnectionOption, uintptr(unsafe.Pointer(list)), uintptr(unsafe.Pointer(&size))); r == 0 {
		return err
	}
	return nil
}

func querySystemProxy() (proxySettings, error) {
	// The flags as Windows' settings show them (with "detect automatically"); older systems only
	// know the plain ones.
	opts := []perConnOption{{option: perConnFlagsUI}, {option: perConnProxyServer}, {option: perConnProxyBypass}, {option: perConnAutoconfigURL}}
	if err := queryPerConn(opts); err != nil {
		opts[0].option = perConnFlags
		if err := queryPerConn(opts); err != nil {
			return proxySettings{}, err
		}
	}
	return proxySettings{
		Flags:         uint32(opts[0].value),
		Server:        takeString(opts[1].value),
		Bypass:        takeString(opts[2].value),
		AutoConfigURL: takeString(opts[3].value),
	}, nil
}

// takeString reads a string WinINet allocated for the caller, and frees it.
func takeString(addr uintptr) string {
	if addr == 0 {
		return ""
	}
	s := windows.UTF16PtrToString((*uint16)(ptrAt(addr)))
	procGlobalFree.Call(addr)
	return s
}

func setSystemProxy(p proxySettings) error {
	server, err := windows.UTF16PtrFromString(p.Server)
	if err != nil {
		return err
	}
	bypass, err := windows.UTF16PtrFromString(p.Bypass)
	if err != nil {
		return err
	}
	pac, err := windows.UTF16PtrFromString(p.AutoConfigURL)
	if err != nil {
		return err
	}
	opts := []perConnOption{
		{option: perConnFlags, value: uintptr(p.Flags)},
		{option: perConnProxyServer, value: uintptr(unsafe.Pointer(server))},
		{option: perConnProxyBypass, value: uintptr(unsafe.Pointer(bypass))},
		{option: perConnAutoconfigURL, value: uintptr(unsafe.Pointer(pac))},
	}
	list := newPerConnList(opts)
	r, _, err := procInternetSetOptionW.Call(0, internetOptionPerConnectionOption, uintptr(unsafe.Pointer(list)), uintptr(list.size))
	runtime.KeepAlive(server)
	runtime.KeepAlive(bypass)
	runtime.KeepAlive(pac)
	if r == 0 {
		return err
	}
	// Running programs reread the settings.
	procInternetSetOptionW.Call(0, internetOptionSettingsChanged, 0, 0)
	procInternetSetOptionW.Call(0, internetOptionRefresh, 0, 0)
	return nil
}

// syncSystemProxy points Windows' proxy at the VPN's proxy while it runs (and the setting is on),
// and puts the user's own settings back otherwise.
func (a *App) syncSystemProxy() {
	a.mu.Lock()
	want := ""
	if a.online && !a.quitting && a.vpn.Status == "connected" && a.vpn.LocalPort > 0 && a.systemProxy {
		want = "127.0.0.1:" + strconv.Itoa(a.vpn.LocalPort)
	}
	a.mu.Unlock()
	a.proxyMu.Lock()
	defer a.proxyMu.Unlock()
	cfg := a.uiConfig()
	if want == cfg.ProxySet {
		return
	}
	if want == "" {
		a.restoreProxyLocked()
		return
	}
	saved := cfg.ProxySaved
	if saved == nil {
		current, err := querySystemProxy()
		if err != nil {
			logf("system proxy: %v", err)
			return
		}
		saved = &current
	}
	if err := setSystemProxy(proxySettings{Flags: proxyTypeDirect | proxyTypeProxy, Server: want, Bypass: proxyBypass}); err != nil {
		logf("system proxy: %v", err)
		return
	}
	logf("system proxy: %s", want)
	a.updateUI(func(c *uiConfig) { c.ProxySet, c.ProxySaved = want, saved })
}

// restoreProxy puts back the user's own proxy settings if the app's are set.
func (a *App) restoreProxy() {
	a.proxyMu.Lock()
	defer a.proxyMu.Unlock()
	a.restoreProxyLocked()
}

func (a *App) restoreProxyLocked() {
	cfg := a.uiConfig()
	if cfg.ProxySet == "" && cfg.ProxySaved == nil {
		return
	}
	// Only while the settings are still the app's: if the user has changed them since, theirs stay.
	if current, err := querySystemProxy(); err == nil && cfg.ProxySaved != nil &&
		current.Server == cfg.ProxySet && current.Flags&proxyTypeProxy != 0 {
		if err := setSystemProxy(*cfg.ProxySaved); err != nil {
			logf("system proxy: restoring: %v", err)
			return
		}
		logf("system proxy: restored")
	} else if err != nil {
		logf("system proxy: %v", err)
	}
	a.updateUI(func(c *uiConfig) { c.ProxySet, c.ProxySaved = "", nil })
}

// restoreProxyAndExit is "FurkVPN.exe --restore-proxy", run by the installer and the uninstaller
// once they have stopped the app.
func restoreProxyAndExit() {
	a := &App{ui: loadUIConfig()}
	a.restoreProxy()
}
