package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
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
	status, _, err := e.postAPI(ctx, "/api/v1/tv/send", map[string]string{"code": code, "key": info.URL})
	if err != nil {
		return ipc.Errorf("tv_failed", err.Error())
	}
	switch status {
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
	return ipc.Errorf("tv_failed", http.StatusText(status))
}

// The other way round, signing in on this computer from a phone: the computer plays the TV's
// part. It asks furkapp.net for a code (/api/v1/tv/session), the window shows it and its link as
// a QR code, and the phone sends its key for that code as it does for a TV: "Подключить
// телевизор" in the FurkVPN app, or the page the QR code opens. The window waits for the key
// (/api/v1/tv/wait) and submits it like a pasted one.

// TVSession is a code of this computer for a phone to send its key to.
type TVSession struct {
	Code string `json:"code"`
	// Token is what only the one waiting for the key knows.
	Token string `json:"token"`
	// URL is the link the QR code holds: furkapp.net/tv/<code>.
	URL string `json:"url"`
	// ExpiresIn is how long the code lives, in seconds.
	ExpiresIn int `json:"expiresIn"`
}

// NewTVSession asks furkapp.net for a code.
func (e *Engine) NewTVSession(ctx context.Context) (*TVSession, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status, body, err := e.postAPI(ctx, "/api/v1/tv/session", struct{}{})
	if err != nil {
		return nil, ipc.Errorf("tv_failed", err.Error())
	}
	if status == http.StatusTooManyRequests {
		return nil, ipc.Errorf("tv_too_many", "")
	}
	var session TVSession
	if status != http.StatusOK || json.Unmarshal(body, &session) != nil {
		return nil, ipc.Errorf("tv_failed", http.StatusText(status))
	}
	if session.Code = keys.NormalizeTVCode(session.Code); session.Code == "" || session.Token == "" {
		return nil, ipc.Errorf("tv_failed", "no code in the answer")
	}
	if link, err := url.Parse(session.URL); err != nil || link.Host == "" || (link.Scheme != "https" && link.Scheme != "http") {
		session.URL = APIBase + "/tv/" + session.Code
	}
	if session.ExpiresIn <= 0 {
		session.ExpiresIn = 900
	}
	return &session, nil
}

// WaitTVKey waits for the key a phone sends for the code: the server holds the request for about
// 10 seconds. It returns the key, or "" when none has come yet (ask again); the error "tv_expired"
// means the code ran out or the server forgot it, and a new one is needed.
func (e *Engine) WaitTVKey(ctx context.Context, code, token string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status, body, err := e.postAPI(ctx, "/api/v1/tv/wait", map[string]string{"code": code, "token": token})
	if err != nil {
		return "", ipc.Errorf("tv_failed", err.Error())
	}
	switch status {
	case http.StatusOK:
		var answer struct{ Status, Key string }
		if json.Unmarshal(body, &answer) != nil {
			return "", ipc.Errorf("tv_failed", "bad answer")
		}
		if answer.Status != "ready" || answer.Key == "" {
			return "", nil
		}
		e.log.Print("key received from a phone")
		return answer.Key, nil
	case http.StatusNotFound:
		return "", ipc.Errorf("tv_expired", "")
	case http.StatusTooManyRequests:
		return "", ipc.Errorf("tv_too_many", "")
	}
	return "", ipc.Errorf("tv_failed", http.StatusText(status))
}

// postAPI sends a JSON request to furkapp.net and returns the status and the answer.
func (e *Engine) postAPI(ctx context.Context, path string, payload any) (int, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, APIBase+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.do(ctx, req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, answer, nil
}
