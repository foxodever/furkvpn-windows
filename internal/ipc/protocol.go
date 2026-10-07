// Package ipc connects the app's window (FurkVPN.exe, run by the user) with the service
// (FurkVPNService.exe, run by Windows as LocalSystem), which runs the VPN and keeps the data.
// Messages are JSON lines over a named pipe:
//
//	→ {"id":1,"method":"connect","params":{...}}
//	← {"id":1,"result":{...}}  or  {"id":1,"error":{"code":"...","message":"..."}}
//	← {"event":"state","data":{...}}   pushed by the service at any time
package ipc

import "encoding/json"

// PipeName is where the service listens.
const PipeName = `\\.\pipe\FurkVPN`

// Version of the protocol; the window and the service of one installation always match, this
// only guards against leftovers of an interrupted update.
const Version = 1

// Request from the window.
type Request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response to a request.
type Response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Event pushed by the service.
type Event struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// Error is a failed request: Code is for the app to pick a message in the user's language.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Code + ": " + e.Message
	}
	return e.Code
}

// Errorf makes an error with a code.
func Errorf(code, message string) *Error { return &Error{Code: code, Message: message} }

// incoming is what the window reads: a response or an event.
type incoming struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
	Event  string          `json:"event,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}
