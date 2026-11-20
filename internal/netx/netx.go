// Package netx knows the computer's real network (not the VPN's adapter): which one the system
// uses, when it changes, its addresses on the local network, and how to send a connection
// through it past the VPN.
package netx

// Kind of a network.
type Kind string

const (
	WiFi     Kind = "wifi"
	Ethernet Kind = "ethernet"
	Mobile   Kind = "mobile"
	Hotspot  Kind = "hotspot"
	Other    Kind = "other"
	None     Kind = "none"
)

// State of the network the system uses; Key changes when the network or its address changes.
type State struct {
	Key   string `json:"key"`
	Kind  Kind   `json:"kind"`
	Index int    `json:"index"`
	Name  string `json:"name"`
	// Its lasting addresses.
	addresses []string
}

// LANAddress is an address of this computer that other devices can reach.
type LANAddress struct {
	IP   string `json:"ip"`
	Kind Kind   `json:"kind"`
	Name string `json:"name"`
}

// TunAlias is the name of the VPN's own adapter, which never counts as the real network.
const TunAlias = "FurkVPN"
