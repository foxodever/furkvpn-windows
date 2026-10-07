//go:build windows

package connlog

import (
	"path/filepath"
	"strings"

	xnet "github.com/xtls/xray-core/common/net"

	"furkvpn/internal/sysinfo"
)

// WindowsResolver finds the program behind a connection from the system's TCP and UDP tables,
// the same way Xray's "process" routing rule does.
type WindowsResolver struct{}

// Owner looks up the socket while it is still open (the core calls the listener before the
// connection goes on).
func (WindowsResolver) Owner(network, srcIP string, srcPort int, dstIP string, dstPort int) Owner {
	if srcIP == "" || srcPort <= 0 {
		return Owner{Key: "unknown", Kind: "unknown"}
	}
	pid, name, path, err := xnet.FindProcess(network, srcIP, uint16(srcPort), dstIP, uint16(dstPort))
	if err != nil || pid <= 0 {
		return Owner{Key: "unknown", Kind: "unknown"}
	}
	// ":System" and ":System Idle Process" are the kernel's own.
	if strings.HasPrefix(path, ":") || pid == 4 {
		return Owner{Key: "system:kernel", Label: "System", Kind: "system"}
	}
	winPath := filepath.FromSlash(path)
	lower := strings.ToLower(winPath)
	windir := strings.ToLower(filepath.Join(systemRoot(), "System32")) + `\`
	// Windows' own services (svchost and the like) are grouped as the system.
	if strings.HasPrefix(lower, windir) && (strings.EqualFold(name, "svchost") || strings.EqualFold(name, "lsass") || strings.EqualFold(name, "services")) {
		return Owner{Key: "system:" + strings.ToLower(name), Path: winPath, Label: name, Kind: "system"}
	}
	return Owner{Key: "exe:" + lower, Path: winPath, Label: sysinfo.ExeTitle(winPath), Kind: "program"}
}
