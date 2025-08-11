package micantainer

import (
	"github.com/vishvananda/netlink"
)

type NetworkInterface struct {
	Name string
	MacAddress string
	Addrs []netlink.Addr
}

type TapInterface struct {
	ID string
	Name string
}

type NetworkStats struct {
	Name string `json:"name,omitempty"`
	RxBytes uint64 `json:"rx_bytes,omitempty"`
	RxPackets uint64 `json:"rx_packets,omitempty"`
	RxErrors uint64 `json:"rx_errors,omitempty"`
	RxDropped uint64 `json:"rx_dropped,omitempty"`
	TxBytes uint64 `json:"tx_bytes,omitempty"`
	TxPackets uint64 `json:"tx_packets,omitempty"`
	TxErrors uint64 `json:"tx_errors,omitempty"`
	TxDropped uint64 `json:"tx_dropped,omitempty"`
}

type NetworkConfig struct {
	NetworkID         string
	NetworkCreated    bool
}

