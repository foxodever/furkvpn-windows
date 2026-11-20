//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"furkvpn/internal/settings"
)

// uiConfig is what the window remembers for this Windows user (the VPN's own settings are the
// service's, for the whole computer).
type uiConfig struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	// The app has run before: autostart was set up then.
	Initialized   bool `json:"initialized"`
	TrayHintShown bool `json:"trayHintShown"`
	// "dark" or "light"; empty follows Windows.
	Theme string `json:"theme,omitempty"`
	// The system proxy the app has set (sysproxy.go), and the user's own settings from before it.
	ProxySet   string         `json:"proxySet,omitempty"`
	ProxySaved *proxySettings `json:"proxySaved,omitempty"`
}

func userConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "FurkVPN", "ui.json")
}

func loadUIConfig() uiConfig {
	c := uiConfig{Width: 1000, Height: 700}
	if data, err := os.ReadFile(userConfigPath()); err == nil {
		_ = json.Unmarshal(data, &c)
	}
	if c.Width < 400 || c.Height < 560 {
		c.Width, c.Height = 1000, 700
	}
	return c
}

func (c uiConfig) save() {
	data, _ := json.MarshalIndent(c, "", "  ")
	_ = settings.WriteAtomically(userConfigPath(), data)
}

// uiConfig is a copy of what the window remembers.
func (a *App) uiConfig() uiConfig {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	return a.ui
}

// updateUI changes what the window remembers and saves it.
func (a *App) updateUI(change func(c *uiConfig)) {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	change(&a.ui)
	a.ui.save()
}

func localAppData() string {
	if dir, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0); err == nil {
		return dir
	}
	return os.Getenv("LOCALAPPDATA")
}

// firstRun: the app starts with Windows unless the user turns it off.
func (a *App) firstRun() {
	if a.uiConfig().Initialized {
		return
	}
	_ = setAutostart(true)
	a.updateUI(func(c *uiConfig) { c.Initialized = true })
}

// dark reports whether the window is dark: the user's choice, or else as in Windows.
func (c uiConfig) dark() bool {
	switch c.Theme {
	case "dark":
		return true
	case "light":
		return false
	}
	return systemUsesDarkTheme()
}

// systemUsesDarkTheme reads the apps' theme of Windows (Settings → Personalization → Colors).
func systemUsesDarkTheme() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}
