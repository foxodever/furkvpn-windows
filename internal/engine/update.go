package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"furkvpn/internal/ipc"
	"furkvpn/internal/remote"
	"furkvpn/internal/settings"
	"furkvpn/internal/version"
)

// UpdatesDir is where downloaded installers wait: readable by the window, which starts them,
// writable only by the service.
func (e *Engine) UpdatesDir() string {
	return filepath.Join(filepath.Dir(e.opt.DataDir), "FurkVPN Updates")
}

// updateAvailability shows the update card when furkapp.net offers a newer version that the user
// hasn't skipped.
func (e *Engine) updateAvailability() {
	cfg := e.cfg()
	skipped := e.settings.Get().SkippedUpdate
	var available *remote.UpdateInfo
	if u := cfg.Windows.Update; u != nil && u.VersionCode > version.CodeInt() && u.VersionCode > skipped {
		c := *u
		available = &c
	}
	e.mu.Lock()
	changedVersion := (e.upd.Available == nil) != (available == nil) ||
		(available != nil && e.upd.Available != nil && available.VersionCode != e.upd.Available.VersionCode)
	e.upd.Available = available
	if changedVersion {
		e.upd.Ready = false
		e.upd.Progress = nil
		e.upd.Error = ""
	}
	e.mu.Unlock()
	e.changed("update")
}

// CheckForUpdates asks furkapp.net now; a manual check brings back a skipped update.
func (e *Engine) CheckForUpdates() (bool, error) {
	if err := e.refreshConfig(e.ctx); err != nil {
		return false, ipc.Errorf("download", err.Error())
	}
	u := e.cfg().Windows.Update
	if u == nil || u.VersionCode <= version.CodeInt() {
		return false, nil
	}
	_, _ = e.settings.Update(func(s *settings.Settings) { s.SkippedUpdate = 0 })
	e.updateAvailability()
	return true, nil
}

// SkipUpdate hides the update card until a newer version is published.
func (e *Engine) SkipUpdate() {
	e.mu.Lock()
	u := e.upd.Available
	e.mu.Unlock()
	if u == nil {
		return
	}
	_, _ = e.settings.Update(func(s *settings.Settings) { s.SkippedUpdate = u.VersionCode })
	e.updateAvailability()
}

func installerPath(dir string, u *remote.UpdateInfo) string {
	name := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r == '.' {
			return r
		}
		return -1
	}, u.VersionName)
	return filepath.Join(dir, fmt.Sprintf("FurkVPN-Setup-%s-%d.exe", name, u.VersionCode))
}

// DownloadUpdate fetches the installer and checks it against the SHA-256 from furkapp.net. The
// window starts it (Windows asks the user for permission).
func (e *Engine) DownloadUpdate() (string, error) {
	e.mu.Lock()
	u := e.upd.Available
	busy := e.upd.Progress != nil
	e.mu.Unlock()
	if u == nil {
		return "", ipc.Errorf("no_update", "")
	}
	if busy {
		return "", ipc.Errorf("busy", "")
	}
	dir := e.UpdatesDir()
	path := installerPath(dir, u)
	if ok, _ := fileHasHash(path, u.SHA256); ok {
		e.setUpdateProgress(nil, true, "")
		return path, nil
	}
	zero := 0.0
	e.setUpdateProgress(&zero, false, "")
	err := e.download(u, path)
	if err != nil {
		e.log.Printf("update download: %v", err)
		e.setUpdateProgress(nil, false, "failed")
		return "", ipc.Errorf("update_failed", err.Error())
	}
	e.setUpdateProgress(nil, true, "")
	return path, nil
}

func (e *Engine) setUpdateProgress(p *float64, ready bool, errText string) {
	e.mu.Lock()
	e.upd.Progress = p
	e.upd.Ready = ready
	e.upd.Error = errText
	e.mu.Unlock()
	e.changed("update")
}

func (e *Engine) download(u *remote.UpdateInfo, path string) error {
	if u.SHA256 == "" {
		return errors.New("the update has no checksum")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.URL, nil)
	if err != nil {
		return err
	}
	resp, err := e.do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := path + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	h := sha256.New()
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 256*1024)
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return err
			}
			h.Write(buf[:n])
			done += int64(n)
			if total > 0 && time.Since(last) > 200*time.Millisecond {
				last = time.Now()
				p := float64(done) / float64(total)
				e.setUpdateProgress(&p, false, "")
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, u.SHA256) {
		_ = os.Remove(tmp)
		return fmt.Errorf("checksum mismatch: %s", got)
	}
	return os.Rename(tmp, path)
}

func fileHasHash(path, want string) (bool, error) {
	if want == "" {
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want), nil
}

// PrepareInstall is called right before the window starts the installer: when the VPN is on, the
// service turns it back on after the update.
func (e *Engine) PrepareInstall() {
	if e.connected() {
		writeMarker(e.opt.DataDir, reconnectMarker)
	}
}

const reconnectMarker = "reconnect-after-update"

func writeMarker(dir, name string) {
	_ = os.WriteFile(filepath.Join(dir, name), []byte(time.Now().Format(time.RFC3339)), 0o600)
}

// consumeMarker reports whether the marker was there (and fresh), removing it.
func consumeMarker(dir, name string) bool {
	path := filepath.Join(dir, name)
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	_ = os.Remove(path)
	return time.Since(st.ModTime()) < 30*time.Minute
}
