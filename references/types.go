package references

import (
	"context"
	"fmt"
	core "mica-shim/pkg/oci"
	"sync"
	"time"

	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/events"
	"github.com/go-openapi/runtime/logger"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// 简化设计：仿照 kata，只保留 shimService -> Container

// ✅ 简化的 shimService（参考 kata）
type shimService struct {
	// 核心管理
	containers map[string]*Container  // 直接管理容器，无中间层
	
	// 上下文管理
	ctx       context.Context
	rootCtx   context.Context         // for tracing if needed
	cancel    func()
	
	// 通信通道
	events    chan interface{}       // 事件发布
	monitor   chan error            // 错误监控
	ec        chan exit             // 退出通知（如果需要）
	
	// 配置
	config    *core.RuntimeConfig
	namespace string
	id        string
	
	mu          sync.RWMutex         // 统一的读写锁
	eventSendMu sync.Mutex          // 事件发送锁
	
	shimPid uint32                  // shim pid
}

// ✅ 简化的 Container（合并原来的多层信息）
type Container struct {
	// 基础标识
	ID        string
	Bundle    string
	Namespace string
	
	// 类型和状态
	Type      ContainerType
	Status    task.Status
	
	// 进程信息（用于 containerd API）
	Pid       uint32                // 占位 PID（对应 RTOS client）
	ExitTime  time.Time
	ExitCode  uint32
	
	// MICA 特定
	MicaIO    *libmica.MicaIO       // I/O 处理
	ClientID  string               // RTOS client ID
	
	// OCI 规范配置（直接嵌入，避免额外嵌套）
	Spec      *specs.Spec          // 原始 OCI spec
	
	// 资源配置（从 ContainerConfig 提取关键字段）
	CPUAllocation int               // 实际分配的 CPU（运行时状态）
	CPURequest    int               // 请求的 CPU 数量
	MemoryLimit   int64             // 内存限制
	
	// MICA 配置（从 ContainerConfig 提取关键字段）
	FirmwarePath  string
	PedestalType  string
	PedestalConf  string
	
	// 生命周期管理
	LifecycleCtx    context.Context
	LifecycleCancel context.CancelFunc
	MonitorCancel   context.CancelFunc
	
	// Sandbox 关系（仅用于 pod 容器）
	SandboxID   string              // 所属 sandbox ID（如果是 pod 容器）
	IsShared    bool               // 是否是共享资源（如果是 sandbox）
	
	// 同步（容器级别的细粒度锁）
	mu sync.RWMutex
}

// ✅ 辅助结构（替代复杂的 ContainerConfig）
type ContainerSpec struct {
	// 从 bundle 解析的关键配置，用于创建时的临时存储
	Bundle       string
	Type         ContainerType
	CPURequest   int
	MemoryLimit  int64
	FirmwarePath string
	PedestalType string
	PedestalConf string
}

// ✅ 简化的创建流程
func (s *shimService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if _, exists := s.containers[r.ID]; exists {
			return nil, errdefs.ErrAlreadyExists
	}
	
	// 直接解析和创建，不需要多层嵌套
	spec, err := s.parseContainerSpec(r.Bundle)
	if err != nil {
			return nil, err
	}
	
	container := &Container{
			ID:            r.ID,
			Bundle:        r.Bundle,
			Namespace:     s.namespace,
			Type:          spec.Type,
			Status:        task.Status_CREATED,
			Pid:           1, // 占位 PID
			CPURequest:    spec.CPURequest,
			MemoryLimit:   spec.MemoryLimit,
			FirmwarePath:  spec.FirmwarePath,
			PedestalType:  spec.PedestalType,
			PedestalConf:  spec.PedestalConf,
	}
	
	// 设置生命周期上下文
	container.LifecycleCtx, container.LifecycleCancel = context.WithCancel(ctx)
	
	s.containers[r.ID] = container
	
	// 发布创建事件
	s.publishTaskCreate(ctx, r.ID, 1)
	
	return &taskAPI.CreateTaskResponse{Pid: 1}, nil
}

// ✅ 简化的启动流程
func (s *shimService) Start(ctx context.Context, r *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {
	s.mu.RLock()
	container, exists := s.containers[r.ID]
	s.mu.RUnlock()
	
	if !exists {
			return nil, errdefs.ErrNotFound
	}
	
	// 直接操作容器，无需通过多层
	if err := s.startMicaClient(container); err != nil {
			return nil, err
	}
	
	container.mu.Lock()
	container.Status = task.Status_RUNNING
	container.mu.Unlock()
	
	// 启动监控
	go s.monitorContainer(container)
	
	// 发布启动事件
	s.publishTaskStart(ctx, r.ID, 1)
	
	return &taskAPI.StartResponse{Pid: 1}, nil
}

// ✅ 直接的 MICA 操作
func (s *shimService) startMicaClient(container *Container) error {
	// 直接从容器获取配置，无需层层传递
	conf := libmica.MicaClientConf{}
	
	cpu, err := s.allocateCPU(container.CPURequest)
	if err != nil {
			return err
	}
	
	container.mu.Lock()
	container.CPUAllocation = cpu
	container.mu.Unlock()
	
	conf.Init(
			uint32(cpu),
			uint64(container.MemoryLimit),
			container.ID,
			container.FirmwarePath,
			container.PedestalType,
			container.PedestalConf,
			false,
	)
	
	// 创建和启动 RTOS client
	if res, err := libmica.MicaCreate(conf); err != nil || !success(res) {
			return fmt.Errorf("failed to create mica client: %w", err)
	}
	
	if res, err := libmica.MicaCtl(libmica.MStart, container.ID); err != nil || !success(res) {
			return fmt.Errorf("failed to start mica client: %w", err)
	}
	
	return nil
}

/*
与 kata 对比的优势：

KATA 架构：
service -> container (2层)
- 直接管理
- 配置嵌入容器
- 统一的生命周期

你的简化架构：
shimService -> Container (2层)
- 直接管理容器
- 关键配置嵌入容器
- 统一的生命周期
- 移除冗余的 TaskContainer 和 ContainerConfig 层

删除的冗余：
1. TaskContainer 整个结构 - 功能合并到 Container
2. ContainerConfig 大部分字段 - 关键配置直接嵌入 Container
3. 重复的状态字段 - 每个信息只保留在最合适的地方
4. 多余的锁 - 统一到 shimService 和 Container 两级

保留的必要复杂性：
1. Container 的资源管理字段（OCI 规范要求）
2. MICA 特定字段（运行时需要）
3. Sandbox 关系字段（k8s 支持需要）
*/

// 全局 shim 服务结构体（每个 shim 实例一个）
type micaTaskService struct {
    // protect procs map and service-global fields
    mu sync.Mutex

    // map containerID -> *Task (并发安全通过 mu 保护)
    tasks map[string]*Task

    // the shim process id and namespace info
    shimPid uint32
    ns      string

    // event publisher (RemoteEventsPublisher 或自实现)
    eventsPublisher *events.RemoteEventsPublisher // 或自定义接口包装

    // CPU scheduler shared state (可能是文件/共享内存句柄)
    cpuAllocator *CPUAllocator

    // libmica socket handle/wrapper
    micaClient *libmica.Client // 假设libmica暴露client

    // logger
    log *logger.Logger

    // root paths
    stateDir string

    // global lifecycle ctx for shim
    ctx    context.Context
    cancel context.CancelFunc
}

// 单个“init”/container 的任务表示
type Task struct {
    // immutable once created
    ID        string
    Bundle    string
    CreatedAt time.Time

    // current process-like fields
    Pid       int
    Terminal  bool
    StdinPath string
    StdoutPath string
    StderrPath string

    // mica io bridge
    micaIO *libmica.MicaIO // 负责 /dev/ttyRPMSG<id> 的打开/读写/关闭

    // lifecycle contexts for per-task goroutines
    ctx    context.Context
    cancel context.CancelFunc

    // startup/done contexts for Wait()
    startupCtx    context.Context
    startupCancel context.CancelFunc
    doneCtx       context.Context
    doneCancel    context.CancelFunc

    // monitor goroutine cancel
    monitorCancel context.CancelFunc

    // locks for per-task mutable state
    mu sync.Mutex

    // exit state
    exitStatus int
    exitTime   time.Time

    // resource limits recorded from containerd
    cpuLimit int
    memLimit int64

    // bookkeeping for exec processes
    execs map[string]*ExecProcess

    // for ensuring cleanup order
    wg sync.WaitGroup

    // metadata/annotations
    labels map[string]string
}
