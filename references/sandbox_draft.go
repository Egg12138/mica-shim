
import (
	"fmt"
)


import (
    "context"
    "fmt"
    "strings"
    
    "github.com/containerd/containerd/api/types/task"
    specs "github.com/opencontainers/runtime-spec/specs-go"
)

// Sandbox-specific implementation for Kubernetes

// 1. Detect container type from annotations
func getContainerType(spec *specs.Spec) (ContainerType, error) {
    if spec.Annotations == nil {
        return Regular, nil
    }
    
    // Kubernetes CRI annotations
    containerType := spec.Annotations["io.kubernetes.cri.container-type"]
    
    switch containerType {
    case "sandbox":
        return PodSandbox, nil
    case "container":
        return PodContainer, nil
    case "":
        // Check if this is a pause container (common sandbox pattern)
        if isPauseContainer(spec) {
            return PodSandbox, nil
        }
        return Regular, nil
    default:
        return UnknownCtype, fmt.Errorf("unknown container type: %s", containerType)
    }
}

func isPauseContainer(spec *specs.Spec) bool {
    if spec.Process == nil || len(spec.Process.Args) == 0 {
        return false
    }
    
    // Common pause container patterns
    pausePatterns := []string{"pause", "/pause", "k8s.gcr.io/pause"}
    
    for _, arg := range spec.Process.Args {
        for _, pattern := range pausePatterns {
            if strings.Contains(arg, pattern) {
                return true
            }
        }
    }
    
    return false
}

// 2. Sandbox-specific container creation
func (s *shimService) createSandboxContainer(r *taskAPI.CreateTaskRequest) (*TaskContainer, error) {
    container, err := createContainer(r)
    if err != nil {
        return nil, err
    }
    
    // Sandbox containers need special handling
    switch container.cType {
    case PodSandbox:
        return s.createPodSandbox(container, r)
    case PodContainer:
        return s.createPodContainer(container, r)
    default:
        return s.createRegularContainer(container, r)
    }
}

// 3. Pod Sandbox implementation
func (s *shimService) createPodSandbox(container *cntr.Container, r *taskAPI.CreateTaskRequest) (*TaskContainer, error) {
    log.Infof("Creating pod sandbox: %s", r.ID)
    
    // Sandbox containers typically:
    // 1. Set up pod-level resources (networking, storage)
    // 2. Provide shared namespaces for pod containers
    // 3. Act as pause/placeholder containers
    
    // For MICA RTOS, sandbox might:
    // 1. Reserve CPU cores for the pod
    // 2. Set up shared communication channels
    // 3. Configure pod-level resource limits
    
    config := container.GetConfig()
    
    // Reserve resources for the entire pod
    podResources := &PodResources{
        CPUCores:    s.reservePodCPUs(config.NCpu),
        MemoryLimit: config.MemoryLimit,
        PodID:       r.ID,
    }
    
    // Create minimal RTOS client for sandbox (might be pause-like)
    sandboxConf := s.createSandboxMicaConf(container, podResources)
    
    taskContainer := &TaskContainer{
        container: container,
        pid:      1, // Placeholder PID
        podResources: podResources,
    }
    
    // Sandbox usually doesn't run actual workload, just reserves resources
    if config.Type == PodSandbox {
        // Create but don't start - sandbox acts as resource placeholder
        res, err := libmica.MicaCreate(sandboxConf)
        if err != nil || !success(res) {
            return nil, fmt.Errorf("failed to create sandbox: %w", err)
        }
        log.Debugf("Created sandbox placeholder: %s", res)
    }
    
    return taskContainer, nil
}

// 4. Pod Container implementation  
func (s *shimService) createPodContainer(container *cntr.Container, r *taskAPI.CreateTaskRequest) (*TaskContainer, error) {
    log.Infof("Creating pod container: %s", r.ID)
    
    // Find associated sandbox
    sandbox, err := s.findPodSandbox(r.ID)
    if err != nil {
        return nil, fmt.Errorf("failed to find pod sandbox: %w", err)
    }
    
    // Use sandbox's allocated resources
    containerConf, err := s.createPodContainerMicaConf(container, sandbox.podResources)
    if err != nil {
        return nil, err
    }
    
    taskContainer := &TaskContainer{
        container: container,
        pid:      1,
        sandbox:  sandbox, // Reference to sandbox
    }
    
    // Pod containers run actual workloads
    res, err := libmica.MicaCreate(containerConf)
    if err != nil || !success(res) {
        return nil, fmt.Errorf("failed to create pod container: %w", err)
    }
    
    return taskContainer, nil
}

// 5. Resource management for pods
type PodResources struct {
    CPUCores    []int
    MemoryLimit int64
    PodID       string
}

func (s *shimService) reservePodCPUs(requestedCPUs int) []int {
    // Implement CPU allocation for entire pod
    // This would use your CPU scheduling algorithm
    
    cpus := make([]int, requestedCPUs)
    for i := 0; i < requestedCPUs; i++ {
        cpu, err := s.allocateCPU() // Your CPU allocation logic
        if err != nil {
            log.Errorf("failed to allocate CPU: %v", err)
            continue
        }
        cpus[i] = cpu
    }
    
    return cpus
}

func (s *shimService) createSandboxMicaConf(container *cntr.Container, resources *PodResources) libmica.MicaClientConf {
    conf := libmica.MicaClientConf{}
    
    // Sandbox typically uses minimal resources
    conf.Init(
        uint32(resources.CPUCores[0]), // Use first allocated CPU
        uint64(resources.MemoryLimit),
        container.ID,
        "/path/to/pause/firmware", // Minimal RTOS image
        "xen",
        "",
        false,
    )
    
    return conf
}

func (s *shimService) createPodContainerMicaConf(container *cntr.Container, resources *PodResources) (libmica.MicaClientConf, error) {
    config := container.GetConfig()
    conf := libmica.MicaClientConf{}
    
    // Use resources allocated by sandbox
    selectedCPU := resources.CPUCores[0] // Simple allocation - use first available
    
    conf.Init(
        uint32(selectedCPU),
        uint64(config.MemoryLimit),
        container.ID,
        config.GetFirmwarePath(),
        config.GetPed().PedestalType.String(),
        config.GetPed().PedestalConf,
        false,
    )
    
    return conf, nil
}

// 6. Sandbox lifecycle management
func (s *shimService) findPodSandbox(containerID string) (*TaskContainer, error) {
    // Parse container ID to find pod ID
    // Kubernetes typically uses format: <pod-uid>-<container-name>
    // Or look for sandbox annotation in container spec
    
    for id, container := range s.containers {
        if container.container.cType == PodSandbox {
            // Simple matching - in real implementation, use proper pod ID parsing
            if strings.HasPrefix(containerID, strings.Split(id, "-")[0]) {
                return container, nil
            }
        }
    }
    
    return nil, fmt.Errorf("sandbox not found for container: %s", containerID)
}

// 7. Enhanced TaskContainer for sandbox support
type TaskContainer struct {
    container    *cntr.Container
    pid          int
    exitTime     time.Time
    exitStatus   int
    
    // Sandbox-specific fields
    podResources *PodResources     // For sandbox containers
    sandbox      *TaskContainer    // For pod containers - reference to their sandbox
    
    // I/O and lifecycle management
    micaIO           *libmica.MicaIO
    lifecycleCtx     context.Context
    lifecycleCancel  context.CancelFunc
    monitorCancel    context.CancelFunc
    doneCtx          context.Context
    doneCancel       context.CancelFunc
    
    mu sync.RWMutex
}

/*
KEY POINTS FOR SANDBOX IMPLEMENTATION:

1. DETECTION: Use Kubernetes annotations to detect sandbox vs regular containers
2. RESOURCE ALLOCATION: Sandbox reserves resources, containers use them  
3. LIFECYCLE: Sandbox created first, containers reference it
4. RTOS MAPPING: 
   - Sandbox = minimal RTOS or resource placeholder
   - Pod containers = actual workload RTOS
5. CPU SCHEDULING: Coordinate between sandbox and containers
6. CLEANUP: Delete containers first, then sandbox

For your MICA runtime:
- Sandbox might not need actual RTOS, just resource reservation
- Pod containers run real RTOS workloads using sandbox's resources
- Consider CPU affinity and memory isolation between pods
*/
type ContainerdSandboxOps interface {
	// ID is a sandbox identifier
	ID() string
	// PID returns sandbox's process PID or error if its not yet started.
	PID() (uint32, error)
	// NewContainer creates new container that will belong to this sandbox
	NewContainer(ctx context.Context, id string, opts ...containerd.NewContainerOpts) (Container, error)
	// Labels returns the labels set on the sandbox
	Labels(ctx context.Context) (map[string]string, error)
	// Start starts new sandbox instance
	Start(ctx context.Context) error
	// Stop sends stop request to the shim instance.
	Stop(ctx context.Context) error
	// Wait blocks until sandbox process exits.
	Wait(ctx context.Context) (<-chan containerd.ExitStatus, error)
	// Shutdown removes sandbox from the metadata store and shutdowns shim instance.
	Shutdown(ctx context.Context) error
}

