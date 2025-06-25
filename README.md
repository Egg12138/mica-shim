<!-- @import "[TOC]" {cmd="toc" depthFrom=1 depthTo=6 orderedList=false} -->

<!-- code_chunk_output -->

- [MICA Shim - Containerd Runtime Shim for MICA](#mica-shim---containerd-runtime-shim-for-mica)
  - [Current Progress](#current-progress)
  - [️ Roadmap](#️-roadmap)
  - [近期Issues](#近期issues)
  - [📋 近期TODOs (优先级)](#-近期todos-优先级)
    - [🔧 非功能性调整](#-非功能性调整)
    - [核心添加](#核心添加)
    - [其他事项](#其他事项)
    - [已知问题](#已知问题)
  - [️ Architecture (unstable)](#️-architecture-unstable)
    - [MCS Arch](#mcs-arch)
  - [Future Plans](#future-plans)
    - [yocto 相关](#yocto-相关)
  - [TODO](#todo)
  - [Content flow](#content-flow)
  - [Architecture (History)](#architecture-history)
    - [DEMO 0.2 0623 overview](#demo-02-0623-overview)
    - [DEMO 0.2 0623 expanded](#demo-02-0623-expanded)
    - [DEMO 0.1 0620](#demo-01-0620)

<!-- /code_chunk_output -->


# MICA Shim - Containerd Runtime Shim for MICA

## Current Progress

- [x] runc style 验证
- [x] shim v2 框架
- [x] libmica 的 通信和通信抽象
- [ ] libmica: containerd rpc--> mica config, 任务配置对接
- [ ] 确定Linux:RTOS clientos 1:1模型的必要性

## ️ Roadmap

本项目的 Roadmap 不仅仅是 mica-shim 本身，还包括了部分 mica 侧的追加功能

- [x] 能通过 isula 拉起 一个 dummy 镜像
- [ ] 自如管理 基本镜像的： OS register, OS boot, Task start
- [ ] 提供一个mica-from-scrach基础镜像,根据这个镜像来搭建混部容器镜像,并且可以根据这些镜像拉起服务
- [ ] Client OS 和 Client Task process 的明确分离管理
- [ ] IO 接管
- [ ] 持久化
- [ ] 网络

## 近期Issues

* 最直接关键的问题：**信息 从哪来**：
1. 从bundle来？那多数都是在image侧静态设定；这个镜像需要多少个核
1. entry point
1. 验证pod, (下次用minikube跑一个demo)
1. pod IP;
1. 1 node 1 micad N clients
2. create: CPU需要调度选定的, firmware path 是完全可以静态的——runtime告诉micad 在哪里拿就好了——micad要有权限
3. autoboot：我们需要一个micad hook?
1. 我们现在是利用mica暴露的北向接口来实现。需不需要从南向的虚拟化底座来……
4. reboot: 对于同一个镜像，同一个task，专门化的reboot代替Stop() + Start()会节省开销吗?
5. 1:1的一个想法是用对应的init process来监控client OS本身的信息 (N:N:1 , N个容器，N个monitor process, 对应一个micad monitor)
* kata container runtime: Why Rust? 在已经有一个runtime-golang的情况下为什么要开发runtime-rs? 对我们是否有启发
* 需要提供请求转发吗？(--runtime=io.containerd.mica.v?，但不是混部容器的情况)
  > 如果要提供，那么我们转发给谁？（配置）, 如果不转发，我们需要让错误处理更加优雅
* shim和runtime是否分离, runtime是否划到micad scope?
  > 我打算合并shim&runtime, 这会使shim和runtime的实现更加自由；并且shim&runtime调试可以独立于mica的编译
* init process 我们要不要实现？
  > demo中我们跑着一个init process，想用它来 "代表" client OS 本身的状态
* 我们是否需要reaper?
  > 不论containerd 是否重启，我们的client OS在运行上和shim， containerd都没亲子关系，完全是跑在另一个核上的由mica管理的实例

## 📋 近期TODOs (优先级)

### 🔧 非功能性调整

- [ ] 调整logger模块：
  - [ ] 去掉LocateDebugf等，全部作为 Debugf:Debugf会同时给containerd;mica shim logFile;stdout都输出；但内容格式不同
- [ ] libmica 接口暴露过多，应减少，并且提供更好的抽象
- [x] containerd_client 对mica-shim runtime运行
- [ ] 优雅的错误处理

###  核心添加

1. shim API， 完整参数处理:
    1. containerd -> shim -> Create() -> create()
1. demo 添加：bundle 解析:
   1. OCI zephyr-scratch 镜像
   2. fetch information from bundle 
1.  container events
1. pod IP


###  其他事项

* 版本
* containerd 1.7是containerd v1的末版本，1.7内部出现了明显的API变动，下一步先调整API到1.7.3之后的API
* libmica接口暴露调整为 Create, Stop, Rm, Delete ，其他都改为private
* package logger 调整
* replace all Unix process handler ==> rtos process monitor

###  已知问题

**TYPOS:**
* micad会先响应一个"No such file"?

**BUG:**
* fix mock_micad memory leaking...

## ️ Architecture (unstable)

### MCS Arch

```
Linux Host Core (ARM Core 0)         RTOS Remote Core (ARM Core 1)
┌─────────────────────────────┐      ┌─────────────────────────────┐
│  micad (User Space)         │      │  Zephyr RTOS (Kernel)       │
│  ├─ RPMsg PTY Client ───────┼──────┼─ RPMsg PTY Server           │
│  ├─ RPMsg RPC Client ───────┼──────┼─ RPMsg RPC Server           │
│  ├─ RPMsg Debug Client ─────┼──────┼─ RPMsg Debug Server         │
│  └─ OpenAMP Library         │      │  └─ OpenAMP Library         │
├─────────────────────────────┤      ├─────────────────────────────┤
│  Kernel Space               │      │  Resource Table (Static)    │
│  ├─ /dev/mcs device         │      │  ├─ Memory regions          │
│  └─ Memory management       │      │  ├─ VirtIO devices          │
└─────────────────────────────┘      │  └─ Endpoint definitions    │
               |                     └─────────────────────────────┘
               |                                      | 
         ┌─────▼──────────────────────────────────────▼───┐
         │  Physical Shared Memory                        │
         │  ├─ VirtQueue Rings (RPMsg transport)          │
         │  ├─ Buffer Pools (Message data)                │
         │  └─ Control Structures (Status, locks)         │
         └────────────────────────────────────────────────┘
```

## Future Plans

* containerd 2.0 (shim-v3)

### yocto 相关

* isulad yocto 需要跟随版本（k8s上游版本）;
    * enable CRI V1(>=CRI 1.26)
```json
{
"group": "isula",
"default-runtime": "runc",
...
"enable-cri-v1": true
}
```
    * 开启`ENABLE_CRI_API_V1` flag: `cmake ../ -D ENABLE_CRI_API_V1`
* yocto: speed up the building of iSulad Shimv2? (其实也就是比用prebuild慢了几秒)
  > 预定考虑的策略： remove debug-info; faster linker; ...
* oebuild 加入 docker, 在构建时手动构建特定rtos的scratch镜像

## TODO

* using XMake to manage the building system ()

## Content flow

## Architecture (History)

### DEMO 0.2 0623 overview

mica-shim
::package core
    :: shim
    :: runtime
    :: bundleParser

<div class="mermaid">
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
</div>
<script src="https://cdn.jsdelivr.net/npm/mermaid/dist/mermaid.min.js"></script>
<script>
    mermaid.initialize({
        startOnLoad: true,
        theme: 'auto', 
        fontFamily: 'inherit' 
    });
</script>



### DEMO 0.2 0623 expanded
> shim : Converter(parer) + runtime 两个部分我们会整合在一起
<div class="mermaid">
%%{init: {'theme':'auto', 'flowchart':{'nodeSpacing': 15, 'rankSpacing': 25, 'curve': 'basis'}}}%%
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
</div>
<script src="https://cdn.jsdelivr.net/npm/mermaid/dist/mermaid.min.js"></script>
<script>
    mermaid.initialize({
        startOnLoad: true,
        theme: 'auto', 
        fontFamily: 'inherit' 
    });
</script>

### DEMO 0.1 0620

<div class="mermaid">
%%{init: {'theme':'auto'}}%%
graph TB
    CD[containerd] ==>|ttrpc/gRPC| S
    S[mica-shim] ==> MR
    MR[mica-runtime（目前跟shim合并在一起）] -->|IPC| A[micad]
    MR -->C[OCI Bundle,RTOS image]
    MR ==>|containerd shim.Command will call a Linux process|AG[RTOS Agent process]
    A -->|RPMsg| B[RTOS Remote Core]
    C -.->|annotation matched| MR
    AG <--> |1:1对应|D
    subgraph RTOS Side 来自bundle
        RT[resource_table]
        K[RTOS kernel]
        B[libmetal/openAMP/...]
        D[RTOS fs] ==> E
        D --> L[libs]
        D --> B
        E[RTOS task workspace]
    end

    subgraph mica daemon scope
        A
        MR
        AG
    end
</div>
<script src="https://cdn.jsdelivr.net/npm/mermaid/dist/mermaid.min.js"></script>
<script>
    mermaid.initialize({
        startOnLoad: true,
        theme: 'auto', 
        fontFamily: 'inherit' 
    });
</script>
