# Architecture 

### Latest

overview
![overview](./images/micashim-overview0623.png)

expanded
![expanded](./images/micashim-expaned0623.png)

### Cloud-Edge Architecture Overview

The following diagram shows mica-shim's role in a Kubernetes/KubeEdge environment:

```mermaid
%%{init: {'theme':'auto', 'flowchart':{'nodeSpacing': 15, 'rankSpacing': 30, 'curve': 'basis'}}}%%
flowchart TB
    Cloud[k8s/k3s in edge] --> Kubectl
    Kubeedge[kubeedge edgecore] --> Kubectl
    Kubectl[Kubelet on edge] -->|CRI| Edge[Containerd/iSulad]
    Edge -->|shimv2| MicaShim[mica-runtime]
   
    subgraph mica
        MicaShim --> Micad[Micad]
    end

    subgraph rtos
        Micad --> RTOS
        RTOS[RTOS on CPUs]
    end
    classDef cloud fill:#e1f5fe;
    classDef edge fill:#e8f5e8;
    classDef component fill:#f3e5f5;
    classDef core fill:#f0088f40;
    
    class Kubectl cloud;
    class Kubeedge cloud;
    class Cloud cloud;
    class Edge,Kubectl edge;
    class MicaShim core;
    class Micad,RTOS component;
```

#f0088f8c




### Key Components
1. **Kubernetes Cluster**: Manages container deployments
2. **Edge Nodes**: Run container workloads
3. **mica-shim**: Converts container requests to RTOS operations
4. **Micad**: MICA daemon managing RTOS instances
5. **RTOS**: Real-time OS instances on dedicated CPUs

### Usage Case: 3-CPU Edge Device

The following diagram shows a specific deployment scenario with 4 CPUs:

```mermaid
%%{init:{
  'theme':'auto',
  'flowchart':{
    'nodeSpacing':10,
    'rankSpacing':30,
    'curve':'basis'
}}}%%

flowchart LR
    Cloud["Cloud"] -->Edgecore

    subgraph ControlCPU["CPU 0 - Control"]
        Edgecore-->iSulad
        iSulad[iSulad]-->Mica
        Mica[MicaRuntime]
    end

    subgraph RTOSCPUs["CPUs 1-2 - RTOS"]
        direction LR          %% 让内部节点横向排布
        Mica -->RTOS1[RTOS1]
        Mica -->RTOS2[RTOS2]

        RTOS1 -.->Mica
        RTOS2 -.->Mica

    end

    classDef cloud fill:#e1f5fe
    classDef edge   fill:#e8f5e8
    classDef rtos   fill:#f3e5f5
    classDef core   fill:#f0088f40

    class RTOS1,RTOS2,RTOS3 rtos
    class Cloud cloud
    class Mica core
```

## containerd

### in-Eco role

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
        AutobootAnnotation
