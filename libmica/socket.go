// TODO: using containerd socket utils
package libmica

import (
	"errors"
	"fmt"
	defs "mica-shim/definitions"
	log "mica-shim/logger"
	"net"
	"os"
	"strings"
	"time"
)

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
	log.FDebugf("Sending message to MicaSocket: %s", string(data))
	if ms.conn == nil {
		return errors.New("socket not connected")
	}
	_, err := ms.conn.Write(data)
	return err
}

func (ms *micaSocket) rx() (string, error) {
	log.FDebugf("Receiving message from MicaSocket")
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
	log.FDebugf("Handling message with socket: %s", ms.socketPath)

	if err := ms.connect(); err != nil {
		return "", fmt.Errorf("failed to connect to socket: %v", err)
	}
	defer func() {
		log.Debugf("Closing socket: %s\n", ms.socketPath)
		ms.close()
	}()

	if err := ms.tx(msg); err != nil {
		return "", fmt.Errorf("failed to send command: %v", err)
	}

	response, err := ms.rx()
	log.FDebugf("Received response: %s, error: %v", response, err)
	if err != nil {
		return "", fmt.Errorf("failed to receive response: %v", err)
	}

	switch response {
	case defs.MicaSuccess:
		log.FDebugf("Command executed successfully: %s", response)
		return response, nil
	case defs.MicaFailed:
		log.FDebugf("Command failed: %s", response)
		return response, fmt.Errorf("mica daemon reported failure")
	default:
		log.FDebugf("Received unexpected response: %s", response)
		return response, fmt.Errorf("unexpected response format: %s", response)
	}
}
