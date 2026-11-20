// devbridge serves the app's interface to an ordinary browser during development: requests go to
// the development service (cmd/furkvpn-service on Linux), and what FurkVPN.exe does itself
// (clipboard, files, programs) is imitated. Not part of the Windows build.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"furkvpn/internal/ipc"
)

type hub struct {
	mu    sync.Mutex
	subs  map[chan []byte]struct{}
	state map[string]json.RawMessage
}

func (h *hub) publish(event string, data json.RawMessage) {
	if event == "state" {
		var parts map[string]json.RawMessage
		if json.Unmarshal(data, &parts) == nil {
			h.mu.Lock()
			for k, v := range parts {
				h.state[k] = v
			}
			h.mu.Unlock()
		}
	}
	msg, _ := json.Marshal(map[string]any{"event": event, "data": data})
	h.mu.Lock()
	for ch := range h.subs {
		select {
		case ch <- msg:
		default:
		}
	}
	h.mu.Unlock()
}

func main() {
	addr := flag.String("addr", "127.0.0.1:34115", "listen address")
	dist := flag.String("dist", "frontend/dist", "built interface")
	clipboard := flag.String("clipboard", "", "what the imitated clipboard holds")
	flag.Parse()

	h := &hub{subs: map[chan []byte]struct{}{}, state: map[string]json.RawMessage{}}
	var client *ipc.Client
	var clientMu sync.Mutex
	connect := func() *ipc.Client {
		clientMu.Lock()
		defer clientMu.Unlock()
		if client != nil {
			select {
			case <-client.Done():
				client = nil
			default:
				return client
			}
		}
		conn, err := ipc.Dial(context.Background(), "")
		if err != nil {
			return nil
		}
		client = ipc.NewClient(conn, func(e ipc.Event) { h.publish(e.Event, e.Data) })
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = client.Call(ctx, "hello", nil, nil)
		}()
		online, _ := json.Marshal(map[string]bool{"online": true})
		h.publish("service", online)
		return client
	}
	go func() {
		for {
			c := connect()
			if c == nil {
				offline, _ := json.Marshal(map[string]bool{"online": false})
				h.publish("service", offline)
				time.Sleep(time.Second)
				continue
			}
			<-c.Done()
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(*dist)))
	mux.HandleFunc("/bridge/call", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		c := connect()
		if c == nil {
			e, _ := json.Marshal(ipc.Errorf("offline", "service not running"))
			http.Error(w, string(e), 503)
			return
		}
		params := req.Params
		if string(params) == "null" {
			params = nil
		}
		out, err := c.CallRaw(r.Context(), req.Method, params)
		if err != nil {
			e, _ := json.Marshal(map[string]string{"code": codeOf(err), "message": err.Error()})
			http.Error(w, string(e), 500)
			return
		}
		w.Write(out)
	})
	mux.HandleFunc("/bridge/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		ch := make(chan []byte, 64)
		h.mu.Lock()
		h.subs[ch] = struct{}{}
		full, _ := json.Marshal(h.state)
		h.mu.Unlock()
		defer func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.mu.Unlock()
		}()
		first, _ := json.Marshal(map[string]any{"event": "state", "data": json.RawMessage(full)})
		fmt.Fprintf(w, "data: %s\n\n", first)
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case msg := <-ch:
				fmt.Fprintf(w, "data: %s\n\n", msg)
				flusher.Flush()
			}
		}
	})
	mux.HandleFunc("/bridge/sys/", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path[len("/bridge/sys/"):]
		body, _ := io.ReadAll(r.Body)
		log.Printf("sys %s %s", name, truncate(string(body), 120))
		var out any
		switch name {
		case "Language":
			out = envOr("FURKVPN_LANG", "ru-RU")
		case "ReadClipboard":
			out = *clipboard
		case "Autostart":
			out = true
		case "Theme":
			out = "system"
		case "Programs":
			out = []map[string]any{
				{"name": "Telegram.exe", "title": "Telegram Desktop", "path": `C:\Users\Тест\AppData\Roaming\Telegram Desktop\Telegram.exe`, "running": true},
				{"name": "chrome.exe", "title": "Google Chrome", "path": `C:\Program Files\Google\Chrome\Application\chrome.exe`, "running": true},
				{"name": "steam.exe", "title": "Steam", "path": `C:\Program Files (x86)\Steam\steam.exe`, "running": false},
				{"name": "Discord.exe", "title": "Discord", "path": `C:\Users\Тест\AppData\Local\Discord\app-1.0.9200\Discord.exe`, "running": false},
			}
		case "ProgramIcons":
			out = map[string]string{}
		case "PickProgram", "OpenTextFile":
			out = nil
		case "SaveTextFile":
			out = true
		}
		json.NewEncoder(w).Encode(out)
	})
	log.Printf("dev bridge on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}

func codeOf(err error) string {
	if e, ok := err.(*ipc.Error); ok {
		return e.Code
	}
	return "failed"
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
