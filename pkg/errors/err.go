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

// TALK: 错误语义的一致性，包含性非常糟糕
const (
	InvalidState ErrCode = iota
	SocketFailed
	InvalidID
	NotFound
	AlreadyExists
	MicadFailed
	DuplicatedKey
	NotRunning
	IOClose
)

// Pre-defined errors.
var (
	ErrInvalidState      = new(InvalidState, "invalid state")
	ErrInvalidCID        = new(InvalidID, "invalid container id")
	ErrSocketFailed      = new(SocketFailed, "socket failed")
	ErrEmptyContainerID  = new(InvalidID, "empty container id")
	ErrEmptySandboxID    = new(InvalidID, "empty sandbox id")
	ErrAlreadyExists     = new(AlreadyExists, "already exists")
	ErrContainerNotFound = new(NotFound, "container not found")
	ErrSandboxNil        = new(NotFound, "sandbox is nil")
	ErrSandboxDown       = new(NotRunning, "sandbox is not running")
	ErrIOClose           = new(IOClose, "io closed")

	ErrMicaStopFailed = new(MicadFailed, "mica stop failed")
)

// Type errors
var (
	ErrDuplicatedKey = new(DuplicatedKey, "duplicated key in the map")
)

// Panic-related errors.
