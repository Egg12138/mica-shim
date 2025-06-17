package main

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

type micaSocket struct {
	conn net.Conn
}

func newMicaSocket() (*micaSocket, error) {
	conn, err := net.Dial("unix", micaCreateSocket)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to mica socket: %v", err)
	}
	return &micaSocket{conn: conn}, nil
}

func (s *micaSocket) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *micaSocket) SendCommand(cmd string) error {
	if s.conn == nil {
		return fmt.Errorf("socket not connected")
	}

	// Set timeout
	s.conn.SetDeadline(time.Now().Add(defaultTimeout))

	// Send command
	_, err := s.conn.Write([]byte(cmd))
	if err != nil {
		return fmt.Errorf("failed to send command: %v", err)
	}

	// Read response
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

// Helper functions for specific commands
func (s *micaSocket) CreateClient(config string) error {
	return s.SendCommand(fmt.Sprintf("create %s", config))
}

func (s *micaSocket) StartClient(client string) error {
	return s.SendCommand(fmt.Sprintf("start %s", client))
}

func (s *micaSocket) StopClient(client string) error {
	return s.SendCommand(fmt.Sprintf("stop %s", client))
}

func (s *micaSocket) RemoveClient(client string) error {
	return s.SendCommand(fmt.Sprintf("rm %s", client))
}

func (s *micaSocket) GetStatus() error {
	return s.SendCommand("status")
} 