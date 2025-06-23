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

const (
	MCreate MicaCommand = "create"
	MStart  MicaCommand = "start"
	MStop   MicaCommand = "stop"
	MRemove MicaCommand = "rm"
	MStatus MicaCommand = "status"
)

// NOTICE: we have to ensure the length of each field consistency with the length of the field in mica daemon
// TODO: add explaination for each field
type micaCreateMsg struct {
	cpu  uint32
	name [32]byte
	// relative path in bundle
	path   [128]byte
	ped    [32]byte
	pedcfg [128]byte
	debug  bool
}

func (m *micaCreateMsg) init(cpu uint32, name string, path string, ped string, pedCfg string, debug bool) {
	m.cpu = cpu
	copy(m.name[:], name)
	copy(m.path[:], path)
	copy(m.ped[:], ped)
	copy(m.pedcfg[:], pedCfg)
	m.debug = debug
}

func (m *micaCreateMsg) pack() []byte {
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
func MicaCreate(config micaCreateMsg) (string, error) {
	s := newMicaSocket(defs.MicaCreatSocketPath)
	return s.handleMsg(config.pack())
}

func MicaCtl(cmd MicaCommand, client string) (string, error) {
	if !validSocketPath(defs.MicaCreatSocketPath) {
		log.Debug("mica socket directory does not exist, please check if micad is running")
		return "", fmt.Errorf("mica socket directory does not exist, please check if micad is running")
	}
	target := filepath.Join(defs.MicaSocketDir, client+".socket")
	log.LocateDebugf("client socket path: %s", target)
	s := newMicaSocket(target)
	msg := string(cmd)
	return s.handleMsg([]byte(msg))
}

// NewMicaCreateMsg creates a properly initialized micaCreateMsg
func NewMicaCreateMsg(cpu uint32, name string, path string, ped string, pedCfg string, debug bool) micaCreateMsg {
	msg := micaCreateMsg{}
	msg.init(cpu, name, path, ped, pedCfg, debug)
	return msg
}


// dummy test functions:

const (
  dummyConfPath = "/lib/firmware/zephyr.elf"
  // dummyConfPath = "/home/egg/source/mica-shim/tests/qemu-zephyr-rproc.conf"
)

func dummyCreateMsg(id string) micaCreateMsg {
	return NewMicaCreateMsg(0, id,
		dummyConfPath,
		"", "", false)
}

func DummyCreate(id string) (string, error) {
	s := newMicaSocket(defs.MicaCreatSocketPath)
	// we do not deref s here, because it is dropped in handleMsg()
	msg := dummyCreateMsg(id)
	return s.handleMsg(msg.pack())
}

func DummyStart(id string) (string, error) {
	// client := "qemu-zephyr"
	return MicaCtl(MStart, id)
}

func DummyStop(id string) (string, error) {
	return MicaCtl(MStop, id)
}

func DummyRemove(id string) (string, error) {
	return MicaCtl(MRemove, id)
}

func TestStatus() (string, error) {
	client := "qemu-zephyr"
	return MicaCtl(MStatus, client)
}
