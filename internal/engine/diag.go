package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
)

// Memory tells what the service's process holds, for "the app eats memory" reports: the request
// "memory" on the pipe. With profiles it also writes the goroutines and the heap to the logs
// folder (goroutines.txt and heap.pprof, for go tool pprof).
func (e *Engine) Memory(profiles bool) map[string]any {
	runtime.GC()
	debug.FreeOSMemory()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	out := map[string]any{
		"goroutines":  runtime.NumGoroutine(),
		"heapAlloc":   m.HeapAlloc,
		"heapObjects": m.HeapObjects,
		"heapInuse":   m.HeapInuse,
		"stackInuse":  m.StackInuse,
		"sys":         m.Sys,
	}
	if profiles {
		dir := filepath.Join(e.opt.DataDir, "logs")
		_ = os.MkdirAll(dir, 0o700)
		for name, debugLevel := range map[string]int{"goroutine": 1, "heap": 0} {
			file := "heap.pprof"
			if name == "goroutine" {
				file = "goroutines.txt"
			}
			if f, err := os.Create(filepath.Join(dir, file)); err == nil {
				_ = pprof.Lookup(name).WriteTo(f, debugLevel)
				_ = f.Close()
			}
		}
		out["profiles"] = dir
	}
	return out
}
