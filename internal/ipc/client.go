package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
)

// ErrClosed: the connection to the service is gone.
var ErrClosed = errors.New("service connection closed")

// Client is the window's side of the connection.
type Client struct {
	nc      net.Conn
	mu      sync.Mutex
	enc     *json.Encoder
	nextID  int64
	pending map[int64]chan incoming
	onEvent func(Event)
	done    chan struct{}
}

// NewClient starts reading from an established connection. onEvent runs on the reading goroutine.
func NewClient(nc net.Conn, onEvent func(Event)) *Client {
	c := &Client{nc: nc, enc: json.NewEncoder(nc), pending: map[int64]chan incoming{}, onEvent: onEvent, done: make(chan struct{})}
	c.enc.SetEscapeHTML(false)
	go c.read()
	return c
}

func (c *Client) read() {
	defer func() {
		c.mu.Lock()
		for id, ch := range c.pending {
			close(ch)
			delete(c.pending, id)
		}
		c.mu.Unlock()
		close(c.done)
	}()
	scanner := bufio.NewScanner(c.nc)
	scanner.Buffer(make([]byte, 64*1024), 4*MaxMessage)
	for scanner.Scan() {
		var msg incoming
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		if msg.Event != "" {
			if c.onEvent != nil {
				c.onEvent(Event{Event: msg.Event, Data: msg.Data})
			}
			continue
		}
		c.mu.Lock()
		ch := c.pending[msg.ID]
		delete(c.pending, msg.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
}

// Done is closed when the connection is lost.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close drops the connection.
func (c *Client) Close() error { return c.nc.Close() }

// CallRaw sends a request with already encoded params and waits for its result.
func (c *Client) CallRaw(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	ch := make(chan incoming, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return nil, ErrClosed
	default:
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	err := c.enc.Encode(Request{ID: id, Method: method, Params: params})
	c.mu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ErrClosed
	}
	select {
	case msg, ok := <-ch:
		if !ok {
			return nil, ErrClosed
		}
		if msg.Error != nil {
			return nil, msg.Error
		}
		return msg.Result, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Call sends a request and decodes its result into result (when not nil).
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = b
	}
	out, err := c.CallRaw(ctx, method, raw)
	if err != nil {
		return err
	}
	if result != nil && len(out) > 0 {
		return json.Unmarshal(out, result)
	}
	return nil
}
