// TODO: using containerd socket utils
package libmica

import (
	"encoding/binary"
	"errors"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type MicaCommand string

const (
	MCreate MicaCommand = "create"
	MStart  MicaCommand = "start"
	MStop   MicaCommand = "stop"
	MRemove MicaCommand = "remove"
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

// TODO: seperate into mick_socket.go

// micaSocket handles Unix domain socket communication with mica daemon
type micaSocket struct {
	socketPath string
	conn       net.Conn
}

func validSocketPath(socketPath string) bool {
	if st, err := os.Stat(socketPath); err != nil {
		return false
	} else {
		return st.Mode()&os.ModeSocket != 0
	}
}

func newMicaSocket(socketPath string) *micaSocket {
	log.Debug("Creating new MicaSocket")
	return &micaSocket{socketPath: socketPath}
}

func (ms *micaSocket) connect() error {
	log.Debug("Connecting to MicaSocket")
	conn, err := net.Dial("unix", ms.socketPath)
	if err != nil {
		log.Error("Failed to connect to MicaSocket", "error: ", err)
		return err
	}
	ms.conn = conn
	return nil
}

func (ms *micaSocket) close() error {
	if ms.conn != nil {
		return ms.conn.Close()
	}
	return nil
}

func (ms *micaSocket) tx(data []byte) error {
	log.LocateDebugf("Sending message to MicaSocket: %s", string(data))
	if ms.conn == nil {
		return errors.New("socket not connected")
	}
	_, err := ms.conn.Write(data)
	return err
}

func (ms *micaSocket) rx() (string, error) {
	log.LocateDebugf("Receiving message from MicaSocket")
	if ms.conn == nil {
		return "", errors.New("socket not connected")
	}

	ms.conn.SetReadDeadline(time.Now().Add(defs.MicaSocketTimout))

	responseBuffer := ""
	buf := make([]byte, defs.MicaSocketBufSize)

	for {
		n, err := ms.conn.Read(buf)
		log.Debugf("Received %d bytes chunk from %s", n, ms.conn.RemoteAddr())
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return "", errors.New("timeout while waiting for micad response")
			}
			return "", err
		}

		if n == 0 {
			break
		}

		responseBuffer += string(buf[:n])
		log.Debugf("Complete Response buffer: %s", responseBuffer)

		if strings.Contains(responseBuffer, defs.MicaFailed) {
			parts := strings.Split(responseBuffer, defs.MicaFailed)
			msg := strings.TrimSpace(parts[0])
			if msg != "" {
				log.Error(msg)
			}
			return defs.MicaFailed, nil
		} else if strings.Contains(responseBuffer, defs.MicaSuccess) {
			parts := strings.Split(responseBuffer, defs.MicaSuccess)
			msg := strings.TrimSpace(parts[0])
			if msg != "" {
				log.Info(msg)
			}
			return defs.MicaSuccess, nil
		}
	}

	return "", errors.New("unexpected response format")
}

// TODO: We need to manually fetch information from managed clients
// Because mica daemon print clients information by its own format, which is not
// compatible with containerd
func (ms *micaSocket) handleMsg(msg []byte) (string, error) {
	log.LocateDebugf("Handling message with socket: %s", ms.socketPath)

	if err := ms.connect(); err != nil {
		return "", fmt.Errorf("failed to connect to socket: %v", err)
	}
	defer ms.close()

	if err := ms.tx(msg); err != nil {
		return "", fmt.Errorf("failed to send command: %v", err)
	}

	response, err := ms.rx()
	log.LocateDebugf("Received response: %s, error: %v", response, err)
	if err != nil {
		return "", fmt.Errorf("failed to receive response: %v", err)
	}

	switch response {
	case defs.MicaSuccess:
		log.LocateDebugf("Command executed successfully: %s", response)
		return response, nil
	case defs.MicaFailed:
		log.LocateDebugf("Command failed: %s", response)
		return response, fmt.Errorf("mica daemon reported failure")
	default:
		log.LocateDebugf("Received unexpected response: %s", response)
		return response, fmt.Errorf("unexpected response format: %s", response)
	}
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

func dummyCreateMsg() micaCreateMsg {
	return NewMicaCreateMsg(3, "qemu-zephyr",
		"/home/egg/source/mica-shim/tests/qemu-zephyr-rproc.conf",
		"", "", false)
}

// Public test functions:
func TestCreate() (string, error) {
	s := newMicaSocket(defs.MicaCreatSocketPath)
	defer s.close()
	s.connect()
	msg := dummyCreateMsg()
	return s.handleMsg(msg.pack())
}

func TestStart() (string, error) {
	client := "qemu-zephyr"
	return MicaCtl(MStart, client)
}

func TestStop() (string, error) {
	client := "qemu-zephyr"
	return MicaCtl(MStop, client)
}

func TestRemove() (string, error) {
	client := "qemu-zephyr"
	return MicaCtl(MRemove, client)
}

func TestStatus() (string, error) {
	client := "qemu-zephyr"
	return MicaCtl(MStatus, client)
}
