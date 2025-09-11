# fatals

- [x] bundle/rootfs/rootfs, 这里多了一层
- [x] bundle rootfs mount空
- [x]. 初始的时候，仍然尝试restore sandbox,这肯定会导致错误的。
- [x] cleanupContainer时，sandbox state.json 不存在时的错误——根本没创建起来?
- [x] shimService.Cleanup仍然遇到state.json问题
- [x] network type mismatched
- [x] newContainer empty container id arguments incorrect
- [x] shim.create.createSandbox len(containers != 1)
- [x] xen pedestal requires a image*.bin file for boot
- [x] containerPath not resolved!
- [x] container create stuck after forward events -- 
- [x] libmica.stop 在socket不存在时，不应该错误(workaround)
- [x] 状态一致性问题严重
- [x] stop没有发送libmica.stop
- [x] libmica.start 没有发送出去
- [x] `ctr task start demo` 卡住
- [x] duplciated config parsing
- [x] sandbox state.json not cleaned -- after delete container
- [ ] sandbox pod net namespace规划
- [ ] ==我们需要MICA monitor!!==
- [ ] kill如果正常执行，可以保证**幂等**，然而如果kill的是UNKNOWN状态的容器，会 `ontainer not found, mark status to UNKNOWN`
- [ ] pod container corner cases
- [ ] kubectl pods status inconsistency
- [ ] drop-in configs 解析顺序不当，override的优先级用最新的  docs/arch.md的设计来做
- [x] cpu request, (oci spec cpu.share)
- [ ] memory 监控
- [ ] annotation downfall example
- [ ] k8s example
- [ ] show cadvisor CPU Topology
- [ ] service::Stats 完整实现


# flaws and features

- [ ] Xen API: 将对xen相关二进制的调用都走 xapi  [go-xen-api](https://github.com/terra-farm/go-xen-api-client)
> metrics support
> ...
- [ ] fields in ContainerConfig are overlapped, it would be clearer that migrate fields like `cpuShares`, `cpuset` to pedestal EssentialResources
- [ ] micad status, micran status (in-memory), containerd status 一致性 **潜在风险**
- [ ] VCPU Pinning 模式： 打开后， CPUs非空，默认CPUs为空，无限制绑核,绑核后 VCPUs=numCPUSet
> 目前实现是： initResource中 ContainerSize::CPU = numCPU = ceil(quota/period) = inited.VCPUs; CPUs=""
> if EnableVCPUPinning, then VCPUS = num(CPUSet); CPUs = CPUSet
- [ ] duplicated OCI Resource config parsing when `ContainerConfig()` and `CalculatingContainerResource()`
- [ ] Lazy&Once的q:全局变量优化：HostPedType, etc... 开销太大了 (e.g. hostpedtype 目前可以加速十倍)
- [ ] 优化镜像配置、sandbox的设置逻辑:annotation => file => default => bundle, ordered by priority; bundle 作为 校验使用。annotation的必须要和 bundle 中解析出来的一致

- [ ] sandbox Update Resource: parse cpuset when updating!
- [ ] 将Sandbox作为一个Pool:
> for xen, sandbox cpu range => sandbox cpu pool
> sandbox memory is a memory pool
- [x] 圆整和同步问题：
> baremetal中都是取整，那么multi-shim记录就需要使用这个取整后的值, xen同理.
> vcpu统一解决：默认圆整cpucapacity
- [x] vcpu映射决策
> 默认的 maxcpus 为多少？计算的数据从从哪来？
> 初始online的vcpu在 `CalculateResource` 中计算，
> vcpu默认是否需要和CPUCapacity持协变? —— 目前来看，不必要，但是在初始化资源的时候，可以一致：init vcpus: 
> vcpus = ceil(CPUCapacity);
> vcpus是否和CPUset 保持一致？当开启VCPUPinning
> **当update cpuset**时，通过annotation, 可以允许vcpus绑到
> 增加 annotation: VcpuNumCoordinateCpuset 来打开 vcpus
- [x] basic convert cpu resource calculation：
> host bind cpu(default=physical core0)
> CPUWeight:XEN-MICA = int((cpushares / (1024/256)))
> make sure that xen is using credit2 or credit1
> CPUCapacity:XEN-MICA = int((CpuQuota / CpuPeriod) * 100)
> CPU:XEN-MICA = CPUs = Convert(cpusetcpus);
> Start the guest with N vCPUs initially online.
> VCPU:XEN-MICA is the cpus DomU aware of, = ceil(CPUCapacity) 
> 需要和CPUs做同步(vCPU=num(CPUs))
- [x] 确认以下cpuset的可分配范围，于集群/containerd来说——是否只从linux host获得？
- [x] workaround： pause and resume 
- [ ] future: mica pause (目前是直接调xl, for ped=Xen)
- [x] containerd metric support
- [x] delete 时bundle位置不当，validMicantainer失败
- [x] pause -> stop ; resume -> start 的转发
- [x]. sandbox.Restore() 空指针解引用
- [x] basic fifo support
- [x] io support shell-like interaction(mock_test: /tmp/dev/PRMSG_TEST)
- [ ] io support for real case
- [x] ctr t task demo: [2] empty container id: unknown
- [x] `ctr t start demo` invalid state: runnign (expected: ready)
- [x] bundle rootfs 全空
- [x] clientpath这一项解析为空
- [x] clientpath firmwarepath的值如果是绝对路径开头的，要处理掉
- [ ] sandbox storage 应该存储更加有意义的信息，分离无状态信息和状态信息
- [x] sandbox state.json not cleaned --- after shim disconnected
- [x] validMicaContainer before mount Rootfs
- [x] defer nil error: when shim.Kill()
- [x] 新版mock_micad行为错误：received buffer size mismatch  with struct create message
- [ ] cgroup cpu.cfg_quota_ua / cpu.cfg_period_us for baremetal (ceil or floor for baremetal)
- [x] task entry Update not implemented : 配合扩容
- [ ] container update not implemented: recalculate resource, realloca resources; 资源伸缩的真实实现
- [ ] CNI support
> [k8s cri api](pkg/apis/runtime/v1/api.proto)
- [ ] `UpdateTaskRequest::Resource` is of anypb type, !!! 类型转为 specs.LinuxResource是我们的一个乐观假设(通常情况下)，事实上这是可以扩展, e.g.:
```go
switch req.Resources.TypeUrl {
case "types.containerd.io/LinuxResources":
case "vendor.example.com/GPUResources":
    // 自定义 GPU 资源
  }

// k8s proto:
message UpdateContainerResourcesRequest {
    // ID of the container to update.
    string container_id = 1;
    // Resource configuration specific to Linux containers.
    LinuxContainerResources linux = 2;
    // Resource configuration specific to Windows containers.
    WindowsContainerResources windows = 3;
    // Unstructured key-value map holding arbitrary additional information for
    // container resources updating. This can be used for specifying experimental
    // resources to update or other options to use when updating the container.
    map<string, string> annotations = 4;
}
```

- [ ] pedestal结构调整：

```console
├── acrn
├── interfaces.go
├── jailhouse
│   ├── j_metrics.go
│   └── resource_mng.go
├── openamp
│   ├── openamp_metrics.go
│   └── resource_mng.go
├── types.go
└── xen
    ├── cmds_and_parser.go
    ├── xapi.go
    └── xen_metrics.go
```

```go
type PedTraits interface {
    PinVCPU()
    Statistics()
    ToString()
    GeneratePedCfg()
    UpdateResource()
}
```

- [ ] 在核心包中去除对pedestal的依赖，将它们全部仅指向为libmica的依赖;
- [ ] 让镜像所需的 rpmsg, 探测等组件都作为一个agent。统一构建出来


# performance


- [x] ParseConfigINI