package shim

import (
	"context"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/errdefs"
	ptypes "github.com/containerd/containerd/protobuf/types"
)

func (s *shimService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Delete(ctx context.Context, r *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Pids(ctx context.Context, r *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Pause(ctx context.Context, r *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Resume(ctx context.Context, r *taskAPI.ResumeRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Kill(ctx context.Context, r *taskAPI.KillRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Exec(ctx context.Context, r *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) ResizePty(ctx context.Context, r *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) CloseIO(ctx context.Context, r *taskAPI.CloseIORequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Checkpoint(ctx context.Context, r *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Update(ctx context.Context, r *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Wait(ctx context.Context, r *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {
	return nil, errdefs.ErrNotImplemented

}

func (s *shimService) Connect(ctx context.Context, r *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {
	return nil, errdefs.ErrNotImplemented
}

func (s *shimService) Shutdown(ctx context.Context, r *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {
	return nil, errdefs.ErrNotImplemented

}

func (s *shimService) Stats(ctx context.Context, r *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {
	return nil, errdefs.ErrNotImplemented

}

func (s *shimService) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {
	return nil, errdefs.ErrNotImplemented

}

