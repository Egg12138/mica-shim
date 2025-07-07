// TODO: using containerd socket utils
package libmica

import (
	"encoding/binary"
	"fmt"
	"mica-shim/cntr"
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
	// scheduled
	cpu  uint32
	// assigned by containerd
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

// 1. search bundle/.../<clientOSname>.elf
// 2. if missing, log and search for binary in bundle recursively
// TALK: 这是预留的核，实际client可能更后面启动, 以及启动可能失败
// TODO: 现在我们全部假定是单核RTOS, mica侧还未实现多核, 但是在镜像label中，我们可以指定核数量
func CreateConf(name string, bundle string) (micaCreateMsg, error) {
	// TODO: use a universal way to get all LABELS && Files under bundle
	ncpu := getNCPU(bundle)
	if ncpu > 1 {
		log.Debugf("expected using %d cores", ncpu)
	}
	// TODO: cpu id should be scheduled by mica-shim
	cpu := schedFreeCPU()

	info, err := cntr.ContainerInfoParse(bundle)
	if err != nil {
		log.Errorf("failed to get container info: %v", err)
		return micaCreateMsg{}, err
	}

	firmware := info.FirmwarePath()
	pedestal := info.Ped()
	
	conf := micaCreateMsg{}
	conf.init(cpu, name, firmware, pedestal.PedestalType.String(), pedestal.PedestalConf, false)
	return conf, nil
}

// NewMicaCreateMsg creates a properly initialized micaCreateMsg
func NewMicaCreateMsg(cpu uint32, name string, path string, ped string, pedCfg string, debug bool) micaCreateMsg {
	msg := micaCreateMsg{}
	msg.init(cpu, name, path, ped, pedCfg, debug)
	return msg
}

func Create(conf micaCreateMsg) (string, error) {
	s := newMicaSocket(defs.MicaCreatSocketPath)
	// we do not deref s hengre, because it is dropped in handleMsg()
	msg := conf.pack()
	return s.handleMsg(msg)
}

func Start(conf micaCreateMsg) (string, error) {
	// client := "qemu-zephyr"
	return MicaCtl(MStart, string(conf.name[:]))
}

func Stop(conf micaCreateMsg) (string, error) {
	return MicaCtl(MStop, string(conf.name[:]))
}

func Remove(conf micaCreateMsg) (string, error) {
	return MicaCtl(MRemove, string(conf.name[:]))
}

// TODO: check status of specific client os is not implemented yet.
func clientStatus(conf micaCreateMsg) (string, error) {
	return MicaCtl(MStatus, string(conf.name[:]))
}

func ClientsStatus() (string, error) {
	return MicaCtl(MStatus, "")
}
