//go:build !windows

package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type echo struct{ connected chan *Conn }

func (h *echo) Handle(ctx context.Context, c *Conn, method string, params json.RawMessage) (any, error) {
	switch method {
	case "echo":
		var v any
		_ = json.Unmarshal(params, &v)
		return v, nil
	case "fail":
		return nil, Errorf("not_found", "nothing")
	}
	return nil, nil
}
func (h *echo) Connected(c *Conn) {
	c.Send("state", map[string]int{"n": 1})
	h.connected <- c
}
func (h *echo) Disconnected(c *Conn) {}

func TestRoundTrip(t *testing.T) {
	t.Setenv("FURKVPN_SOCKET", filepath.Join(t.TempDir(), "s.sock"))
	l, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	h := &echo{connected: make(chan *Conn, 1)}
	s := NewServer(l, h)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Serve(ctx)
	defer s.Close()

	events := make(chan Event, 4)
	conn, err := Dial(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	c := NewClient(conn, func(e Event) { events <- e })
	defer c.Close()
	<-h.connected
	select {
	case e := <-events:
		if e.Event != "state" || string(e.Data) != `{"n":1}` {
			t.Fatalf("%+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no first state")
	}
	var out map[string]string
	if err := c.Call(ctx, "echo", map[string]string{"привет": "мир"}, &out); err != nil || out["привет"] != "мир" {
		t.Fatalf("%v %v", out, err)
	}
	err = c.Call(ctx, "fail", nil, nil)
	var ie *Error
	if !errors.As(err, &ie) || ie.Code != "not_found" {
		t.Fatalf("%v", err)
	}
	s.Broadcast("message", map[string]string{"code": "x"})
	select {
	case e := <-events:
		if e.Event != "message" {
			t.Fatalf("%+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no broadcast")
	}
	if s.Count() != 1 {
		t.Fatalf("count %d", s.Count())
	}
	s.Close()
	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("client did not notice the server going away")
	}
	if err := c.Call(context.Background(), "echo", 1, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("%v", err)
	}
}
