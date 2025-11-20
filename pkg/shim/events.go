// Package shim provides the implementation of the containerd shim v2 interface for micran.
package shim

import (
	"time"

	"github.com/containerd/containerd/api/events"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	timeOut              = 5 * time.Second
	ttrpcAddrEnv         = "TTRPC_ADDRESS"
	contdShimEnvShedCore = "SCHED_CORE"
)

// listenAndReportExits listens for exit events on a channel and reports them.
func (s *shimService) listenAndReportExits() {
	for e := range s.ec {
		s.reportExit(e)
	}
}

// reportExit sends a TaskExit event to containerd.
func (s *shimService) reportExit(e exit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := e.execid
	if id == "" {
		id = e.cid
	}
	s.eventSendMu.Lock()
	defer s.eventSendMu.Unlock()
	s.send(&events.TaskExit{
		ContainerID: e.cid,
		ID:          id,
		Pid:         e.pid,
		ExitStatus:  uint32(e.status),
		ExitedAt:    timestamppb.New(e.ts),
	})
}

// send places an event on the events channel for forwarding.
func (s *shimService) send(ev any) {
	if s.events != nil {
		s.events <- ev
	}
}
