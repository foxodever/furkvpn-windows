//go:build windows

package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
)

const firewallRule = "FurkVPN: раздача интернета"

var firewall struct {
	sync.Mutex
	ports string
}

// openShareFirewall lets devices on the local network reach the share proxy: Windows Firewall
// blocks incoming connections by default. Only the service's own program on those ports.
func openShareFirewall(httpPort, socksPort int) {
	ports := strconv.Itoa(httpPort) + "," + strconv.Itoa(socksPort)
	firewall.Lock()
	defer firewall.Unlock()
	if firewall.ports == ports {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	netsh("advfirewall", "firewall", "delete", "rule", "name="+firewallRule)
	for _, proto := range []string{"TCP", "UDP"} {
		netsh("advfirewall", "firewall", "add", "rule", "name="+firewallRule, "dir=in", "action=allow",
			"protocol="+proto, "localport="+ports, "program="+exe, "profile=any", "edge=no")
	}
	firewall.ports = ports
}

func closeShareFirewall() {
	firewall.Lock()
	defer firewall.Unlock()
	if firewall.ports == "" {
		return
	}
	netsh("advfirewall", "firewall", "delete", "rule", "name="+firewallRule)
	firewall.ports = ""
}

// ResetFirewall removes a rule left over from a crash.
func ResetFirewall() {
	netsh("advfirewall", "firewall", "delete", "rule", "name="+firewallRule)
}

func netsh(args ...string) {
	system32, err := windows.GetSystemDirectory()
	if err != nil {
		return
	}
	cmd := exec.Command(filepath.Join(system32, "netsh.exe"), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	_ = cmd.Run()
}

// SecureDirs gives the service's folders their own permissions: the data (the key among it) only
// to the system and administrators; the downloaded installers readable by everyone but writable
// only by them, so nobody can swap an installer between its check and its start.
func SecureDirs(dataDir, updatesDir string) error {
	for _, d := range []struct {
		path string
		sddl string
	}{
		{dataDir, "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"},
		{updatesDir, "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;0x1200a9;;;BU)"},
	} {
		if err := os.MkdirAll(d.path, 0o755); err != nil {
			return err
		}
		sd, err := windows.SecurityDescriptorFromString(d.sddl)
		if err != nil {
			return err
		}
		dacl, _, err := sd.DACL()
		if err != nil {
			return err
		}
		err = windows.SetNamedSecurityInfo(d.path, windows.SE_FILE_OBJECT,
			windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
		if err != nil {
			return err
		}
	}
	return nil
}
