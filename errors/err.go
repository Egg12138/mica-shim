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
	invalidState ErrCode = iota
	notFound
	socketFailed
	invalid
	alreadyExists
	micadFailed
	duplicatedKey
	unexpectedStatus
	ioClose
	notSupported
	micadAbnormal
	parseFailed
)

// Pre-defined errors.
var (
	InvalidState      = new(invalidState, "invalid state")
	InvalidCID        = new(invalid, "invalid container id")
	SocketFailed      = new(socketFailed, "socket failed")
	EmptyContainerID  = new(invalid, "empty container id")
	EmptySandboxID    = new(invalid, "empty sandbox id")
	AlreadyExists     = new(alreadyExists, "already exists")
	ContainerNotFound = new(notFound, "container not found")
	SandboxNotFound   = new(notFound, "sandbox is nil")
	SandboxDown       = new(unexpectedStatus, "sandbox is not running")
	IOClosed          = new(ioClose, "io closed")
	NotRunning        = new(unexpectedStatus, "container is not running")

	PedestalMismatched = new(invalid, "host pedestal type mismatch with image pedestal type")
	ErrOutputParse     = new(parseFailed, "failed to parse command output")

	MicadOpFailed   = new(micadFailed, "mica operation failed")
	MicadNotRunning = new(micadAbnormal, "mica daemon is not running")
	MicaSocketDown  = new(micadAbnormal, "mica-create socket is not alive")
	NotSupported    = new(notSupported, "micran or mica does not support this")
	InvalidSignal   = new(invalid, "invalid signal for client os")
)

// Type errors
var (
	DuplicatedKey = new(duplicatedKey, "duplicated key in the map")
)

// Panic-related errors.

// Warnings

var (
	FlexibleTaskUnsupported = new(micadFailed, "micran does not support exec task, task are immutable inside client os")
	ContainerVCPUNotPined   = new(micadFailed, "container's vcpus are not pinned")
)
