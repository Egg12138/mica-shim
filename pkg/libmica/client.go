// TODO: using containerd socket utils
package libmica

import (
	"encoding/binary"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"path/filepath"
)

type MicaCommand string
type PedType int

const (
	// 0
	Baremetal PedType = iota
	Jailhouse
	Xen
)

const (
	MCreate MicaCommand = "create"
	MStart  MicaCommand = "start"
	MStop   MicaCommand = "stop"
	MRemove MicaCommand = "rm"
	MStatus MicaCommand = "status"
)

type McsFS struct {
	Source string `json:"source"`
	//
	Target  string   `json:"target"`
	Ped     PedType  `json:"ped"`
	OS      string   `json:"os"`
	Mounted bool     `json:"mounted"`
	Options []string `json:"options"`
}

// NOTICE: we have to ensure the length of each field consistency with the length of the field in mica daemon
// TODO: add explaination for each field
type MicaClientConf struct {
	// scheduled
	cpu uint32
	// assigned by containerd
	name [32]byte
	// relative path in bundle
	path   [128]byte
	ped    [32]byte
	pedcfg [128]byte
	debug  bool
}

func (m *MicaClientConf) Init(cpu uint32, name string, path string, ped string, pedCfg string, debug bool) {
	m.cpu = cpu
	copy(m.name[:], name)
	copy(m.path[:], path)
	copy(m.ped[:], ped)
	copy(m.pedcfg[:], pedCfg)
	m.debug = debug
}

func (m *MicaClientConf) pack() []byte {
	buf := make([]byte, 4+32+128+32+128+1) // Total: 325 bytes

	binary.LittleEndian.PutUint32(buf[0:4], m.cpu)
	copy(buf[4:36], m.name[:])
	copy(buf[36:164], m.path[:])
	copy(buf[164:196], m.ped[:])
	copy(buf[196:324], m.pedcfg[:])

	if m.debug {
		buf[324] = 1
	} else {
		buf[324] = 0
	}

	return buf
}

// Public functions:

// MicaCreate creates a new mica client; while MicaCtl is used to control the mica client
func MicaCreate(config MicaClientConf) (string, error) {
	s := newMicaSocket(defs.MicaCreatSocketPath)
	return s.handleMsg(config.pack())
}

func MicaCtl(cmd MicaCommand, client string) (string, error) {
	if !validSocketPath(defs.MicaCreatSocketPath) {
		log.Debug("mica socket directory does not exist, please check if micad is running")
		return "", fmt.Errorf("mica socket directory does not exist, please check if micad is running")
	}
	target := filepath.Join(defs.MicaSocketDir, client+".socket")
	log.Debugf("client socket path: %s", target)
	s := newMicaSocket(target)
	msg := string(cmd)
	return s.handleMsg([]byte(msg))
}

// NewMicaCreateMsg creates a properly initialized micaCreateMsg
func NewMicaCreateMsg(cpu uint32, name string, path string, ped string, pedCfg string, debug bool) MicaClientConf {
	msg := MicaClientConf{}
	msg.Init(cpu, name, path, ped, pedCfg, debug)
	return msg
}

func CreateMicaClient(conf MicaClientConf) (string, error) {
	log.Debugf("create mica client conf: %+v", conf)
	s := newMicaSocket(defs.MicaCreatSocketPath)
	// we do not deref s here, because it is dropped in handleMsg()
	msg := conf.pack()
	return s.handleMsg(msg)
}

func StartMicaClient(conf MicaClientConf) (string, error) {
	// client := "qemu-zephyr"
	return MicaCtl(MStart, string(conf.name[:]))
}

func Stop(conf MicaClientConf) (string, error) {
	return MicaCtl(MStop, string(conf.name[:]))
}

func Remove(conf MicaClientConf) (string, error) {
	return MicaCtl(MRemove, string(conf.name[:]))
}

// TODO: check status of specific client os is not implemented yet.
func clientStatus(conf MicaClientConf) (string, error) {
	return MicaCtl(MStatus, string(conf.name[:]))
}

func ClientsStatus() (string, error) {
	return MicaCtl(MStatus, "")
}
