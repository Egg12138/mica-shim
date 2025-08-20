package shim

import (
	"io"
	cntr "mica-shim/pkg/micantainer"
	"time"

	"github.com/containerd/containerd/api/types/task"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type container struct {
	s           *shimService
	ttyio       *ttyIO
	spec        *specs.Spec
	exitTime    time.Time
	exitIOch    chan struct{}
	stdinPipe   io.WriteCloser
	stdinCloser chan struct{}
	exitCh      chan uint32
	id          string
	stdin       string
	// stdout, stderr => consoleOut
	stdout      string
	stderr      string
	bundle      string
	cType       cntr.ContainerType
	// exit status code
	exit        uint32
	status      task.Status
	terminal    bool
	mounted     bool
}


type ttyIO struct {
}
