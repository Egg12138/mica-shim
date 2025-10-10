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
	NotFound
	SocketFailed
	Invalid
	AlreadyExists
	MicadFailed
	DuplicatedKey
	UnexpectedStatus
	IOClose
	NotSuppoted
	MicadAbnormal
	ParseFailed
)

// Pre-defined errors.
var (
	ErrInvalidState      = new(InvalidState, "invalid state")
	ErrInvalidCID        = new(Invalid, "invalid container id")
	ErrSocketFailed      = new(SocketFailed, "socket failed")
	ErrEmptyContainerID  = new(Invalid, "empty container id")
	ErrEmptySandboxID    = new(Invalid, "empty sandbox id")
	ErrAlreadyExists     = new(AlreadyExists, "already exists")
	ErrContainerNotFound = new(NotFound, "container not found")
	ErrSandboxNil        = new(NotFound, "sandbox is nil")
	ErrSandboxDown       = new(UnexpectedStatus, "sandbox is not running")
	ErrIOClose           = new(IOClose, "io closed")
	ErrNotRunning        = new(UnexpectedStatus, "container is not running")

	ErrPedestalMismatch = new(Invalid, "host pedestal type mismatch with image pedestal type")
	ErrCommandOutputParse = new(ParseFailed, "failed to parse command output")

	ErrMicadFailed     = new(MicadFailed, "mica operation failed")
	ErrMicadNotRunning = new(MicadAbnormal, "mica daemon is not running")
	ErrMicaCreatSock = new(MicadAbnormal, "mica-create socket is not alive")
	ErrNotSuppoted     = new(NotSuppoted, "micran or mica does not support this")
	ErrInvalidSig      = new(Invalid, "invalid signal for client os")
)

// Type errors
var (
	ErrDuplicatedKey = new(DuplicatedKey, "duplicated key in the map")
)

// Panic-related errors.

// Warnings

var (
	FlexibleTaskUnsupported = new(MicadFailed, "micran does not support exec task, task are immutable inside client os")
	ContainerVCPUNotPined = new(MicadFailed, "container's vcpus are not pinned")
)
