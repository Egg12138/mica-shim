package reference

import (
	"fmt"
	"net"
	"time"
)

const (
	micaCreateSocket = "/run/mica/mica-create.socket"
	micaSocketDir    = "/run/mica"
	defaultTimeout   = 5 * time.Second
)

type MicaSocket struct {
	conn net.Conn
}

func NewMicaSocket() (*MicaSocket, error) {
	conn, err := net.Dial("unix", micaCreateSocket)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to mica socket: %v", err)
	}
	return &MicaSocket{conn: conn}, nil
}

func (s *MicaSocket) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *MicaSocket) sendCommand(cmd string) error {
	if s.conn == nil {
		return fmt.Errorf("socket not connected")
	}

	s.conn.SetDeadline(time.Now().Add(defaultTimeout))

	_, err := s.conn.Write([]byte(cmd))
	if err != nil {
		return fmt.Errorf("failed to send command: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := s.conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	response := string(buf[:n])
	if response == "MICA-FAILED" {
		return fmt.Errorf("mica command failed")
	}

	return nil
}

// Callers

// TODO: make config a Struct
func (s *MicaSocket) CreateClient(config string) error {
	return s.sendCommand(fmt.Sprintf("create %s", config))
}

func (s *MicaSocket) StartClient(client string) error {
	return s.sendCommand(fmt.Sprintf("start %s", client))
}

func (s *MicaSocket) StopClient(client string) error {
	return s.sendCommand(fmt.Sprintf("stop %s", client))
}

func (s *MicaSocket) RemoveClient(client string) error {
	return s.sendCommand(fmt.Sprintf("rm %s", client))
}

func (s *MicaSocket) GetStatus() error {
	return s.sendCommand("status")
}
