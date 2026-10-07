//go:build !windows

package sysinfo

import (
	"os"
	"path/filepath"
	"strings"
)

// OSVersion on other systems (development only).
func OSVersion() string { return "dev" }

// DeviceModel on other systems (development only).
func DeviceModel() string { return "Development machine" }

// MachineGUID on other systems (development only).
func MachineGUID() string {
	b, _ := os.ReadFile("/etc/machine-id")
	return strings.TrimSpace(string(b))
}

// ExeTitle is the file name without its extension.
func ExeTitle(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
