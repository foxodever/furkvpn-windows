package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"furkvpn/internal/ipc"
	"furkvpn/internal/keys"
)

// Adding this computer's key to a TV with FurkVPN: the TV shows a code (and furkapp.net/tv/<code>
// as a QR code) and waits; the key sent for that code goes to it through furkapp.net (API
// /api/v1/tv/send, see the Android app's TvPairing).

// SendTVKey sends the key to the TV with code (as typed off its screen or taken from its link).
func (e *Engine) SendTVKey(code string) error {
	code = keys.NormalizeTVCode(code)
	if code == "" {
		return ipc.Errorf("tv_bad_code", "")
	}
	info := e.subs.Info()
	if info == nil {
		return ipc.Errorf("tv_need_key", "")
	}
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"code": code, "key": info.URL})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, APIBase+"/api/v1/tv/send", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.do(ctx, req)
	if err != nil {
		return ipc.Errorf("tv_failed", err.Error())
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		e.log.Print("key sent to a TV")
		return nil
	case http.StatusNotFound:
		return ipc.Errorf("tv_no_code", "")
	case http.StatusBadRequest:
		return ipc.Errorf("foreign_key", "")
	case http.StatusTooManyRequests:
		return ipc.Errorf("tv_too_many", "")
	}
	return ipc.Errorf("tv_failed", resp.Status)
}
