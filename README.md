# Progress

- [x] runc style 验证
- [x] shim v2 框架
- [] libmica 的 通信和通信抽象
- [] libmica 的任务配置对接
- [] 确定Linux:RTOS clientos 1:1模型的必要性

# Roadmap

1. 能通过 isula 拉起 一个 dummy 镜像
2. 提供一个mica-from-scrach基础镜像,根据这个镜像来搭建混部容器镜像
3. 

# 近期issues

* kata container runtime: Why Rust? 在已经有一个runtime-golang的情况下为什么要开发runtime-rs? 对我们是否有启发
* 需要提供请求转发吗？(--runtime=io.containerd.mica.v?，但是不是混部容器的情况)
> 如果要提供，那么我们转发给谁？（配置）
* shim和runtime是否分离, runtime是否划到micad scope?
> 我打算合并shim&runtime, 这会使shim和runtime的实现更加自由；并且shim&runtime调试可以独立于mica的编译
* init process 我们要不要实现？
> demo中我们跑着一个init process，想用它来 "代表" client OS 本身的状态
* 我们是否需要reaper?
> 不论containerd 是否重启，我们的client OS在运行上和shim， containerd都没亲子关系，完全是跑在另一个核上的由mica管理的实例
* 

# 近期TODOs (优先级)

## 非功能性调整
1. 调整logger模块：
  1. 去掉LocateDebugf等，全部作为 Debugf:Debugf会同时给containerd;mica shim logFile;stdout都输出；但内容格式不同
-[] libmica 接口暴露过多，应减少，并且提供更好的抽象
-[] containerd_client 对mica-shim runtime运行

## 核心添加
1. demo 添加：bundle 解析 
2. 


* 版本
*  containerd 1.7是containerd v1的末版本，1.7内部出现了明显的API变动，下一步先调整API到1.7.3之后的API
* libmica接口暴露调整为 Create, Stop, Rm, Delete ，其他都改为private
* package logger 调整
* replace all Unix process handler ==> rtos process monitor

TYPOS:
* micad会先响应一个"No such file"?
BUG
* fix mock_micad memory leaking...

# architecture (unstable)

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
        theme: 'auto', // 自动适配日/夜间模式
        fontFamily: 'inherit' // 继承文章字体
    });
</script>


# FUTURE
* containerd 2.0 (shim-v3)

TODO

* using XMake to manage the building system