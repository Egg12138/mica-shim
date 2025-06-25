package core

import (
	"context"
	"fmt"
	defs "mica-shim/definitions"
	"mica-shim/libmica"
	log "mica-shim/logger"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
)

// setup the task and client os; without managing micataskservice
func create(ctx context.Context, req *taskAPI.CreateTaskRequest) (_ *taskAPI.CreateTaskResponse, retErr error) {

	res, err := libmica.DummyCreate(req.ID)
	if err != nil {
		return nil, fmt.Errorf("mica create: %w", err)
	}
	if res == defs.MicaSuccess {
		log.LocateDebugf("mica create success")
	}

	return nil, nil
}