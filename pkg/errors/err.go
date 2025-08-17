package errors

import (
	"fmt"
)

type ErrCode int
type MicranErr struct {
	Code ErrCode
	Msg  string
}

func (e *MicranErr) Error() string {
	return fmt.Sprintf("[%d] %s", e.Code, e.Msg)
}

func new(code ErrCode, msg string) *MicranErr {
	return &MicranErr{
		Code: code,
		Msg:  msg,
	}
}

const (
	InvalidState ErrCode = iota
	SocketFailed
	EmptyID
	NotFound
	AlreadyExists
)

// Pre-defined errors.
var (
	ErrInvalidState      = new(InvalidState, "invalid state")
	ErrSocketFailed      = new(SocketFailed, "socket failed")
	ErrEmptyContainerID  = new(EmptyID, "empty container id")
	ErrEmptySandboxID    = new(EmptyID, "empty sandbox id")
	ErrAlreadyExists     = new(AlreadyExists, "already exists")
	ErrContainerNotFound = new(NotFound, "container not found")
	ErrSandboxNil        = new(NotFound, "sandbox is nil")
)

// Panic-related errors.
