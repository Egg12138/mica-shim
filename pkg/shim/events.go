package shim

import (
	"time"

	"github.com/containerd/containerd/api/events"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	timeOut = 5 * time.Second
	ttrpcAddrEnv = "TTRPC_ADDRESS"
	contdShimEnvShedCore = "SCHED_CORE"

)


func (s *shimService) listenAndReportExits() {
	for e := range s.ec {
		s.reportExit(e)
	}
}

func (s *shimService) reportExit(e exit)  {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := e.execid
	if id == "" { id = e.cid }
	s.eventSendMu.Lock()
	defer s.eventSendMu.Unlock()
	s.send(&events.TaskExit{
		ContainerID: 	e.cid,
		ID: 					id,
		Pid: 					e.pid,
		ExitStatus: 	uint32(e.status),
		ExitedAt: 		timestamppb.New(e.ts),
	})
}

func (s *shimService) send(ev any) {
	if s.events != nil {
		s.events <- ev
	}
}

