// pingtest is a developer tool: it pings every config of an Xray JSON subscription file the way the
// app's ping button does and prints the delays.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"furkvpn/internal/core"
)

func main() {
	file := flag.String("sub", "", "Xray JSON subscription file (array of configs)")
	url := flag.String("url", "https://www.gstatic.com/generate_204", "test URL")
	kind := flag.String("type", "proxy", "proxy, tcp or icmp")
	timeout := flag.Duration("timeout", 5*time.Second, "timeout")
	flag.Parse()
	raw, err := os.ReadFile(*file)
	if err != nil {
		panic(err)
	}
	var configs []json.RawMessage
	if err := json.Unmarshal(raw, &configs); err != nil {
		panic(err)
	}
	results := make([]string, len(configs))
	var wg sync.WaitGroup
	limit := make(chan struct{}, 6)
	for i, c := range configs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			var remarks struct{ Remarks string }
			_ = json.Unmarshal(c, &remarks)
			started := time.Now()
			ms, err := core.MeasureDelayBy(core.Method(*kind), c, *url, *timeout)
			results[i] = fmt.Sprintf("%-44s %6d ms  (took %v) %v", remarks.Remarks, ms, time.Since(started).Round(time.Millisecond), errOrEmpty(err))
		}()
	}
	wg.Wait()
	for _, r := range results {
		fmt.Println(r)
	}
}

func errOrEmpty(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
