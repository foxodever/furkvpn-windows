//go:build windows

package connlog

import "golang.org/x/sys/windows"

func systemRoot() string {
	dir, err := windows.GetWindowsDirectory()
	if err != nil {
		return `C:\Windows`
	}
	return dir
}
