//go:build windows

package main

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

//go:embed icons/tray-off.ico
var trayOff []byte

//go:embed icons/tray-busy.ico
var trayBusy []byte

//go:embed icons/tray-on.ico
var trayOn []byte

var (
	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procAppendMenuW            = user32.NewProc("AppendMenuW")
	procSetMenuDefaultItem     = user32.NewProc("SetMenuDefaultItem")
	procTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procCreateIconFromResEx    = user32.NewProc("CreateIconFromResourceEx")
	procShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmNull        = 0x0000
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmContextMenu = 0x007B
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmApp         = 0x8000
	wmTray        = wmApp + 1
	wmTrayUpdate  = wmApp + 2
	wmTrayBalloon = wmApp + 3
	ninSelect     = 0x0400
	ninKeySelect  = 0x0401

	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4
	nifMessage    = 0x1
	nifIcon       = 0x2
	nifTip        = 0x4
	nifInfo       = 0x10
	nifShowTip    = 0x80
	niifInfo      = 0x1

	mfString    = 0x0
	mfGrayed    = 0x1
	mfSeparator = 0x800

	tpmReturnCmd   = 0x0100
	tpmNoNotify    = 0x0080
	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020

	smCxSmIcon = 49

	menuToggle = 1
	menuOpen   = 2
	menuQuit   = 3
)

type notifyIconData struct {
	cbSize           uint32
	hWnd             windows.HWND
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            windows.Handle
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         windows.GUID
	hBalloonIcon     windows.Handle
}

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

type point struct{ x, y int32 }

type msg struct {
	hwnd     windows.HWND
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

// tray is the icon next to the clock: its color shows the VPN's state, a click opens the window,
// the right button has Connect / Open / Quit.
type tray struct {
	app            *App
	hwnd           windows.HWND
	icons          map[string]windows.Handle
	taskbarCreated uint32

	mu           sync.Mutex
	balloonTitle string
	balloonText  string
}

var trayInstance *tray

func startTray(a *App) *tray {
	t := &tray{app: a, icons: map[string]windows.Handle{}}
	ready := make(chan error, 1)
	go t.run(ready)
	select {
	case err := <-ready:
		if err != nil {
			return nil
		}
	case <-time.After(5 * time.Second):
		return nil
	}
	return t
}

func (t *tray) run(ready chan<- error) {
	// The window and its messages belong to this thread.
	runtime.LockOSThread()
	trayInstance = t
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className := windows.StringToUTF16Ptr("FurkVPNTray")
	wc := wndClassEx{
		lpfnWndProc:   syscall.NewCallback(trayWndProc),
		hInstance:     windows.Handle(hInstance),
		lpszClassName: className,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	// A hidden ordinary window (not message-only): it hears TaskbarCreated when Explorer restarts.
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("FurkVPN"))), 0, 0, 0, 0, 0, 0, 0, hInstance, 0)
	if hwnd == 0 {
		ready <- err
		return
	}
	t.hwnd = windows.HWND(hwnd)
	tc, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("TaskbarCreated"))))
	t.taskbarCreated = uint32(tc)
	size, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	for name, data := range map[string][]byte{"off": trayOff, "busy": trayBusy, "on": trayOn} {
		if h, err := iconFromICO(data, int(size)); err == nil {
			t.icons[name] = h
		}
	}
	t.add()
	ready <- nil
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func trayWndProc(hwnd windows.HWND, message uint32, wParam, lParam uintptr) uintptr {
	t := trayInstance
	if t == nil {
		r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
		return r
	}
	switch message {
	case wmTray:
		switch uint32(lParam & 0xFFFF) {
		case wmLButtonUp, ninSelect, ninKeySelect:
			go t.app.show()
		case wmContextMenu, wmRButtonUp:
			t.menu()
		}
		return 0
	case wmTrayUpdate:
		t.modify(nifIcon | nifTip | nifShowTip)
		return 0
	case wmTrayBalloon:
		t.modify(nifInfo)
		return 0
	case wmClose:
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case wmDestroy:
		t.remove()
		procPostQuitMessage.Call(0)
		return 0
	}
	if t.taskbarCreated != 0 && message == t.taskbarCreated {
		t.add()
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return r
}

func (t *tray) data(flags uint32) *notifyIconData {
	d := &notifyIconData{hWnd: t.hwnd, uID: 1, uFlags: flags, uCallbackMessage: wmTray}
	d.cbSize = uint32(unsafe.Sizeof(*d))
	status, server, online := t.app.trayState()
	icon := "off"
	text := t.app.text("status_idle")
	switch {
	case !online:
		text = t.app.text("status_offline")
	case status == "connected":
		icon, text = "on", t.app.text("status_connected")
		if server != "" {
			text += " · " + server
		}
	case status == "connecting":
		icon, text = "busy", t.app.text("status_connecting")
	case status == "stopping":
		icon, text = "busy", t.app.text("status_stopping")
	}
	d.hIcon = t.icons[icon]
	copyUTF16(d.szTip[:], "FurkVPN — "+text)
	if flags&nifInfo != 0 {
		t.mu.Lock()
		copyUTF16(d.szInfoTitle[:], t.balloonTitle)
		copyUTF16(d.szInfo[:], t.balloonText)
		t.mu.Unlock()
		d.dwInfoFlags = niifInfo
	}
	return d
}

func (t *tray) add() {
	d := t.data(nifMessage | nifIcon | nifTip | nifShowTip)
	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(d)))
	d.uVersion = 4 // NOTIFYICON_VERSION_4
	procShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(d)))
}

func (t *tray) modify(flags uint32) {
	d := t.data(flags)
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(d)))
}

func (t *tray) remove() {
	d := &notifyIconData{hWnd: t.hwnd, uID: 1}
	d.cbSize = uint32(unsafe.Sizeof(*d))
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(d)))
}

// update redraws the icon and its tooltip (from any goroutine).
func (t *tray) update() {
	procPostMessageW.Call(uintptr(t.hwnd), wmTrayUpdate, 0, 0)
}

// balloon shows a notification from the tray icon.
func (t *tray) balloon(title, text string) {
	t.mu.Lock()
	t.balloonTitle, t.balloonText = title, text
	t.mu.Unlock()
	procPostMessageW.Call(uintptr(t.hwnd), wmTrayBalloon, 0, 0)
}

func (t *tray) close() {
	procPostMessageW.Call(uintptr(t.hwnd), wmClose, 0, 0)
}

func (t *tray) menu() {
	status, _, online := t.app.trayState()
	m, _, _ := procCreatePopupMenu.Call()
	if m == 0 {
		return
	}
	defer procDestroyMenu.Call(m)
	toggle := t.app.text("tray_connect")
	if status == "connected" || status == "connecting" {
		toggle = t.app.text("tray_disconnect")
	}
	flags := uintptr(mfString)
	if !online {
		flags |= mfGrayed
	}
	procAppendMenuW.Call(m, flags, menuToggle, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(toggle))))
	procAppendMenuW.Call(m, mfSeparator, 0, 0)
	procAppendMenuW.Call(m, mfString, menuOpen, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(t.app.text("tray_open")))))
	procAppendMenuW.Call(m, mfString, menuQuit, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(t.app.text("tray_quit")))))
	procSetMenuDefaultItem.Call(m, menuOpen, 0)
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Without this the menu stays open after clicking elsewhere.
	procSetForegroundWindow.Call(uintptr(t.hwnd))
	cmd, _, _ := procTrackPopupMenu.Call(m, tpmReturnCmd|tpmNoNotify|tpmRightButton|tpmBottomAlign, uintptr(pt.x), uintptr(pt.y), 0, uintptr(t.hwnd), 0)
	procPostMessageW.Call(uintptr(t.hwnd), wmNull, 0, 0)
	switch cmd {
	case menuToggle:
		go t.app.toggleVPN()
	case menuOpen:
		go t.app.show()
	case menuQuit:
		go t.app.quit()
	}
}

func copyUTF16(dst []uint16, s string) {
	src, _ := windows.UTF16FromString(s)
	if len(src) > len(dst) {
		src = src[:len(dst)]
		src[len(src)-1] = 0
	}
	copy(dst, src)
}

// iconFromICO picks the image of an .ico file closest to size (but not smaller, when possible).
func iconFromICO(data []byte, size int) (windows.Handle, error) {
	if len(data) < 6 || binary.LittleEndian.Uint16(data[2:]) != 1 {
		return 0, errors.New("not an icon")
	}
	count := int(binary.LittleEndian.Uint16(data[4:]))
	bestIdx, bestSize := -1, 0
	for i := 0; i < count; i++ {
		e := data[6+16*i:]
		w := int(e[0])
		if w == 0 {
			w = 256
		}
		better := bestIdx < 0 ||
			(w >= size && (bestSize < size || w < bestSize)) ||
			(w < size && bestSize < size && w > bestSize)
		if better {
			bestIdx, bestSize = i, w
		}
	}
	if bestIdx < 0 {
		return 0, errors.New("empty icon")
	}
	e := data[6+16*bestIdx:]
	length := binary.LittleEndian.Uint32(e[8:])
	offset := binary.LittleEndian.Uint32(e[12:])
	if int(offset+length) > len(data) {
		return 0, errors.New("broken icon")
	}
	img := data[offset : offset+length]
	h, _, err := procCreateIconFromResEx.Call(uintptr(unsafe.Pointer(&img[0])), uintptr(length), 1, 0x00030000, uintptr(size), uintptr(size), 0)
	if h == 0 {
		return 0, err
	}
	return windows.Handle(h), nil
}
