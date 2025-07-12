package core

import (
	"context"
	"fmt"
	"mica-shim/cntr"
	defs "mica-shim/definitions"
	"mica-shim/libmica"
	log "mica-shim/logger"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
)

// setup the task and client os; without managing micataskservice
func create(ctx context.Context, req *taskAPI.CreateTaskRequest) (taskRes *taskAPI.CreateTaskResponse, retErr error) {

	// get firmware path from bundle: <bundle>/.../<clientOSname>.elf
	// NOTICE: currently, everytime mica startup client os, it search files from the given path
	// There, we must verify the bundle integrity before creating && **running** the client os.

	spec, err := cntr.LoadContainerSpec(req)
	if err != nil {
		return nil, fmt.Errorf("failed to load container spec: %w", err)
	}
	ctype, err := cntr.GetContainerType(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to get container type: %w", err)
	}
	container, err := cntr.NewContainer(req, *spec, ctype)
	conf := CreateMicaConf(container)
	if err != nil {
		return nil, fmt.Errorf("failed to create mica create conf: %w", err)
	}
	res, err := libmica.Create(conf)
	if err != nil {
		return nil, fmt.Errorf("mica create: %w", err)
	}
	if res == defs.MicaSuccess {
		taskRes = &taskAPI.CreateTaskResponse{
			Pid: 1,
		}
		log.LocateDebugf("mica create success")
	}

	return nil, nil
}

func start(ctx context.Context, req *taskAPI.StartRequest) (taskRes *taskAPI.StartResponse, retErr error) {

	libmica.MicaCtl(libmica.MStart, req.ID)

	return nil, nil
}

// 维护一个全局的clientPairs, 用于管理{client : agent process} pairs
var pairs *clientPairs

func init() {
	pairs = &clientPairs{}
}

type clientPairs map[string]int

// TODO: A RED-BLACK tree structure, managing {client : agent process} pairs
func (t *clientPairs) addClientPair(clientOSname string, agentPid int) {
	if _, ok := (*t)[clientOSname]; ok {
		log.Errorf("client %s already exists", clientOSname)
		return
	}
	(*t)[clientOSname] = agentPid
}

func (t *clientPairs) getClientPair(clientOSname string) int {
	pid, ok := (*t)[clientOSname]
	if !ok {
		log.Errorf("client %s not found", clientOSname)
		return -1
	}
	return pid
}

func (t *clientPairs) removeClient(clientOSname string) {
	if _, ok := (*t)[clientOSname]; !ok {
		log.Errorf("client %s not found", clientOSname)
		return
	}
	delete(*t, clientOSname)
}

func (t *clientPairs) getAgentPid(clientOSname string) int {
	pid, ok := (*t)[clientOSname]
	if !ok {
		log.Errorf("client %s not found", clientOSname)
		return -1
	}
	return pid
}
