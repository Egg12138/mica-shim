package entry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	defs "mica-shim/definitions"
	log "mica-shim/logger"
	cntr "mica-shim/pkg/container"
	"mica-shim/pkg/libmica"

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
	// TALK: for one rtos container, make agent process(in Linux) as the init process?
	pid        int
	doneCtx    context.Context
	exitTime   time.Time
	exitStatus int
	stdout     string
	micaIO     *libmica.MicaIO // IO handler for PTY communication
}

// micaTaskService is an implementation of a containerd taskAPI.TaskService
// which prints the current time at regular intervals.
type micaTaskService struct {
	m     sync.RWMutex
	procs initProcByTaskID
	ss    shutdown.Service
}

var (
	_ taskAPI.TaskService = (*micaTaskService)(nil)
)

// RegisterTTRPC registers this TTRPC service with the given TTRPC server.
func (s *micaTaskService) RegisterTTRPC(srv *ttrpc.Server) error {
	taskAPI.RegisterTaskService(srv, s)
	return nil
}

// TODO: expand mica response system
func success(response string) bool {
	return response == defs.MicaSuccess 
}


func loadContainerState(id string) (*cntr.Container, error) {
	// bundlestate:id == id?
	cwd, err := os.Getwd()
	log.Debugf("cwd: %s", cwd)
	if err == nil {
		container, err := cntr.LoadContainerState(cwd)
		if err == nil {
			if id != container.ID {
				return nil, fmt.Errorf("container id mismatch: %s != %s", id, container.ID)
			}
			return container, nil
		}
	}

	container, err := cntr.LoadContainerState(defs.MicranStateDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load container state: %w", err)
	}
	if id != container.ID {
		return nil, fmt.Errorf("container id mismatch: %s != %s", id, container.ID)
	}
	return container, nil
}

func setupMicranStateDir() error {
	if err := os.MkdirAll(defs.MicranStateDir, 0755); err != nil {
		return fmt.Errorf("failed to create micran state directory: %w", err)
	}
	return nil
}

func saveContainerState(c *cntr.Container) error {
	bundle := c.GetConfig().Bundle
	id := c.ID
	
	statePath := filepath.Join(bundle, "state.json")
	state, err := json.Marshal(c)
	log.Pretty("save container %v as state %v", c, state)
	if err != nil {
		return fmt.Errorf("failed to marshal container state: %w", err)
	}
	if err := os.WriteFile(statePath, state, 0644); err != nil {
		return fmt.Errorf("failed to write container state to %s: %w", statePath, err)
	}

	// join "defs.MicranStateDir/<id>.json"
	statePath = filepath.Join(defs.MicranStateDir, id+".json")
	state, err = json.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal container state: %w", err)
	}
	if err := os.WriteFile(statePath, state, 0644); err != nil {
		return fmt.Errorf("failed to write container state to %s: %w", statePath, err)
	}

	log.Debugf("saved container state to %s", statePath)
	return nil
}