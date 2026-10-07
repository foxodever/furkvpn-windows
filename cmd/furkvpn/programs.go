//go:build windows

package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"

	"furkvpn/internal/sysinfo"
	"furkvpn/internal/transfer"
)

// Program is a program on this computer for "Программы без VPN"; Name (the file name) is what
// the setting keeps and what Xray matches.
type Program struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Path    string `json:"path"`
	Running bool   `json:"running"`
}

// Programs lists the programs of the Start menu and the running ones.
func (a *App) Programs() ([]Program, error) {
	byName := map[string]*Program{}
	add := func(p Program) {
		key := strings.ToLower(p.Name)
		if old, ok := byName[key]; ok {
			old.Running = old.Running || p.Running
			return
		}
		byName[key] = &p
	}
	for _, p := range startMenuPrograms() {
		add(p)
	}
	for _, p := range runningPrograms() {
		add(p)
	}
	out := make([]Program, 0, len(byName))
	for _, p := range byName {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title) })
	return out, nil
}

// PickProgram asks for a program file.
func (a *App) PickProgram() (*Program, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Filters: []runtime.FileFilter{{DisplayName: a.text("program") + " (*.exe)", Pattern: "*.exe"}},
	})
	if err != nil || path == "" {
		return nil, err
	}
	name, ok := transfer.ProgramName(path)
	if !ok {
		return nil, errors.New("not a program")
	}
	return &Program{Name: name, Title: sysinfo.ExeTitle(path), Path: path}, nil
}

func ignored(path string) bool {
	lower := strings.ToLower(path)
	windir, _ := windows.GetWindowsDirectory()
	base := filepath.Base(lower)
	return strings.HasPrefix(lower, strings.ToLower(windir)+`\`) ||
		strings.Contains(base, "unins") || strings.Contains(base, "uninstall") ||
		base == "furkvpn.exe" || base == "furkvpnservice.exe" || base == "msedgewebview2.exe" ||
		!strings.HasSuffix(base, ".exe")
}

// startMenuPrograms resolves the shortcuts of the Start menu (everyone's and this user's).
func startMenuPrograms() []Program {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		var oleErr *ole.OleError
		// S_FALSE: already initialized on this thread.
		if !errors.As(err, &oleErr) || oleErr.Code() != 1 {
			return nil
		}
	}
	defer ole.CoUninitialize()
	unknown, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return nil
	}
	defer unknown.Release()
	shell, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return nil
	}
	defer shell.Release()

	var roots []string
	if dir, err := windows.KnownFolderPath(windows.FOLDERID_CommonPrograms, 0); err == nil {
		roots = append(roots, dir)
	}
	if dir, err := windows.KnownFolderPath(windows.FOLDERID_Programs, 0); err == nil {
		roots = append(roots, dir)
	}
	var out []Program
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".lnk") {
				return nil
			}
			res, err := oleutil.CallMethod(shell, "CreateShortcut", path)
			if err != nil {
				return nil
			}
			link := res.ToIDispatch()
			target, err := oleutil.GetProperty(link, "TargetPath")
			link.Release()
			if err != nil {
				return nil
			}
			exe := target.ToString()
			if exe == "" || ignored(exe) {
				return nil
			}
			if _, err := os.Stat(exe); err != nil {
				return nil
			}
			name, ok := transfer.ProgramName(exe)
			if !ok {
				return nil
			}
			title := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			out = append(out, Program{Name: name, Title: title, Path: exe})
			return nil
		})
	}
	return out
}

// runningPrograms are the programs running now, Windows' own aside.
func runningPrograms() []Program {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var out []Program
	seen := map[string]bool{}
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		path := processPath(entry.ProcessID)
		if path == "" || ignored(path) || seen[strings.ToLower(path)] {
			continue
		}
		seen[strings.ToLower(path)] = true
		name, ok := transfer.ProgramName(path)
		if !ok {
			continue
		}
		out = append(out, Program{Name: name, Title: sysinfo.ExeTitle(path), Path: path, Running: true})
	}
	return out
}

func processPath(pid uint32) string {
	if pid == 0 || pid == 4 {
		return ""
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

// --- icons --------------------------------------------------------------------------------

var (
	procSHGetFileInfoW = shell32.NewProc("SHGetFileInfoW")
	procDestroyIcon    = user32.NewProc("DestroyIcon")
	procGetIconInfo    = user32.NewProc("GetIconInfo")
	procGetDC          = user32.NewProc("GetDC")
	procReleaseDC      = user32.NewProc("ReleaseDC")
	gdi32              = windows.NewLazySystemDLL("gdi32.dll")
	procGetDIBits      = gdi32.NewProc("GetDIBits")
	procGetObjectW     = gdi32.NewProc("GetObjectW")
	procDeleteObject   = gdi32.NewProc("DeleteObject")
)

type shFileInfo struct {
	hIcon         windows.Handle
	iIcon         int32
	dwAttributes  uint32
	szDisplayName [260]uint16
	szTypeName    [80]uint16
}

type iconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  windows.Handle
	hbmColor windows.Handle
}

type bitmap struct {
	bmType       int32
	bmWidth      int32
	bmHeight     int32
	bmWidthBytes int32
	bmPlanes     uint16
	bmBitsPixel  uint16
	bmBits       uintptr
}

type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

// ProgramIcons returns the programs' icons as PNG data URLs, by path.
func (a *App) ProgramIcons(paths []string) map[string]string {
	out := map[string]string{}
	for _, p := range paths {
		if len(out) >= 200 {
			break
		}
		if png, err := fileIconPNG(p); err == nil {
			out[p] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
		}
	}
	return out
}

func fileIconPNG(path string) ([]byte, error) {
	const shgfiIcon, shgfiLargeIcon = 0x100, 0x0
	var info shFileInfo
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	r, _, _ := procSHGetFileInfoW.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), shgfiIcon|shgfiLargeIcon)
	if r == 0 || info.hIcon == 0 {
		return nil, errors.New("no icon")
	}
	defer procDestroyIcon.Call(uintptr(info.hIcon))
	return iconToPNG(info.hIcon)
}

func iconToPNG(hicon windows.Handle) ([]byte, error) {
	var ii iconInfo
	if r, _, _ := procGetIconInfo.Call(uintptr(hicon), uintptr(unsafe.Pointer(&ii))); r == 0 {
		return nil, errors.New("GetIconInfo failed")
	}
	if ii.hbmMask != 0 {
		defer procDeleteObject.Call(uintptr(ii.hbmMask))
	}
	if ii.hbmColor == 0 {
		return nil, errors.New("monochrome icon")
	}
	defer procDeleteObject.Call(uintptr(ii.hbmColor))
	var bm bitmap
	procGetObjectW.Call(uintptr(ii.hbmColor), unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm)))
	w, h := int(bm.bmWidth), int(bm.bmHeight)
	if w <= 0 || h <= 0 || w > 256 || h > 256 {
		return nil, errors.New("odd icon size")
	}
	pixels, err := dibits(ii.hbmColor, w, h)
	if err != nil {
		return nil, err
	}
	hasAlpha := false
	for i := 3; i < len(pixels); i += 4 {
		if pixels[i] != 0 {
			hasAlpha = true
			break
		}
	}
	var mask []byte
	if !hasAlpha && ii.hbmMask != 0 {
		mask, _ = dibits(ii.hbmMask, w, h)
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			a := pixels[i+3]
			if !hasAlpha {
				a = 255
				if mask != nil && mask[i] != 0 {
					a = 0
				}
			}
			img.SetNRGBA(x, y, color.NRGBA{R: pixels[i+2], G: pixels[i+1], B: pixels[i], A: a})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// dibits reads a bitmap as top-down 32-bit BGRA.
func dibits(hbm windows.Handle, w, h int) ([]byte, error) {
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return nil, errors.New("GetDC failed")
	}
	defer procReleaseDC.Call(0, hdc)
	header := bitmapInfoHeader{biWidth: int32(w), biHeight: -int32(h), biPlanes: 1, biBitCount: 32}
	header.biSize = uint32(unsafe.Sizeof(header))
	// BITMAPINFO: the header plus room for the color table GetDIBits may write.
	info := make([]byte, unsafe.Sizeof(header)+256*4)
	*(*bitmapInfoHeader)(unsafe.Pointer(&info[0])) = header
	buf := make([]byte, w*h*4)
	if r, _, _ := procGetDIBits.Call(hdc, uintptr(hbm), 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&info[0])), 0); r == 0 {
		return nil, errors.New("GetDIBits failed")
	}
	return buf, nil
}
