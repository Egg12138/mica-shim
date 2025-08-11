package references
package entry

import (
    "context"
    "sync"
    "time"
    
    "github.com/containerd/containerd/api/events"
    "github.com/containerd/containerd/api/types/task"
    "github.com/containerd/typeurl"
    "github.com/sirupsen/logrus"
)

// Event publishing for shim
func (s *shimService) publishEvent(ctx context.Context, topic string, event interface{}) error {
    s.eventSendMutex.Lock()
    defer s.eventSendMutex.Unlock()
    
    // Convert event to protobuf Any type
    any, err := typeurl.MarshalAny(event)
    if err != nil {
        return err
    }
    
    // Send event through containerd's event system
    select {
    case s.events <- &events.Envelope{
        Timestamp: time.Now(),
        Namespace: s.namespace,
        Topic:     topic,
        Event:     any,
    }:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    default:
        logrus.Warnf("event channel full, dropping event: %s", topic)
        return nil
    }
}

// Required events to implement:

// 1. Task Create Event
func (s *shimService) publishTaskCreate(ctx context.Context, containerID string, pid uint32) error {
    return s.publishEvent(ctx, "/tasks/create", &task.TaskCreate{
        ContainerID: containerID,
        Pid:         pid,
        Bundle:      "", // Set appropriate bundle path
        IO: &task.TaskIO{
            Stdin:    "",
            Stdout:   "",
            Stderr:   "",
            Terminal: false,
        },
        Checkpoint: "",
    })
}

// 2. Task Start Event  
func (s *shimService) publishTaskStart(ctx context.Context, containerID string, pid uint32) error {
    return s.publishEvent(ctx, "/tasks/start", &task.TaskStart{
        ContainerID: containerID,
        Pid:         pid,
    })
}

// 3. Task Exit Event (CRITICAL - containerd waits for this)
func (s *shimService) publishTaskExit(ctx context.Context, containerID string, pid uint32, exitStatus uint32, exitedAt time.Time) error {
    return s.publishEvent(ctx, "/tasks/exit", &task.TaskExit{
        ContainerID: containerID,
        ID:          containerID,
        Pid:         pid,
        ExitStatus:  exitStatus,
        ExitedAt:    exitedAt,
    })
}

// 4. Task Delete Event
func (s *shimService) publishTaskDelete(ctx context.Context, containerID string, pid uint32, exitStatus uint32, exitedAt time.Time) error {
    return s.publishEvent(ctx, "/tasks/delete", &task.TaskDelete{
        ContainerID: containerID,
        Pid:         pid,
        ExitStatus:  exitStatus,
        ExitedAt:    exitedAt,
    })
}

// 5. Task Paused/Resumed Events (if supported)
func (s *shimService) publishTaskPaused(ctx context.Context, containerID string) error {
    return s.publishEvent(ctx, "/tasks/paused", &task.TaskPaused{
        ContainerID: containerID,
    })
}

// Integration into your existing methods:
func (s *shimService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
    // ... existing create logic ...
    
    // After successful creation:
    if err := s.publishTaskCreate(ctx, r.ID, 1); err != nil {
        log.Warnf("failed to publish task create event: %v", err)
    }
    
    return &taskAPI.CreateTaskResponse{Pid: 1}, nil
}

func (s *shimService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
    // ... existing start logic ...
    
    // After successful start:
    if err := s.publishTaskStart(ctx, r.ID, uint32(proc.pid)); err != nil {
        log.Warnf("failed to publish task start event: %v", err)
    }
    
    return &taskAPI.StartResponse{Pid: uint32(proc.pid)}, nil
}

// CRITICAL: Fix handleTaskExit to publish events
func (s *shimService) handleTaskExit(id string, exitCode int) {
    log.Debugf("handleTaskExit id:%s exitCode:%d", id, exitCode)
    
    s.m.Lock()
    defer s.m.Unlock()
    
    if container, ok := s.containers[id]; ok {
        exitTime := time.Now()
        container.exitTime = exitTime
        container.exitStatus = exitCode
        
        // CRITICAL: Publish exit event - containerd waits for this
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        
        if err := s.publishTaskExit(ctx, id, uint32(container.pid), uint32(exitCode), exitTime); err != nil {
            log.Errorf("failed to publish task exit event for %s: %v", id, err)
        }
        
        // Cancel contexts
        if container.doneCancel != nil {
            container.doneCancel()
        }
        if container.lifecycleCancel != nil {
            container.lifecycleCancel()
        }
    }
}


type Publisher interface {
	Publish(ctx context.Context, topic string, ev interface{}) error
}

type containerdPublisher struct {
	rp *shim.RemoteEventsPublisher
}

func NewContainerdPublisher(ttrpcAddr string) (Publisher, error) {
	rp := shim.NewRemoteEventsPublisher(ttrpcAddr)
	return &containerdPublisher{rp: rp}, nil
}

func (p *containerdPublisher) Publish(ctx context.Context, topic string, ev interface{}) error {
	// retry with backoff for important events (TaskExit)
	var lastErr error
	for i := 0; i < 3; i++ {
			if err := p.rp.Publish(ctx, topic, ev); err == nil {
					return nil
			} else {
					lastErr = err
					time.Sleep(200 * time.Millisecond)
			}
	}
	return lastErr
}