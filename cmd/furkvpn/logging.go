//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var logMu sync.Mutex

// logf writes to %LOCALAPPDATA%\FurkVPN\ui.log (kept under 256 KB), for when something goes wrong.
func logf(format string, args ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	path := filepath.Join(localAppData(), "FurkVPN", "ui.log")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if st, err := os.Stat(path); err == nil && st.Size() > 256<<10 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}
