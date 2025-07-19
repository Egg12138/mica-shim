# Architecture 

## Latest

overview
![overview](./images/micashim-overview0623.png)

expanded
![expanded](./images/micashim-expaned0623.png)

## Logs

### DEMO 0.2 0623 overview

mica-shim
::package core
    :: shim
    :: runtime
    :: bundleParser

```mermaid
%%{init: {'theme':'auto', 'flowchart':{'nodeSpacing': 15, 'rankSpacing': 30, 'curve': 'basis'}}}%%
flowchart LR
    subgraph ContainerEco ["Container Ecosystem"]
        Containerd[containerd] --> |ttrpc/gRPC|MicaShim[mica-runtime shimv2]
        Kubernetes[Kubernetes] --> |CRI gRPC|Containerd
        CtrCLI[ctr CLI] --> |ttrpc/gRPC|Containerd
    end
   
    subgraph OCIImages ["OCI Images Sample Bundle"]
        BaseImage[zephyr-mica-scratch:1.0.0]
        BaseImage --> |extends|DebugApp[debug-enabled-zephyr]
        BaseImage --> |extends|HelloWorldApp[hello-world-zephyr]
    end
    
    subgraph MicaRuntime ["MICA Runtime"]
        BundleParser[OCI Bundle Parser]
        MicaShim --> RuntimeService[MicaRuntimeService]
        RuntimeService --> MicaClient[libMica]
        RuntimeService --> Components[Components]
        MicaShim -.-> BundleParser
    end
    
    subgraph MicaDaemonScope ["MICA Daemon & RTOS"]
        MicaDaemon[Micad]
        MicaClient --> |UnixSocket IPC|MicaDaemon
        MicaDaemon --> |CommunicationLayer|ZephyrRTOS[Zephyr RTOS]
        ZephyrRTOS --> AppTasks[Application Tasks]
        ZephyrRTOS --> RPMsgServices[RPMsg Services]
        ZephyrRTOS --> DebugInterface[Debug Interface]
    end
    
    subgraph Annotations ["Annotations examples"]
        OSAnnotation{{"org.openeuler.mica.client.os"}}
        FirmwareAnnotation{{"org.openeuler.mica.client.firmware"}}
    end
    
    HelloWorldApp -.-> Annotations
    BundleParser -.-> Annotations
    BundleParser -.-> Components
    
    %% Container Ecosystem styles
    style Containerd fill:#e1f5fe
    
    %% MICA Runtime styles (consistent for all runtime components)
    style MicaShim fill:#f3e5f5
    style RuntimeService fill:#f3e5f5
    style MicaClient fill:#f3e5f5
    style Components fill:#f3e5f5

    %% MICA Daemon styles
    style MicaDaemon fill:#e8f5e8
    
    %% RTOS Core styles
    style ZephyrRTOS fill:#fff3e0
    style AppTasks fill:#fff3e0
    style RPMsgServices fill:#fff3e0
    style DebugInterface fill:#fff3e0
    
    %% OCI Images styles
    style BaseImage fill:#fce4ec
    style HelloWorldApp fill:#fce4ec
    style DebugApp fill:#fce4ec
    
    %% Annotation styles (configuration data, not components)
    style OSAnnotation fill:#f5f5f5,stroke:#999,stroke-dasharray: 5 5
    style FirmwareAnnotation fill:#f5f5f5,stroke:#999,stroke-dasharray: 5 5
```



### DEMO 0.2 0623 expanded
> shim : Converter(parer) + runtime 两个部分我们会整合在一起

```mermaid
%%{init: {'theme':'auto', 'flowchart':{'nodeSpacing': 15, 'rankSpacing': 25, 'curve': 'basis'}}}%%
flowchart TD
    subgraph ContainerEco ["Container Ecosystem"]
        Containerd[containerd] --> |ttrpc/gRPC|MicaShim[mica-runtime shimv2]
        Kubernetes[Kubernetes] --> |CRI gRPC|Containerd
        CtrCLI[ctr CLI] --> |ttrpc/gRPC|Containerd
    end
    
    subgraph OCIImages ["OCI Images Sample Bundle"]
        BaseImage[zephyr-mica-scratch:1.0.0]
        BaseImage --> |extends|DebugApp[debug-enabled-zephyr]
        BaseImage --> |extends|HelloWorldApp[hello-world-zephyr]
    end
    
    subgraph MicaRuntime ["MICA Runtime"]
        BundleParser
        MicaShim --> RuntimeService[MicaRuntimeService]
        RuntimeService --> MicaClient[libMica]
        RuntimeService --> Components[Components...]
    end
    
    subgraph MicaDaemonSG ["MICA Daemon & Core (Linux Host)"]
        MicaDaemon[Micad]
        MicaClient --> |UnixSocket IPC|MicaDaemon
        MicaDaemon --> SocketListener[Socket Listener]
        MicaDaemon --> MicaCore[MICA Core Library]
        MicaDaemon --> LinuxRPMsgServices[RPMsg Clients<br/>PTY/RPC/Debug]
        
        MicaCore --> RemoteProcCore[remoteproc_core]
        RemoteProcCore --> BaremetalRproc[baremetal_rproc]
        RemoteProcCore --> JailhouseRproc[jailhouse_rproc]
        BaremetalRproc --> McsDevice["/dev/mcs"]
        JailhouseRproc --> JailhouseHypervisor[Jailhouse Hypervisor]
    end
    
    subgraph CommunicationInfra ["Communication Infrastructure"]
        SharedMemoryHW[Shared Memory<br/>Physical RAM]
        RPMsgProtocol[RPMsg/VirtIO Protocol<br/>Bidirectional]
    end
    
    subgraph RTOSCore ["RTOS Core (Remote CPU)"]
        RTOSResourceTable[Resource Tables<br/>Embedded in Firmware]
        McsDevice --> CPUControl[CPU Control]
        McsDevice --> MemoryMapping[Memory Mapping]
        McsDevice --> IPIInterrupts[IPI/Interrupts]
        JailhouseHypervisor --> ZephyrRTOS[Zephyr RTOS]
        CPUControl --> ZephyrRTOS
        MemoryMapping --> ZephyrRTOS
        IPIInterrupts --> ZephyrRTOS
        ZephyrRTOS --> AppTasks[Application Tasks]
        ZephyrRTOS --> RTOSRPMsgServices[RPMsg Servers<br/>PTY/RPC/Debug]
        ZephyrRTOS --> DebugInterface[Debug Interface]
        RTOSResourceTable --> ZephyrRTOS
    end
    
    %% Communication connections
    MicaCore <--> |mmap access|SharedMemoryHW
    ZephyrRTOS <--> |direct access|SharedMemoryHW
    LinuxRPMsgServices <--> |via VirtIO|RPMsgProtocol
    RTOSRPMsgServices <--> |via VirtIO|RPMsgProtocol
    MicaCore --> |parse|RTOSResourceTable
    
    subgraph Annotations ["Annotations"]
        OSAnnotation{{"org.openeuler.mica.client.os"}}
        FirmwareAnnotation{{"org.openeuler.mica.client.firmware"}}
        CPUAnnotation{{"org.openeuler.mica.client.cpu"}}
        AutobootAnnotation{{"org.openeuler.mica.client.autoboot"}}
    end
    
    HelloWorldApp -.-> Annotations
    MicaShim -.-> BundleParser[OCI Bundle Parser]
    BundleParser -.-> Annotations
    BundleParser -.-> Components
    
    %% Container Ecosystem styles
    style Containerd fill:#e1f5fe
    
    %% MICA Runtime styles (consistent for all runtime components)
    style MicaShim fill:#f3e5f5
    style RuntimeService fill:#f3e5f5
    style MicaClient fill:#f3e5f5
    style Components fill:#f3e5f5

    
    %% MICA Daemon styles
    style MicaDaemon fill:#e8f5e8
    style SocketListener fill:#e8f5e8
    style MicaCore fill:#e8f5e8
    style LinuxRPMsgServices fill:#e8f5e8
    
    %% RemoteProc Layer styles
    style RemoteProcCore fill:#f0f4c3
    style BaremetalRproc fill:#f0f4c3
    style JailhouseRproc fill:#f0f4c3
    
    %% Kernel Interface styles
    style McsDevice fill:#ffccbc
    style JailhouseHypervisor fill:#ffccbc
    style CPUControl fill:#ffccbc
    style MemoryMapping fill:#ffccbc
    style IPIInterrupts fill:#ffccbc
    
    %% Communication Infrastructure styles
    style SharedMemoryHW fill:#e0f2f1
    style RPMsgProtocol fill:#e0f2f1
    
    %% RTOS Core styles
    style ZephyrRTOS fill:#fff3e0
    style AppTasks fill:#fff3e0
    style RTOSRPMsgServices fill:#fff3e0
    style DebugInterface fill:#fff3e0
    style RTOSResourceTable fill:#fff3e0
    
    %% OCI Images styles
    style BaseImage fill:#fce4ec
    style HelloWorldApp fill:#fce4ec
    style DebugApp fill:#fce4ec
    
    %% Annotation styles (configuration data, not components)
    style OSAnnotation fill:#f5f5f5,stroke:#999,stroke-dasharray: 5 5
    style FirmwareAnnotation fill:#f5f5f5,stroke:#999,stroke-dasharray: 5 5
    style CPUAnnotation fill:#f5f5f5,stroke:#999,stroke-dasharray: 5 5
    style AutobootAnnotation fill:#f5f5f5,stroke:#999,stroke-dasharray: 5 5
```

## Profiling

### Runtime pprof server
When mica-shim is started with the environment variable `MICA_SHIM_PPROF=1` it automatically starts a Go *pprof* HTTP server on `localhost:6060` (override with `MICA_SHIM_PPROF_ADDR`).  The server exposes the standard Go debug endpoints, e.g.:

* `/debug/pprof/profile?seconds=30` – CPU profile sample
* `/debug/pprof/heap` – Heap profile

This works for both debug and production builds and does **not** change normal shim behaviour – the handler runs in a background goroutine.

### One-shot flame-graph via Make
For a quick, automated CPU profile and SVG flame-graph you can simply run:

```bash
make profile-flame        # debug build
# or
make profile-flame-prod   # production build
```

The target will:
1. build the chosen binary;
2. start it in the background with pprof enabled;
3. collect a 30-second CPU profile through the HTTP endpoint; and
4. save the rendered SVG flame graph to `flamegraph.svg` before shutting the shim down.

For library-level benchmarks inside the `cntr` package you can also run:

```bash
make profile-cntr
```

which produces `cntr_flamegraph.svg` from the internal benchmark harness.

> ℹ️ The flame-graph generation relies on `go tool pprof` and Graphviz (`dot`). Make sure they are installed and in your `PATH`.
