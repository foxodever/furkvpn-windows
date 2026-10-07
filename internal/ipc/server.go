package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"sync"
	"sync/atomic"
)

// MaxMessage is the largest request accepted: settings files are up to 5 MB, escaped in JSON.
const MaxMessage = 16 << 20

// Handler serves the requests of the windows connected to the service.
type Handler interface {
	Handle(ctx context.Context, c *Conn, method string, params json.RawMessage) (any, error)
	Connected(c *Conn)
	Disconnected(c *Conn)
}

// Server accepts windows.
type Server struct {
	listener net.Listener
	handler  Handler
	mu       sync.Mutex
	conns    map[*Conn]struct{}
}

// NewServer serves handler on listener.
func NewServer(listener net.Listener, handler Handler) *Server {
	return &Server{listener: listener, handler: handler, conns: map[*Conn]struct{}{}}
}

// Serve accepts connections until the listener is closed.
func (s *Server) Serve(ctx context.Context) error {
	for {
		nc, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		c := &Conn{nc: nc, enc: json.NewEncoder(nc)}
		c.enc.SetEscapeHTML(false)
		s.mu.Lock()
		s.conns[c] = struct{}{}
		s.mu.Unlock()
		go s.serveConn(ctx, c)
	}
}

// Close stops accepting and drops every window.
func (s *Server) Close() {
	_ = s.listener.Close()
	s.mu.Lock()
	for c := range s.conns {
		_ = c.nc.Close()
	}
	s.mu.Unlock()
}

// Broadcast sends an event to every window.
func (s *Server) Broadcast(event string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	s.mu.Lock()
	conns := make([]*Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		c.sendRaw(event, raw)
	}
}

// Count is the number of connected windows.
func (s *Server) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

func (s *Server) serveConn(ctx context.Context, c *Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		_ = c.nc.Close()
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
		s.handler.Disconnected(c)
	}()
	s.handler.Connected(c)
	scanner := bufio.NewScanner(c.nc)
	scanner.Buffer(make([]byte, 64*1024), MaxMessage)
	// A window has no business sending many requests at once; this keeps a broken one in check.
	slots := make(chan struct{}, 32)
	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			c.respond(Response{Error: Errorf("bad_request", err.Error())})
			continue
		}
		slots <- struct{}{}
		go func() {
			defer func() { <-slots }()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("ipc: %s panicked: %v", req.Method, r)
					c.respond(Response{ID: req.ID, Error: Errorf("internal", "internal error")})
				}
			}()
			result, err := s.handler.Handle(ctx, c, req.Method, req.Params)
			resp := Response{ID: req.ID}
			if err != nil {
				var e *Error
				if !errors.As(err, &e) {
					e = Errorf("failed", err.Error())
				}
				resp.Error = e
			} else if result != nil {
				raw, err := json.Marshal(result)
				if err != nil {
					resp.Error = Errorf("internal", err.Error())
				} else {
					resp.Result = raw
				}
			}
			c.respond(resp)
		}()
	}
}

// Conn is one connected window.
type Conn struct {
	nc  net.Conn
	mu  sync.Mutex
	enc *json.Encoder
	// Values a handler keeps per window.
	Watching atomic.Bool
}

func (c *Conn) respond(r Response) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.enc.Encode(r)
}

// Send pushes an event to this window.
func (c *Conn) Send(event string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	c.sendRaw(event, raw)
}

func (c *Conn) sendRaw(event string, raw json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.enc.Encode(Event{Event: event, Data: raw})
}
