//go:build windows

package core

import "golang.zx2c4.com/wintun"

// ReinstallAdapterDriver takes the VPN adapter's driver (Wintun) out of Windows, so the next Start
// installs it anew from wintun.dll. That is the way out when Windows lists the driver as installed
// while its file or its service is gone (another VPN's uninstaller, an antivirus): the adapter is
// then made, never comes up, and Start ends with "The system cannot find the file specified".
// It fails, changing nothing, while another program's adapter uses the driver.
func ReinstallAdapterDriver() error { return wintun.Uninstall() }
