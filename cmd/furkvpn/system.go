//go:build windows

package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc/mgr"

	"furkvpn/internal/ipc"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
	procShellExecuteExW  = shell32.NewProc("ShellExecuteExW")
)

// ReadClipboard returns the text on the clipboard ("Вставить ключ").
func (a *App) ReadClipboard() string {
	text, _ := runtime.ClipboardGetText(a.ctx)
	return text
}

// CopyText puts text on the clipboard.
func (a *App) CopyText(text string) error {
	return runtime.ClipboardSetText(a.ctx, text)
}

// OpenURL opens a web page, Telegram or an e-mail in the system's apps. Only these kinds of links:
// whatever the interface shows comes partly from the internet.
func (a *App) OpenURL(link string) error {
	u, err := url.Parse(link)
	if err != nil {
		return err
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "http", "mailto", "tg":
	default:
		return fmt.Errorf("links of type %q are not opened", u.Scheme)
	}
	return shellOpen(u.String())
}

func shellOpen(target string) error {
	return windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(target), nil, nil, windows.SW_SHOWNORMAL)
}

// OpenHotspotSettings opens Settings → Network → Mobile hotspot.
func (a *App) OpenHotspotSettings() error { return shellOpen("ms-settings:network-mobilehotspot") }

// OpenLogFolder shows the service's logs in Explorer.
func (a *App) OpenLogFolder() error {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0)
	if err != nil {
		return err
	}
	return shellOpen(filepath.Join(dir, "FurkVPN", "logs"))
}

func (a *App) fileFilters() []runtime.FileFilter {
	return []runtime.FileFilter{{DisplayName: a.text("settings_file") + " (*.json)", Pattern: "*.json"}}
}

// SaveTextFile asks where to save a settings file; false when the user cancelled.
func (a *App) SaveTextFile(name, content string) (bool, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: name,
		Filters:         a.fileFilters(),
	})
	if err != nil || path == "" {
		return false, err
	}
	if filepath.Ext(path) == "" {
		path += ".json"
	}
	return true, os.WriteFile(path, []byte(content), 0o644)
}

// OpenTextFile asks for a settings file and returns its text ("" when the user cancelled).
func (a *App) OpenTextFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Filters: a.fileFilters()})
	if err != nil || path == "" {
		return "", err
	}
	data, err := readSmallFile(path, 5<<20)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// CopyFileToClipboard saves the settings as a file and puts the file on the clipboard, so that it
// can be pasted into Telegram or an e-mail with Ctrl+V.
func (a *App) CopyFileToClipboard(name, content string) error {
	dir := filepath.Join(os.TempDir(), "FurkVPN")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, filepath.Base(name))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	return clipboardFiles([]string{path})
}

// clipboardFiles puts files on the clipboard as CF_HDROP, like Explorer's "Copy".
func clipboardFiles(paths []string) error {
	const (
		cfHDROP      = 15
		gmemMoveable = 0x0002
		gmemZeroInit = 0x0040
		dropFilesLen = 20 // DROPFILES: pFiles, pt.x, pt.y, fNC, fWide
	)
	var list []uint16
	for _, p := range paths {
		list = append(list, utf16.Encode([]rune(p))...)
		list = append(list, 0)
	}
	list = append(list, 0)
	size := dropFilesLen + len(list)*2
	var opened bool
	for i := 0; i < 10; i++ {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			opened = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !opened {
		return errors.New("the clipboard is busy")
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	h, _, _ := procGlobalAlloc.Call(gmemMoveable|gmemZeroInit, uintptr(size))
	if h == 0 {
		return errors.New("out of memory")
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		procGlobalFree.Call(h)
		return errors.New("GlobalLock failed")
	}
	buf := unsafe.Slice((*byte)(ptrAt(ptr)), size)
	*(*uint32)(unsafe.Pointer(&buf[0])) = dropFilesLen // pFiles
	*(*uint32)(unsafe.Pointer(&buf[16])) = 1           // fWide
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&buf[dropFilesLen])), len(list)), list)
	procGlobalUnlock.Call(h)
	if r, _, err := procSetClipboardData.Call(cfHDROP, h); r == 0 {
		procGlobalFree.Call(h)
		return err
	}
	return nil
}

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// Autostart: FurkVPN starts (to the tray) when the user signs in.
func (a *App) Autostart() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("FurkVPN")
	return err == nil
}

// SetAutostart turns starting with Windows on or off.
func (a *App) SetAutostart(on bool) error { return setAutostart(on) }

func setAutostart(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		err := k.DeleteValue("FurkVPN")
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue("FurkVPN", `"`+exe+`" --tray`)
}

// Quit disconnects the VPN and closes the app.
func (a *App) Quit() { a.quit() }

// Hide sends the window to the tray.
func (a *App) Hide() { runtime.WindowHide(a.ctx) }

// StartService starts the FurkVPN service; the installer lets signed-in users do that.
func (a *App) StartService() error {
	m, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(m)
	h, err := windows.OpenService(m, windows.StringToUTF16Ptr("FurkVPN"), windows.SERVICE_START|windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return ipc.Errorf("service", err.Error())
	}
	s := &mgr.Service{Name: "FurkVPN", Handle: h}
	defer s.Close()
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return ipc.Errorf("service", err.Error())
	}
	return nil
}

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         windows.HWND
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     windows.Handle
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    windows.Handle
	dwHotKey     uint32
	hIcon        windows.Handle
	hProcess     windows.Handle
}

// InstallUpdate starts the downloaded installer with administrator rights (Windows asks the user)
// and closes the app, so that the installer can replace it; it starts the app again when done.
func (a *App) InstallUpdate(path string) error {
	updates := strings.ToLower(filepath.Clean(filepath.Join(programData(), "FurkVPN Updates"))) + `\`
	if !strings.HasPrefix(strings.ToLower(filepath.Clean(path)), updates) || !strings.EqualFold(filepath.Ext(path), ".exe") {
		return ipc.Errorf("update_failed", "unexpected installer path")
	}
	const seeMaskNoCloseProcess = 0x40
	info := shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess,
		lpVerb:       windows.StringToUTF16Ptr("runas"),
		lpFile:       windows.StringToUTF16Ptr(path),
		lpParameters: windows.StringToUTF16Ptr("/S /UPDATE"),
		nShow:        windows.SW_SHOWNORMAL,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if r, _, err := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return ipc.Errorf("cancelled", "")
		}
		return ipc.Errorf("update_failed", err.Error())
	}
	if info.hProcess != 0 {
		windows.CloseHandle(info.hProcess)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		a.mu.Lock()
		a.quitting = true
		a.mu.Unlock()
		runtime.Quit(a.ctx)
	}()
	return nil
}

func programData() string {
	if dir, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0); err == nil {
		return dir
	}
	return os.Getenv("ProgramData")
}

var procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")

// bringToFront lets the window come to the front when another copy of the app asked for it.
func bringToFront() {
	procAllowSetForegroundWindow.Call(uintptr(^uint32(0))) // ASFW_ANY
}

// ptrAt turns an address the system gave (memory Go doesn't manage, like a GlobalLock'ed block)
// into a pointer.
func ptrAt(addr uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&addr)) }
