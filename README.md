<!-- @import "[TOC]" {cmd="toc" depthFrom=1 depthTo=6 orderedList=false} -->

<!-- code_chunk_output -->

- [MICA Shim - Containerd Runtime Shim for Runtime Micran](#mica-shim---containerd-runtime-shim-for-mica)
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

<!-- /code_chunk_output -->


# Micran - Containerd Runtime Shim for MICA

## Current Progress

- [x] runc style 验证
- [x] shim v2 框架
- [x] libmica 的 通信和通信抽象
- [x] libmica: containerd rpc--> mica config, 任务配置对接整理
- [x] 确定Linux:RTOS clientos 1:1模型的必要性
- [x] mica支持长容器ID
- [x] basic micran state storage
- [x] 完善micran state存储
> 由于我们在create中不再创建mica client而是放到 start()中，所以我们创建容器后需要存放状态信息，目前有几个考虑，分三步：
> 1. 双state.json副本, 一个位于bundle, 一个位于micran state dir
> 1. 在micran state dir中维护一个db,存储state信息，提高性能
> 1. 整个bundle都转到 micran state dir
- [x] 从struct manager 迁移到struct shim
- [x] 重构项目，提高维护性
- [x] SCHED_CORE 支持
- [ ] 分析对1:1:1模型中 r.ExecID的处理：确认会传什么，确认是否要转化为对容器的操作
- [ ] 对Xen做适配
- [x] task events all completed
- [x] io mock test
- [ ] pty
- [x] k8s Pod, Sandbox support
- [x] 镜像规制考虑
- [x] Xen basic interface
- [ ] use vendor

## ️ Roadmap

本项目的 Roadmap 不仅仅是 mica-shim 本身，还包括了部分 mica 侧的追加功能

- [x] 能通过 isula 拉起 一个 dummy 镜像
- [x] 自如管理 基本镜像的： OS register, OS boot, Task start
- [x] 提供一个mica-from-scrach基础镜像,根据这个镜像来搭建混部容器镜像,并且可以根据这些镜像拉起服务
- [x] Client OS 和 Shim Task process 的明确分离管理
- [x] IO 接管
- [x] sandbox管理containers
- [ ] RTOS生命周期的监控：task exited 如何让micran或者mica感知
- [ ] k8s打通
- [ ] 持久化
- [ ] 网络
- [ ] 保证mica daemon 和 micran 生命周期不一致时容器状态的一致性
- [ ] k8s deployment demo
- [ ] kubeedge deployment demo
- [x] libmica 不恰当地参与了service等packages,需要再做解耦

## 近期Issues

1. 验证pod, (下次用minikube跑一个demo)
1. pod IP;
1. 探讨：暴露RTOS跑在哪个CPU上
    1. Downward API是有的
    2. Upward API 有吗？
1. 暂时使用简单的调度方式：
1. shimv2可能会有很多实例（并不是单shim的）；所以RTOS核调度应该在系统中有一个.lock OR 共享内存（我们最后选择用共享内存）
3. autoboot：我们需要一个micad hook?
1. 我们现在是利用mica暴露的北向接口来实现。需不需要从南向的虚拟化底座来……
4. reboot: 对于同一个镜像，同一个task，专门化的reboot代替Stop() + Start()会节省开销吗?
5. 1:1的一个想法是用对应的init process来监控client OS本身的信息 (N:N:1 , N个容器，N个monitor process, 对应一个micad monitor)
- [ ] ~~镜像规制1:`${RTOS}_APP:{VERSION}`~~
    1. k8s侧基于 ped=xxx 来选择不同image, 有可能吗？runtime不应该做这件事！ -- 虽然我们可以：
      1. k8s pod apply : ped=zen, image=zephy:latest
      2. runtime resolve image annotations 
      3. call contaienrd client.GetImage(zephy-{ped}:latest) 
      可以，但这样非常不好
- [x] 镜像规制2: `{Registry}/{APP}-micran-{PED}:{VERSION}`: localhost:5000/zephyr-hello-world-micran-jailhouse:1.0  
    1. 如果k8s pod指定错了BSP怎么办？ micran 会检查 Ped 匹配性
    1. 更多信息应该封装到rootfs/client.conf中

* 一些很侵入式的特殊行为，我认为应该分离出来，作为mica runtime的plugin

* 需要提供请求转发吗？(--runtime=io.containerd.mica.v?，但不是混部容器的情况)
  > 如果要提供，那么我们转发给谁？（配置）, 如果不转发，我们需要让错误处理更加优雅
* shim和runtime是否分离, runtime是否划到micad scope?
  > 我打算合并shim&runtime, 这会使shim和runtime的实现更加自由；并且shim&runtime调试可以独立于mica的编译
* init process 我们要不要实现？
  > 未来需要，现在不需要

* 我们是否需要reaper?
  > 不论containerd 是否重启，我们的client OS在运行上和shim， containerd都没亲子关系，完全是跑在另一个核上的由mica管理的实例

* 未来的整合工作：
    1. 分离 runtime + shim
    1. runtime的归属
        1. runtime 整合到 mica, 作为一个独立模块, 简短一个通讯链路
        1. 作为一个独立模块但用Rust重写？Rust相比C会更适配云原生底座，性能和体积上比go适合嵌入式

## 资源生命周期分析与调整策略


## 📋 近期TODOs (优先级)

### 🔧 非功能性调整

- [x] 调整logger模块：
  - [x] LocateDebugf -> FDebugf
  - [x] 完全去掉LocateDebugf等，全部作为 Debugf:Debugf会同时给containerd; micran logFile;stdout都输出；但内容格式不同
- [x] libmica 接口暴露过多，应减少，并且提供更好的抽象
- [x] containerd_client 对micran运行
- [x] 优雅的错误处理
- [x] migrate to containerd 1.7.27
- [x] container 相关结构语义化明确，减小耦合



###  核心添加

1. shim API， 全对接
    - [x] task CreateRequest, task CreateResponse, ...生命周期相关
    - [ ] task Update, Stats 资源管理和监控
1. workarounds: mica后续会逐渐提供以下问题的原生支持
    - [x] pause and resume, not via Mica, 
    - [ ] MICA支持pause and resume
    - [ ] update
    - [ ] MICA支持update: `set Memory`, `set CPUs`
    - [x] Metrics workaround: 1. dummy stats
    - [ ] Metrics workaround: 2. via Xen api
    - [ ] Metrics workaround: 3. via 

1. ContainerConfig解析
    - [x] 时序ok
    - [ ] annotations 有效性验证


1. demo 添加：bundle 解析:
    - [x] OCI zephyr-scratch 镜像
    - [x] fetch information from bundle 
    - [x] 分配可用(目前不支持多核)CPU给clientOS
    - [x] 改用ocispec annotation + rootfs conf双解析，保险起见
1. image build tool 
    - [x] 独立可用；一键构建
    - [ ] 继承到yocto 
    - [ ] yocto对于xen-mica的情况需要编image.bin
1. 核分配完善:
    - [ ] 异常 CPU 
    - [ ] 可分配核池
    - [ ] Pod Sandbox CPU 归入 Pool
1. IO
    - [x] basic work
    - [ ] 完全连通性
    - [ ] pty 实现
1. network
1. **保留Linux上使用容器的能力**

- [x] container events
1. pod IP
- [x] client sock收回的问题；


###  其他事项

* 版本
* k8s接入的问题分析
* log轻型化

