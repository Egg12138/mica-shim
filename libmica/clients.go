// TODO: using containerd socket utils
package libmica

import (
	"encoding/binary"
	"errors"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"net"
	"strings"
	"time"
)

// NOTICE: we have to ensure the length of each field consistency with the length of the field in mica daemon
// TODO: add explaination for each field
type MicaCreateMsg struct {
	CPU	 					uint32
	Name 					[32]byte
	// relative path in bundle
	Path					[128]byte
	Ped 					[32]byte
	PedCfg  			[128]byte
	Debug 				bool
}

func (m *MicaCreateMsg) Pack() []byte {
	buf := make([]byte, 4+32+128+32+128+1) // Total: 325 bytes
	
	binary.LittleEndian.PutUint32(buf[0:4], m.CPU)
	copy(buf[4:36], m.Name[:])
	copy(buf[36:164], m.Path[:])
	copy(buf[164:196], m.Ped[:])
	copy(buf[196:324], m.PedCfg[:])
	
	if m.Debug {
		buf[324] = 1
	} else {
		buf[324] = 0
	}
	
	return buf
}

// TODO: seperate into mick_socket.go

// MicaSocket handles Unix domain socket communication with mica daemon
type MicaSocket struct {
	socketPath string
	conn       net.Conn
}

func NewMicaSocket() *MicaSocket {
	// socketPath := defs.MicaSocketPath
	// conn, err := net.Dial("unix", socketPath)
	// if err != nil {
	// 	return nil, err
	// }
	// return &MicaSocket{
	// 	socketPath: socketPath,
	// 	conn:       conn,
	// }, nil
	log.Debug("Creating new MicaSocket")
	return &MicaSocket{socketPath: defs.MicaSocketPath}
}

func (ms *MicaSocket) Connect() error {
	log.Debug("Connecting to MicaSocket")
	conn, err := net.Dial("unix", ms.socketPath)
	if err != nil {
		log.Error("Failed to connect to MicaSocket", "error", err)
		return err
	}
	ms.conn = conn
	return nil
}

func (ms *MicaSocket) Close() error {
	if ms.conn != nil {
		return ms.conn.Close()
	}
	return nil
}

func (ms *MicaSocket) SendMsg(data []byte) error {
	log.Debugf("Sending message to MicaSocket: %s", string(data))
	if ms.conn == nil {
		return errors.New("socket not connected")
	}
	_, err := ms.conn.Write(data)
	return err
}

func (ms *MicaSocket) Recv() (string, error) {
	log.LocateDebug("Receiving message from MicaSocket")
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
		
		if strings.Contains(responseBuffer, "MICA-FAILED") {
			parts := strings.Split(responseBuffer, "MICA-FAILED")
			msg := strings.TrimSpace(parts[0])
			if msg != "" {
				log.Error(msg)
			}
			return "MICA-FAILED", nil
		} else if strings.Contains(responseBuffer, "MICA-SUCCESS") {
			parts := strings.Split(responseBuffer, "MICA-SUCCESS")
			msg := strings.TrimSpace(parts[0])
			if msg != "" {
				log.Info(msg)
			}
			return "MICA-SUCCESS", nil
		}
	}
	
	return "", errors.New("unexpected response format")
}

func NewMicaClient() (string, error) {
	return "", nil
}

func CreateMicaClient() {}