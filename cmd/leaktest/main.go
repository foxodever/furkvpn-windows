// leaktest is a developer tool: it runs what the service does with Xray over and over and prints
// the goroutines and the heap after each round, to see what is left behind. Three workloads: the
// ping button on a subscription file (-sub), one of its configs as the VPN with a socks inbound
// in place of the TUN (-vpn name), and an auto-select over servers that accept the connection and
// never answer (-tarpit). Goroutine and heap profiles go to -out.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strings"
	"sync"
	"time"

	"furkvpn/internal/core"
)

func rss() int {
	b, _ := os.ReadFile("/proc/self/status")
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmRSS:") {
			var kb int
			fmt.Sscanf(strings.TrimPrefix(l, "VmRSS:"), "%d", &kb)
			return kb / 1024
		}
	}
	return 0
}

func report(what string) {
	runtime.GC()
	runtime.GC()
	debug.FreeOSMemory()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("%-22s goroutines %5d  heapAlloc %6.1f MB  heapObjects %8d  sys %6.1f MB  rss %4d MB\n",
		what, runtime.NumGoroutine(), float64(m.HeapAlloc)/1e6, m.HeapObjects, float64(m.Sys)/1e6, rss())
}

func dump(dir, name string) {
	f, _ := os.Create(dir + "/" + name + ".goroutines.txt")
	_ = pprof.Lookup("goroutine").WriteTo(f, 1)
	f.Close()
	f, _ = os.Create(dir + "/" + name + ".heap.pprof")
	_ = pprof.Lookup("heap").WriteTo(f, 0)
	f.Close()
}

func main() {
	file := flag.String("sub", "", "subscription file")
	rounds := flag.Int("rounds", 6, "ping-all rounds")
	out := flag.String("out", ".", "where to write profiles")
	vpn := flag.String("vpn", "", "run the config with this in its name as the VPN instead of pinging")
	minutes := flag.Int("minutes", 3, "how long to run the VPN")
	tarpit := flag.Bool("tarpit", false, "run an auto-select over servers that never answer the handshake")
	flag.Parse()
	if *tarpit {
		runTarpit(*out, *minutes)
		return
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		panic(err)
	}
	var configs []json.RawMessage
	if err := json.Unmarshal(raw, &configs); err != nil {
		panic(err)
	}
	if *vpn != "" {
		runVPN(configs, *vpn, *minutes, *out)
		return
	}
	report("start")
	for r := 1; r <= *rounds; r++ {
		var wg sync.WaitGroup
		limit := make(chan struct{}, 6)
		ok := 0
		var mu sync.Mutex
		for _, c := range configs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				limit <- struct{}{}
				defer func() { <-limit }()
				if ms, err := core.MeasureDelay(c, "https://www.gstatic.com/generate_204", 5*time.Second); err == nil && ms >= 0 {
					mu.Lock()
					ok++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		time.Sleep(2 * time.Second)
		report(fmt.Sprintf("ping round %d (%d ok)", r, ok))
		if r == 1 {
			dump(*out, "round1")
		}
	}
	for _, wait := range []int{30, 60, 90, 150, 330} {
		time.Sleep(time.Duration(wait) * time.Second)
		report(fmt.Sprintf("+%d s", wait))
	}
	dump(*out, "end")
}
