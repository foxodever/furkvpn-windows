//go:build windows

package sysinfo

import (
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var descriptions sync.Map // path -> string

// ExeTitle is the name a program shows about itself ("Google Chrome"), from its version
// resource, or the file name without ".exe".
func ExeTitle(path string) string {
	if v, ok := descriptions.Load(strings.ToLower(path)); ok {
		return v.(string)
	}
	title := versionString(path, "FileDescription")
	if title == "" {
		title = versionString(path, "ProductName")
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	descriptions.Store(strings.ToLower(path), title)
	return title
}

func versionString(path, name string) string {
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil || size == 0 {
		return ""
	}
	buf := make([]byte, size)
	if err := windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&buf[0])); err != nil {
		return ""
	}
	// The first language and code page the file lists.
	var translations *[2]uint16
	var n uint32
	block := `\VarFileInfo\Translation`
	if err := windows.VerQueryValue(unsafe.Pointer(&buf[0]), block, unsafe.Pointer(&translations), &n); err != nil || n < 4 {
		return ""
	}
	lang := translations[0]
	codepage := translations[1]
	for _, sub := range []string{
		`\StringFileInfo\` + hex4(lang) + hex4(codepage) + `\` + name,
		`\StringFileInfo\040904B0\` + name,
		`\StringFileInfo\040904E4\` + name,
	} {
		var value *uint16
		var length uint32
		if windows.VerQueryValue(unsafe.Pointer(&buf[0]), sub, unsafe.Pointer(&value), &length) == nil && length > 0 {
			s := strings.TrimSpace(windows.UTF16PtrToString(value))
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func hex4(v uint16) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[v>>12&0xF], digits[v>>8&0xF], digits[v>>4&0xF], digits[v&0xF]})
}
