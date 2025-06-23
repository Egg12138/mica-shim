package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/pkg/shutdown"
	"github.com/containerd/containerd/runtime/v2/shim"
	"github.com/containerd/ttrpc"
)

// shutdown.Service is used to facilitate shutdown by through callback
func newTaskService(ss shutdown.Service) (*micaTaskService, error) {
	s := &micaTaskService{
		procs: make(initProcByTaskID, 1),
		ss:    ss,
	}

	sockAddr, err := shim.ReadAddress("address")
	if err != nil {
		return nil, fmt.Errorf("reading socket address from address file: %w", err)
	}

	ss.RegisterCallback(func(context.Context) error {
		if err := shim.RemoveSocket(sockAddr); err != nil {
			return fmt.Errorf("removing shim socket on shutdown: %w", err)
		}
		return nil
	})

	// ss.RegisterCallback(func(ctx context.Context) error {
	// 	if sockAddr, err := shim.ReadAddress("address"); err == nil {
	// 		if err := shim.RemoveSocket(sockAddr); err != nil {
	// 			log.Errorf("removing shim socket on shutdown: %v", err)
	// 			return fmt.Errorf("removing shim socket on shutdown: %w", err)
	// 		}
	// 	}
	// 	// Don't fail shutdown if address file doesn't exist or socket removal fails
	// 	return nil
	// })

	return s, nil
}

// initProcByTaskID maps init (parent) processes to their associated task by ID.
type initProcByTaskID map[string]*initProcess

// initProcess encapsulates information about an init (parent) process.
// TODO: handle the init process, there it is just a placeholder
// TALK: init process **represent** the process in RTOS
type initProcess struct {
	// TALK: for one container pod, make agent process(in Linux) as the init process?
	pid        int
	doneCtx    context.Context
	exitTime   time.Time
	exitStatus int
	stdout     string
}

// micaTaskService is an implementation of a containerd taskAPI.TaskService
// which prints the current time at regular intervals.
type micaTaskService struct {
	m     sync.RWMutex
	procs initProcByTaskID
	ss shutdown.Service
}

var (
	_ shim.TTRPCService   = (*micaTaskService)(nil)
	_ taskAPI.TaskService = (*micaTaskService)(nil)
)

// RegisterTTRPC registers this TTRPC service with the given TTRPC server.
func (s *micaTaskService) RegisterTTRPC(srv *ttrpc.Server) error {
	taskAPI.RegisterTaskService(srv, s)
	return nil
}
