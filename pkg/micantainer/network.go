package micantainer

import (
	"fmt"
	log "mica-shim/logger"

	"github.com/containerd/errdefs"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)


type NetworkInterface struct {
	Name       string
	MacAddress string
	Addrs      []netlink.Addr
	IP				 string
	DefaultGW  string
}

type TapInfo struct {
	ID   string
	Name string
	MTU  int
}

const (
	DefaultHostIfName  = "eth0"
	DefaultHostTapName = "tap1"
	DefaultMTU         = 1500
)

type NetworkStats struct {
	Name      string `json:"name,omitempty"`
	RxBytes   uint64 `json:"rx_bytes,omitempty"`
	RxPackets uint64 `json:"rx_packets,omitempty"`
	RxErrors  uint64 `json:"rx_errors,omitempty"`
	RxDropped uint64 `json:"rx_dropped,omitempty"`
	TxBytes   uint64 `json:"tx_bytes,omitempty"`
	TxPackets uint64 `json:"tx_packets,omitempty"`
	TxErrors  uint64 `json:"tx_errors,omitempty"`
	TxDropped uint64 `json:"tx_dropped,omitempty"`
}

type NetworkConfig struct {
	NetworkID      string
	NetworkCreated bool
}


type Network interface {
	NetworkIsCreated() bool 
	NetID() string
	NetworkCleanup() error
}


// TODO: analyse how to setup netdevice in zephyr, rtthread via mica-Xen
func NetworkSetup(id string, ipAddr string, config NetworkConfig, spec specs.Spec) (netlink.Link, error) {
	uid, gid := spec.Process.User.UID, spec.Process.User.GID
	tapDevice, err := createTapDevice(newTap(id, DefaultHostTapName, DefaultMTU), uid, gid)
	if err != nil {
		return nil, err
	}	

	ingressDummy()
	ipn, err := netlink.ParseAddr(ipAddr)
	if err != nil {
		return nil, err
	}
	err = netlink.AddrReplace(tapDevice, ipn)
	if err != nil {
		return nil, err
	}
	err = netlink.LinkSetUp(tapDevice)
	if err != nil {
		return nil, err
	}
	return tapDevice, nil
}

func NetDeviceCleanup(id uint32) error {
	return nil
}

func newTap(id string, name string, mtu int) TapInfo {
	return TapInfo{
		ID:   id,
		Name: name,
		MTU: mtu,
	}
}

// dummy codes
func createTapDevice(tap TapInfo, uid, gid uint32) (netlink.Link, error) {
	tapLink := &netlink.Tuntap{
		LinkAttrs: netlink.LinkAttrs{
			TxQLen: 1000,
			MTU: tap.MTU,
			Name: tap.Name,
		},
		Mode: netlink.TUNTAP_MODE_TAP,
		Flags: netlink.TUNTAP_DEFAULTS,
	}
	err := netlink.LinkAdd(tapLink)
	if err != nil {
		return nil, fmt.Errorf("failed to create tap device: %w", err)
	}

	name := tap.Name
	mtu := tap.MTU

	for _, tapFd := range tapLink.Fds {
		err = unix.IoctlSetInt(int(tapFd.Fd()), unix.TUNSETOWNER, int(uid))
		if err != nil {
			return nil, fmt.Errorf("failed to set tap %s owner to uid %d: %w", name, uid, err)
		}

		err = unix.IoctlSetInt(int(tapFd.Fd()), unix.TUNSETGROUP, int(gid))
		if err != nil {
			return nil, fmt.Errorf("failed to set tap %s group to gid %d: %w", name, gid, err)
		}
	}

		err = netlink.LinkSetMTU(tapLink, mtu)
		if err != nil {
			return nil, fmt.Errorf("failed to set tap device MTU to %d: %w", mtu, err)
		}

	return tapLink, nil

	
}

func addTapDevice(device netlink.Link) error {
	return errdefs.ErrNotImplemented
}

func delTapDevice(device netlink.Link) error {
	return errdefs.ErrNotImplemented
}



func ingressDummy() {
	log.Debugf("ingress related managements..")
}