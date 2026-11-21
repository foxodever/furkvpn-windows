// tuntest turns the VPN adapter on and off many times in a row, as the service does when it
// restarts the core, and tells which of the starts failed and with what. It needs no server: what
// goes into the adapter leaves directly. Run it as administrator, with the service stopped.
//
//	tuntest -n 50 -gap 0 -hold 300ms
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"furkvpn/internal/core"
	"furkvpn/internal/xrayconf"
)

const raw = `{"remarks":"tuntest",
 "outbounds":[{"tag":"proxy","protocol":"freedom"},{"tag":"direct","protocol":"freedom"}],
 "dns":{"servers":["1.1.1.1"]}}`

func main() {
	n := flag.Int("n", 20, "how many times to start")
	gap := flag.Duration("gap", 0, "pause between a stop and the next start")
	hold := flag.Duration("hold", 300*time.Millisecond, "how long the adapter stays up")
	ipv6 := flag.Bool("ipv6", true, "IPv6 goes into the adapter")
	strict := flag.Bool("strict", true, "DNS only through the adapter")
	die := flag.Bool("die", false, "exit with the adapter up after the last start, as a crash would")
	verbose := flag.Bool("v", false, "print Wintun's own messages")
	repair := flag.Bool("repair", false, "when the adapter does not come up, install its driver anew and start again, as the service does")
	flag.Parse()
	if *verbose {
		log.SetOutput(os.Stdout)
	} else {
		log.SetOutput(discard{})
	}
	config, err := xrayconf.Build([]byte(raw), xrayconf.Options{
		LogLevel: "warning",
		Tun:      &xrayconf.Tun{MTU: xrayconf.MTU, IPv6: *ipv6, StrictDNS: *strict},
	})
	if err != nil {
		fmt.Println("config:", err)
		os.Exit(2)
	}
	failed := 0
	for i := 1; i <= *n; i++ {
		began := time.Now()
		err := core.Start(config)
		if err != nil && *repair && core.AdapterFailed(err) {
			fmt.Printf("     the adapter did not come up (%v), installing its driver anew\n", err)
			if rerr := core.ReinstallAdapterDriver(); rerr != nil {
				fmt.Println("     driver:", rerr)
			} else {
				err = core.Start(config)
			}
		}
		took := time.Since(began).Round(time.Millisecond)
		if err != nil {
			failed++
			fmt.Printf("%3d  FAILED in %v: %v\n", i, took, err)
			time.Sleep(*gap)
			continue
		}
		fmt.Printf("%3d  up in %v\n", i, took)
		time.Sleep(*hold)
		if *die && i == *n {
			fmt.Println("exiting with the adapter up")
			os.Exit(0)
		}
		began = time.Now()
		_ = core.Stop()
		if took := time.Since(began); took > time.Second {
			fmt.Printf("     stop took %v\n", took.Round(time.Millisecond))
		}
		time.Sleep(*gap)
	}
	fmt.Printf("failed %d of %d\n", failed, *n)
	if failed > 0 {
		os.Exit(1)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
