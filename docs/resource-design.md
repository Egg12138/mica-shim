# Resource Design

## cgriyo

这是一个非常专业且深入的底层问题。要准确回答这个问题，我们需要厘清 **Kubernetes (K8s)**、**Containerd (shimv2)**、**OCI Spec** 以及 **runc (libcontainer/cgroup)** 之间的职责边界。

以下是对 `runc` 行为、CPU 资源调度及计算公式的深度分析：

---

### 1. Runc 的默认行为与 VCPU 数量
**结论：Runc 本身不“思考” VCPU 的数量，它只是 Cgroups 的执行者。**

* **默认行为（无限制）：** 如果 OCI Spec（容器配置文件）中没有指定 CPU 限制，`runc` 不会去探测 VCPU 数量来人为设定限制。此时，容器内的进程可以使用宿主机（或 VM）上**所有**可用的 CPU 核心。
* **VCPU 的角色：** 对于 `runc` 而言，它看到的是 Linux 内核呈现的“逻辑核心”。无论是在物理机上还是在虚拟机（VM）里，内核识别到的核心数就是 `runc` 能调用的上限。
* **Shimv2 的角色：** `shimv2` (如 `io.containerd.runc.v2`) 的作用是将 containerd 的请求转化为 `runc` 命令。它本身不决定策略，只是忠实传递 K8s 计算好并写入 `config.json` (OCI Spec) 的参数。

---

### 2. Runc 对 Cpuset (Pinning) 和 Quota/Period 的处理

`runc` 完全依赖 Linux Cgroups (v1 或 v2) 来实现资源隔离。

#### A. Cpuset (Pinning)
* **是否生效：** 是的。
* **机制：** 当 OCI Spec 中指定了 `linux.resources.cpu.cpus` 时，`runc` 会将其写入 Cgroup 的 `cpuset.cpus` 文件。
* **效果：** 这是一个**硬亲和性（Hard Affinity）**设置。容器内的进程**只能**被调度器安排在指定的这些核心上运行，绝对不会漂移到其他核心。

#### B. Quota/Period (算力时间)
* **是否生效：** 是的。
* **机制：** `runc` 会读取 OCI Spec 中的 `quota` 和 `period`，并写入 Cgroup：
    * **Cgroup v1:** 写入 `cpu.cfs_quota_us` 和 `cpu.cfs_period_us`。
    * **Cgroup v2:** 写入 `cpu.max` (格式为 `quota period`)。
* **效果：** 这是 CFS (Completely Fair Scheduler) 的带宽控制。它决定了在一段 `period` 时间内，该容器能消耗多少 CPU 时间片。

---

### 3. OCI Spec 中的字段与共存关系

#### Q: `cpulimit` 和 `cpu quota`, `period` 会同时在 oci.spec 中被指定吗？

**准确回答：不会，因为 OCI Spec 中根本没有 `cpulimit` 这个字段。**

这里有一个常见的概念混淆：
* **Kubernetes 层：** 用户定义 `resources.limits.cpu` (俗称 CPU Limit)。
* **转换层 (Kubelet/Containerd)：** K8s 会根据 Limit 值计算出 `quota`。通常 `period` 默认为 100ms (100000us)。
    * 例如：Limit = 0.5 core -> Quota = 50000, Period = 100000。
* **OCI Spec 层 (Runc 看到的)：** `config.json` 文件中只有标准的 Linux Cgroup 参数字段：
    * `linux.resources.cpu.quota`
    * `linux.resources.cpu.period`
    * `linux.resources.cpu.shares` (对应 K8s 的 requests)
    * `linux.resources.cpu.cpus` (对应 cpuset)

**Runc 的对待方式：**
Runc 只认 OCI 标准字段。它看到 `quota` 和 `period` 就会去设置 CFS 带宽控制；看到 `cpus` 就会去设置 Cpuset。这两者可以在 OCI Spec 中**共存**，Runc 会同时设置它们。

---

### 4. 最终生效的 CPU 算力时间由什么决定？

这是一个多层约束问题。实际生效的算力受 **CFS Bandwidth (Quota)** 和 **Cpuset (物理/逻辑核数)** 的**双重约束**。

#### 你的算式分析与修正

$$\text{最终生效的 CPU 算力} = \min(\text{可用核心数}, \frac{\text{quota}}{\text{period}}, \text{物理核心数}) \times 100\%$$

**这个算式基本正确，但需要更精确的定义：**

1.  **Quota/Period 比例 (逻辑上限)：**
    这是 CFS 允许的时间片总量。例如 `Quota=200ms`, `Period=100ms`，意味着容器可以在 100ms 的周期内使用 200ms 的 CPU 时间。这等同于 **2.0 个 CPU 的算力**。

2.  **Cpuset (物理瓶颈)：**
    如果容器被 `cpuset` 限制只能在 Core 0 上运行（共 1 个核），即便 Quota 允许它跑 2.0 个核的量，实际上它**物理上最多只能跑到 1.0 个核**（即 100% 单核占用）。

3.  **可用核心数 (系统瓶颈)：**
    如果宿主机负载极高，其他进程占满了 CPU，容器即使有 Quota 和 Cpuset，也抢不到时间片。但这属于“运行时状态”，不属于“配置上限”。

**修正后的精确逻辑（配置上限）：**

$$\text{理论最大算力 (Cores)} = \min\left( \frac{\text{quota}}{\text{period}}, \text{Count}(\text{cpuset.cpus}) \right)$$

**如果未指定 cpuset，则：**
$$\text{理论最大算力 (Cores)} = \min\left( \frac{\text{quota}}{\text{period}}, \text{Host Total Cores} \right)$$

---

### 5. 核心问题：Cpuset 会影响实际的算力占用时间吗？

**答案：非常会，而且是决定性的物理天花板。**

请看以下场景示例：

* **场景 A (无瓶颈)：**
    * Quota/Period = 2.0 (允许使用 2 核)
    * Cpuset = `0-3` (绑定到 4 个核)
    * **结果：** 容器可以跑满 2.0 个核的算力。限制因素是 **Quota**。

* **场景 B (Cpuset 成为瓶颈)：**
    * Quota/Period = 2.0 (允许使用 2 核)
    * Cpuset = `0` (只绑定到核 0)
    * **结果：** 容器死活只能跑到 1.0 个核（核 0 的 100%）。虽然 CFS 给了它 200ms 的票，但它只有一个窗口（核）去兑换，物理上无法在 100ms 周期内通过单核消费 200ms。限制因素是 **Cpuset**。

### 总结

1.  **Runc 行为：** 忠实执行 OCI Spec，根据 Spec 设置 Cgroup 的 `cpu.cfs_quota/period` 和 `cpuset.cpus`。
2.  **OCI 字段：** `cpulimit` 不存在于 Spec 中，只有 `quota` 和 `period`。它们与 `cpuset` 可以共存。
3.  **最终算力决定因素：** 是 **CFS Quota 计算出的逻辑核数** 与 **Cpuset 绑定的物理核数** 之间的**最小值**。
4.  **Cpuset 影响：** `cpuset` 设定了硬性的物理执行单元数量，它会作为“物理天花板”截断 Quota 允许的逻辑算力。



## containerd 资源管理

我们来看这样一个例子：

```shell
# --cpu-quota=50000  -> 允许在 100000 微秒 (period) 内使用 50000 微秒，即 0.5 CPU
# --cpu-period=100000
# --memory-limit=134217728 -> 128 * 1024 * 1024 = 134217728 字节
ctr run \
  --rm \
  --with-ns "pid:" \
  --with-ns "cgroup:" \
  --cpu-quota 50000 \
  --cpu-period 100000 \
  --memory-limit 134217728 \
  docker.io/polinux/stress:latest \
  stress-test-container \
  stress --cpu 2 --vm 1 --vm-bytes 256M
```

### ctr/crictl的控制 选项 / 容器 cgroup 资源绑定 的预期


`--cpu-shares` cgroup cpu.shares; 
对应物： `xen cpu.weight`

micran的cpu亲和性以及算力限制的控制能力的预期，是略逊于 runc 所能做到的程度。我们先评估：
1. runc, cpuset 对性能和核可见性的影响
1. xen, cpu affinity 对性能和核可见性影响 (cpus绑核)
1. runc, cpu-qupta 对性能的影响
1. xen, cpu-qupta 对性能的影响


Linux容器中，我们用Stress-ng来测试:
nerdctl run `--cpus` 是 cpu 数量(quota/period), --cpuset-cpus 是可执行的cpu数，

```console
nerdctl run --rm \
    cpu-workload --cpu 8 --cpu-method matrixprod -t 30s
stress-ng: info:  [1] setting to a 30 second run per stressor
stress-ng: info:  [1] dispatching hogs: 8 cpu
stress-ng: info:  [1] successful run completed in 30.01s

    PID USER      PR  NI    VIRT    RES    SHR S  %CPU  %MEM     TIME+ COMMAND
2082167 root      20   0   76532   2388   1024 R 100.0   0.0   0:05.78 stress-ng
2082170 root      20   0   76532   2388   1024 R 100.0   0.0   0:05.78 stress-ng
2082171 root      20   0   76532   2388   1024 R 100.0   0.0   0:05.78 stress-ng
2082172 root      20   0   76532   2388   1024 R 100.0   0.0   0:05.78 stress-ng
2082173 root      20   0   76532   2388   1024 R 100.0   0.0   0:05.78 stress-ng
2082174 root      20   0   76532   2388   1024 R 100.0   0.0   0:05.78 stress-ng
2082168 root      20   0   76532   2388   1024 R  99.7   0.0   0:05.78 stress-ng
2082169 root      20   0   76532   2388   1024 R  99.7   0.0   0:05.77 stress-ng

nerdctl run --rm --cpuset-cpus="0-3" \
    cpu-workload --cpu 8 --cpu-method matrixprod -t 30s
stress-ng: info:  [1] setting to a 30 second run per stressor
stress-ng: info:  [1] dispatching hogs: 8 cpu
stress-ng: info:  [1] successful run completed in 30.01s

2097277 root      20   0   76532   2388   1024 R  69.1   0.0   0:10.60 stress-ng
2097278 root      20   0   76532   2388   1024 R  68.1   0.0   0:10.26 stress-ng
2097284 root      20   0   76532   2388   1024 R  62.5   0.0   0:10.32 stress-ng
2097283 root      20   0   76532   2388   1024 R  40.5   0.0   0:06.25 stress-ng
2097280 root      20   0   76532   2388   1024 R  40.2   0.0   0:06.26 stress-ng
2097282 root      20   0   76532   2388   1024 R  39.9   0.0   0:06.23 stress-ng
2097281 root      20   0   76532   2388   1024 R  39.5   0.0   0:06.28 stress-ng
2097279 root      20   0   76532   2388   1024 R  39.2   0.0   0:06.14 stress-ng

nerdctl run --rm --cpus="4.0"  \
    cpu-workload --cpu 8 --cpu-method matrixprod -t 30s
stress-ng: info:  [1] setting to a 30 second run per stressor
stress-ng: info:  [1] dispatching hogs: 8 cpu
stress-ng: info:  [1] successful run completed in 30.02s

PID USER      PR  NI    VIRT    RES    SHR S  %CPU  %MEM     TIME+ COMMAND
2085414 root      20   0   76532   2388   1024 R  50.5   0.0   0:02.23 stress-ng
2085420 root      20   0   76532   2388   1024 R  50.5   0.0   0:02.22 stress-ng
2085415 root      20   0   76532   2388   1024 R  50.2   0.0   0:02.22 stress-ng
2085418 root      20   0   76532   2388   1024 R  50.2   0.0   0:02.23 stress-ng
2085419 root      20   0   76532   2388   1024 R  49.8   0.0   0:02.20 stress-ng
2085416 root      20   0   76532   2388   1024 R  49.5   0.0   0:02.21 stress-ng
2085417 root      20   0   76532   2388   1024 R  49.5   0.0   0:02.21 stress-ng
2085421 root      20   0   76532   2388   1024 R  49.2   0.0   0:02.19 stress-ng

```

```
      --cpus float                                     Number of CPUs
      --cpuset-cpus string                             CPUs in which to allow execution (0-3, 0,1)
```

在平均执行用量上，我们发现第二组和第三组等效。但是每一个核的用量并不同； cpus, cpu_quota, cpu_period是CFS的, nproc=host, 
cpuset-cpus

```console
nerdctl run -d --cpus="4.0" --cpuset-cpus="2,4-10" cpu-workload --cpu 6 --cpu-method matrixprod -t 30s

# nproc=8 (2,[4-10])
2113832 root      20   0   76528   2644   1280 R  67.3   0.0   0:05.89 stress-ng
2113834 root      20   0   76528   2388   1024 R  67.3   0.0   0:05.87 stress-ng
2113837 root      20   0   76528   2644   1280 R  67.3   0.0   0:05.84 stress-ng
2113835 root      20   0   76528   2388   1024 R  66.7   0.0   0:05.87 stress-ng
2113833 root      20   0   76528   2388   1024 R  66.3   0.0   0:05.84 stress-ng
2113836 root      20   0   76528   2644   1280 R  66.3   0.0   0:05.82 stress-ng
```

这个场景中，cpus限制了4.0的CFS配额，虽然容器可以在八个核心上面跑，但是只能获得4个核的算力。

反之同理，cpuset也是算力限制：

```console
 nerdctl run -d --cpus="4.0" --cpuset-cpus="2" cpu-workload --cpu 6 --cpu-method matrixprod -t 30s
 2118095 root      20   0   76528   2644   1280 R  16.6   0.0   0:02.24 stress-ng
2118096 root      20   0   76528   2644   1280 R  16.6   0.0   0:02.24 stress-ng
2118097 root      20   0   76528   2644   1280 R  16.6   0.0   0:02.24 stress-ng
2118098 root      20   0   76528   2644   1280 R  16.6   0.0   0:02.24 stress-ng
2118099 root      20   0   76528   2644   1280 R  16.6   0.0   0:02.23 stress-ng
2118100 root      20   0   76528   2644   1280 R  16.6   0.0   0:02.23 stress-ng

```

这六个stress-ng进程都在 cpuid=2 上运行。$400\%$ 的 `--cpus` 是上限，在这里只能用到 $100\%$



`--cpu-quota` / `--cpu-period(default: 100000us)` 和 `--cpus`不能混用,因为cpusbb本身就是 `quota/period`

```go
if cpus := context.Float64("cpus"); cpus > 0.0 {
			var (
				period = uint64(100000)
				quota  = int64(cpus * 100000.0)
			)
			opts = append(opts, oci.WithCPUCFS(quota, period))
		}

		if shares := context.Int("cpu-shares"); shares > 0 {
			opts = append(opts, oci.WithCPUShares(uint64(shares)))
		}

		quota := context.Int64("cpu-quota")
		period := context.Uint64("cpu-period")
		if quota != -1 || period != 0 {
			if cpus := context.Float64("cpus"); cpus > 0.0 {
				return nil, errors.New("cpus and quota/period should be used separately")
			}
			opts = append(opts, oci.WithCPUCFS(quota, period))
		}
```

这都符合我们的知识，现在我们需要考虑如何将这些资源配额传播给xen

### weight (share)

### 热更新

通过 CRI 和 containerd 通信时（k8s集群等）,容器资源可以热更新.

### ocispec Linux Resource 默认值

`populateDefaultUnixSpec`:

```go

func populateDefaultUnixSpec(ctx context.Context, s *Spec, id string) error {
	ns, err := namespaces.NamespaceRequired(ctx)
	if err != nil {
		return err
	}

	*s = Spec{
		Version: specs.Version,
		Root: &specs.Root{
			Path: defaultRootfsPath,
		},
		Process: &specs.Process{
			Cwd:             "/",
			NoNewPrivileges: true,
			User: specs.User{
				UID: 0,
				GID: 0,
			},
			Capabilities: &specs.LinuxCapabilities{
				Bounding:  defaultUnixCaps(),
				Permitted: defaultUnixCaps(),
				Effective: defaultUnixCaps(),
			},
			Rlimits: []specs.POSIXRlimit{
				{
					Type: "RLIMIT_NOFILE",
					Hard: uint64(1024),
					Soft: uint64(1024),
				},
			},
		},
		Linux: &specs.Linux{
			MaskedPaths: []string{
				"/proc/acpi",
				"/proc/asound",
				"/proc/kcore",
				"/proc/keys",
				"/proc/latency_stats",
				"/proc/timer_list",
				"/proc/timer_stats",
				"/proc/sched_debug",
				"/sys/firmware",
				"/sys/devices/virtual/powercap",
				"/proc/scsi",
			},
			ReadonlyPaths: []string{
				"/proc/bus",
				"/proc/fs",
				"/proc/irq",
				"/proc/sys",
				"/proc/sysrq-trigger",
			},
			CgroupsPath: filepath.Join("/", ns, id),
			Resources: &specs.LinuxResources{
				Devices: []specs.LinuxDeviceCgroup{
					{
						Allow:  false,
						Access: rwm,
					},
				},
			},
			Namespaces: defaultUnixNamespaces(),
		},
	}
	s.Mounts = defaultMounts()
	return nil
}
```

`Linux.Resources` 其他默认值都是 nil. 参考 runc 的行为，当 quota, peiord 这一对无效时，开放全部算力给容器。
对于mica，此情况意味着，`CPUCapacity=0`, 全权交给调度器.



## k8s 资源管理

以下不再重述:k8s的资源管控并不直接影响micran,但是它们的定义和containerd有相当的重合；而我们总要承接containerd的资源管控，并且，对可伸缩的“容器资源”而言，更多的伸缩和限制需求来自k8s,因此我们从k8s开始分析

我们基于 [resource management for pods and containers](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/), [Resource Quota](https://kubernetes.io/docs/concepts/policy/resource-quotas/) 和 [Limit Ranges](https://kubernetes.io/docs/concepts/policy/limit-range/) 来展开讨论


### 基本

k8s 垂直扩缩

```yaml
# VPA (Vertical Pod Autoscaler)
apiVersion: v1
kind: Pod
metadata:
  name: hot-update-example
spec:
  containers:
  - name: app
    image: nginx
    resources:
      requests:
        cpu: "500m"
        memory: "512Mi"
      limits:
        cpu: "1000m"     # 可通过 kubectl patch 热更新
        memory: "1Gi"    # 可通过 kubectl patch 热更新
```

kubelet 将 PodSpec 中的 `requested cpu, limited memory, limited cpu` 传给下游，
 `requested memory` 不会被填充, 这是由k8s来监控的。因此 **micran必须要能够准确反馈内存资源信息给k8s**

```go

MilliCPUToCPU = 1000
SharesPerCPU = 1024 //默认权重， cgroupv1 cpu.shares, cgroupv2 cpu.weight
cpuRequests := int64(0)
cpuLimits := int64(0)
memoryLimits := int64(0)

if request, found := reqs[v1.ResourceCPU]; found {
    cpuRequests = request.MilliValue()
}
if limit, found := limits[v1.ResourceCPU]; found {
    cpuLimits = limit.MilliValue()
}
if limit, found := limits[v1.ResourceMemory]; found {
    memoryLimits = limit.Value()
}

// convert to CFS values
cpuShares := MilliCPUToShares(cpuRequests)
cpuQuota := MilliCPUToQuota(cpuLimits, int64(cpuPeriod))

// quota is not capped when cfs quota is disabled
if !enforceCPULimits {
    cpuQuota = int64(-1)
	}

// MilliCPUToShares converts the milliCPU to CFS shares.
func MilliCPUToShares(milliCPU int64) uint64 {
	if milliCPU == 0 {
		// Docker converts zero milliCPU to unset, which maps to kernel default
		// for unset: 1024. Return 2 here to really match kernel default for
		// zero milliCPU.
		return MinShares
	}
	// Conceptually (milliCPU / milliCPUToCPU) * sharesPerCPU, but factored to improve rounding.
	shares := (milliCPU * SharesPerCPU) / MilliCPUToCPU
	if shares < MinShares {
		return MinShares
	}
	if shares > MaxShares {
		return MaxShares
	}
	return uint64(shares)
}

```

在oci spec 中 `CPU.requests` => `CPU.shares` 是这样的逻辑：

```go
// 对于 cpu.requests = 500m 来说， cpuRequest = millivalue(500m) = 500
cpuShares := cpuRequests * 1024 / 1000  // to 500 * 1024 / 1000
// and MinShares <= cpuShares <= MaxShares
```

当争用时，我们需要 `cpuShares`, 近似的对应选项为 `Xen::cpu_weight`

范围：

```c
#define MIN_SHARES		(1UL <<  1)
#define MAX_SHARES		(1UL << 18)
```
* cpushares: 2 - 262144, default=1024
* cpu_weight: 1 - 65535, default=256

可见是不完全成比例的范围，$W(S)=\lfloor \frac{S}{R} \rfloor$ 要对范围首尾做处理:
$$
W(S) = \max({1, \min{(\lfloor \frac{S}{R} \rfloor, 65535)})}; R=4
$$

### limit ranges

limit ranges 是k8s的一个机制而非话题。


* ResourceQuota：可以限制命名空间内的总资源消耗，但它不防止单个对象独占资源。
* LimitRange：命名空间级的策略，用来约束单个对象（Pod、Container、PVC）的资源分配。

**limit** and **request**

管理员在namespace中创建 LimitRange， 用户在这个ns中创建objects（如Pods）。我们通过sandbox来划定ns.

LimitRange首先在 apiserver这边作为一个门禁来检测:

```yaml
apiVersion: v1
kind: LimitRange
metadata:
  name: cpu-resource-constraint
spec:
  limits:
  - default: # this section defines default limits
      cpu: 500m
    defaultRequest: # this section defines default requests
      cpu: 500m
    max: # max and min define the limit range
      cpu: "1"
    min:
      cpu: 100m
    type: Container
# declare a Pod:
apiVersion: v1
kind: Pod
metadata:
  name: example-conflict-with-limitrange-cpu
spec:
  containers:
  - name: demo
    image: registry.k8s.io/pause:3.8
    resources:
      requests:
        cpu: 700m

# Pod "example-conflict-with-limitrange-cpu" is invalid: spec.containers[0].resources.requests: Invalid value: "700m": must be less than or equal to cpu limit

# ok Pod:
    resources:
      requests:
        cpu: 700m
      limits:
        cpu: 700m 

```

同时设定了 requests <= limits, 即便超出了limit range default value, 也是成功的。所以我们要处理这种corner case (default 显然是可以覆写的)

ResourceQuota 确保总量公平。LimitRange 防止单个对象“吃独食”。还能自动填充默认值，避免 Pod 因缺少 request/limit 而被 ResourceQuota 拒绝。





### Resource computation

我们直入主题，讨论如何转换、计算资源的分配到mica侧；至于这些关系的设计由来，可以从后面的小节中查看。



## CRI to Container Resource Mapping

### LinuxContainerResources to OCI Runtime Spec

| CRI Field | OCI Runtime Spec Field | Mapping Strategy |
|-----------|------------------------|------------------|
| `cpu_period` | `s.Linux.Resources.CPU.Period` | Direct assignment (`uint64(resources.GetCpuPeriod())`) |
| `cpu_quota` | `s.Linux.Resources.CPU.Quota` | Direct assignment (`resources.GetCpuQuota()`) |
| `cpu_shares` | `s.Linux.Resources.CPU.Shares` | Direct assignment (`uint64(resources.GetCpuShares())`) |
| `memory_limit_in_bytes` | `s.Linux.Resources.Memory.Limit` | Direct assignment (`resources.GetMemoryLimitInBytes()`) |
| `memory_swap_limit_in_bytes` | `s.Linux.Resources.Memory.Swap` | Direct assignment (`resources.GetMemorySwapLimitInBytes()`) |
| `cpuset_cpus` | `s.Linux.Resources.CPU.Cpus` | Direct string assignment (`resources.GetCpusetCpus()`) |
| `cpuset_mems` | `s.Linux.Resources.CPU.Mems` | Direct string assignment (`resources.GetCpusetMems()`) |
| `hugepage_limits` | `s.Linux.Resources.HugepageLimits` | Convert to `runtimespec.LinuxHugepageLimit` struct array |
| `unified` | `s.Linux.Resources.Unified` | Direct copy of map (`resources.GetUnified()`) |
| `oom_score_adj` | `s.Process.OOMScoreAdj` | Handled separately via `WithOOMScoreAdj` |

### Kubernetes Resources Proto Definition

Windows的比较简单:

```proto
// WindowsContainerResources specifies Windows specific configuration for
// resources.
message WindowsContainerResources {
    // CPU shares (relative weight vs. other containers). Default: 0 (not specified).
    int64 cpu_shares = 1;
    // Number of CPUs available to the container. Default: 0 (not specified).
    int64 cpu_count = 2;
    // Specifies the portion of processor cycles that this container can use as a percentage times 100.
    int64 cpu_maximum = 3;
    // Memory limit in bytes. Default: 0 (not specified).
    int64 memory_limit_in_bytes = 4;
}
```



```proto
// LinuxContainerResources specifies Linux specific configuration for
// resources.
message LinuxContainerResources {
    // CPU CFS (Completely Fair Scheduler) period. Default: 0 (not specified).
    int64 cpu_period = 1;
    // CPU CFS (Completely Fair Scheduler) quota. Default: 0 (not specified).
    int64 cpu_quota = 2;
    // CPU shares (relative weight vs. other containers). Default: 0 (not specified).
    int64 cpu_shares = 3;
    // Memory limit in bytes. Default: 0 (not specified).
    int64 memory_limit_in_bytes = 4;
    // OOMScoreAdj adjusts the oom-killer score. Default: 0 (not specified).
    int64 oom_score_adj = 5;
    // CpusetCpus constrains the allowed set of logical CPUs. Default: "" (not specified).
    string cpuset_cpus = 6;
    // CpusetMems constrains the allowed set of memory nodes. Default: "" (not specified).
    string cpuset_mems = 7;
    // List of HugepageLimits to limit the HugeTLB usage of container per page size. Default: nil (not specified).
    repeated HugepageLimit hugepage_limits = 8;
    // Unified resources for cgroup v2. Default: nil (not specified).
    // Each key/value in the map refers to the cgroup v2.
    // e.g. "memory.max": "6937202688" or "io.weight": "default 100".
    map<string, string> unified = 9;
    // Memory swap limit in bytes. Default 0 (not specified).
    int64 memory_swap_limit_in_bytes = 10;
}

```

### Corresponding Go Type

```go

// LinuxContainerResources specifies Linux specific configuration for
// resources.
type LinuxContainerResources struct {
	// CPU CFS (Completely Fair Scheduler) period. Default: 0 (not specified).
	CpuPeriod int64 `protobuf:"varint,1,opt,name=cpu_period,json=cpuPeriod,proto3" json:"cpu_period,omitempty"`
	// CPU CFS (Completely Fair Scheduler) quota. Default: 0 (not specified).
	CpuQuota int64 `protobuf:"varint,2,opt,name=cpu_quota,json=cpuQuota,proto3" json:"cpu_quota,omitempty"`
	// CPU shares (relative weight vs. other containers). Default: 0 (not specified).
	CpuShares int64 `protobuf:"varint,3,opt,name=cpu_shares,json=cpuShares,proto3" json:"cpu_shares,omitempty"`
	// Memory limit in bytes. Default: 0 (not specified).
	MemoryLimitInBytes int64 `protobuf:"varint,4,opt,name=memory_limit_in_bytes,json=memoryLimitInBytes,proto3" json:"memory_limit_in_bytes,omitempty"`
	// OOMScoreAdj adjusts the oom-killer score. Default: 0 (not specified).
	OomScoreAdj int64 `protobuf:"varint,5,opt,name=oom_score_adj,json=oomScoreAdj,proto3" json:"oom_score_adj,omitempty"`
	// CpusetCpus constrains the allowed set of logical CPUs. Default: "" (not specified).
	CpusetCpus string `protobuf:"bytes,6,opt,name=cpuset_cpus,json=cpusetCpus,proto3" json:"cpuset_cpus,omitempty"`
	// CpusetMems constrains the allowed set of memory nodes. Default: "" (not specified).
	CpusetMems string `protobuf:"bytes,7,opt,name=cpuset_mems,json=cpusetMems,proto3" json:"cpuset_mems,omitempty"`
	// List of HugepageLimits to limit the HugeTLB usage of container per page size. Default: nil (not specified).
	HugepageLimits []*HugepageLimit `protobuf:"bytes,8,rep,name=hugepage_limits,json=hugepageLimits,proto3" json:"hugepage_limits,omitempty"`
	// Unified resources for cgroup v2. Default: nil (not specified).
	// Each key/value in the map refers to the cgroup v2.
	// e.g. "memory.max": "6937202688" or "io.weight": "default 100".
	Unified map[string]string `protobuf:"bytes,9,rep,name=unified,proto3" json:"unified,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
	// Memory swap limit in bytes. Default 0 (not specified).
	MemorySwapLimitInBytes int64    `protobuf:"varint,10,opt,name=memory_swap_limit_in_bytes,json=memorySwapLimitInBytes,proto3" json:"memory_swap_limit_in_bytes,omitempty"`
	XXX_NoUnkeyedLiteral   struct{} `json:"-"`
	XXX_sizecache          int32    `json:"-"`
}
```

### Containerd Resource Conversion

```go
func WithResources(resources *runtime.LinuxContainerResources, tolerateMissingHugetlbController, disableHugetlbController bool) oci.SpecOpts
```

会转换资源类型为 `*specs.LinuxResources`:

```go
// LinuxResources has container runtime resource constraints
type LinuxResources struct {
	// Devices configures the device allowlist.
	Devices []LinuxDeviceCgroup `json:"devices,omitempty"`
	// Memory restriction configuration
	Memory *LinuxMemory `json:"memory,omitempty"`
	// CPU resource restriction configuration
	CPU *LinuxCPU `json:"cpu,omitempty"`
	// Task resource restriction configuration.
	Pids *LinuxPids `json:"pids,omitempty"`
	// BlockIO restriction configuration
	BlockIO *LinuxBlockIO `json:"blockIO,omitempty"`
	// Hugetlb limits (in bytes). Default to reservation limits if supported.
	HugepageLimits []LinuxHugepageLimit `json:"hugepageLimits,omitempty"`
	// Network restriction configuration
	Network *LinuxNetwork `json:"network,omitempty"`
	// Rdma resource restriction configuration.
	// Limits are a set of key value pairs that define RDMA resource limits,
	// where the key is device name and value is resource limits.
	Rdma map[string]LinuxRdma `json:"rdma,omitempty"`
	// Unified resources.
	Unified map[string]string `json:"unified,omitempty"`
}
```


## Containerd Cgroup 资源配额 转换为 Pedestal 资源抽象

首先，micran直接接触到的是 `specs.LinuxResources`, 我们需要将这些基于cgroup的资源配额转化为某种 `pedestal.Resource`。
我们排除掉不适宜的条目，比如:

### 目前不支持的:
- BlockIO
- HugepageLimits  
- Devices

### 不会支持的:
- Pids
- Rdma
- Unified cgroup v2

### 仅保留资源
仅保留 `CPU`, `Memory`, 并且我们仅关注:
- **CPU**: Period, Quota, Shares, Cpus, Mems
- **Memory**: Limit, Swap

```go
// LinuxCPU for Linux cgroup 'cpu' resource management
type LinuxCPU struct {
	// CPU shares (relative weight (ratio) vs. other cgroups with cpu shares).
	Shares *uint64 `json:"shares,omitempty"`
	// CPU hardcap limit (in usecs). Allowed cpu time in a given period.
	Quota *int64 `json:"quota,omitempty"`
	// CPU hardcap burst limit (in usecs). Allowed accumulated cpu time additionally for burst in a
	// given period.
	Burst *uint64 `json:"burst,omitempty"`
	// CPU period to be used for hardcapping (in usecs).
	Period *uint64 `json:"period,omitempty"`
	// How much time realtime scheduling may use (in usecs).
	RealtimeRuntime *int64 `json:"realtimeRuntime,omitempty"`
	// CPU period to be used for realtime scheduling (in usecs).
	RealtimePeriod *uint64 `json:"realtimePeriod,omitempty"`
	// CPUs to use within the cpuset. Default is to use any CPU available.
	Cpus string `json:"cpus,omitempty"`
	// List of memory nodes in the cpuset. Default is to use any available memory node.
	Mems string `json:"mems,omitempty"`
	// cgroups are configured with minimum weight, 0: default behavior, 1: SCHED_IDLE.
	Idle *int64 `json:"idle,omitempty"`
}



// LinuxMemory for Linux cgroup 'memory' resource management
type LinuxMemory struct {
	// Memory limit (in bytes).
	Limit *int64 `json:"limit,omitempty"`
	// Memory reservation or soft_limit (in bytes).
	Reservation *int64 `json:"reservation,omitempty"`
	// Total memory limit (memory + swap).
	Swap *int64 `json:"swap,omitempty"`
	// Kernel memory limit (in bytes).
	//
	// Deprecated: kernel-memory limits are not supported in cgroups v2, and
	// were obsoleted in [kernel v5.4]. This field should no longer be used,
	// as it may be ignored by runtimes.
	//
	// [kernel v5.4]: https://github.com/torvalds/linux/commit/0158115f702b0ba208ab0
	Kernel *int64 `json:"kernel,omitempty"`
	// Kernel memory limit for tcp (in bytes)
	KernelTCP *int64 `json:"kernelTCP,omitempty"`
	// How aggressive the kernel will swap memory pages.
	Swappiness *uint64 `json:"swappiness,omitempty"`
	// DisableOOMKiller disables the OOM killer for out of memory conditions
	DisableOOMKiller *bool `json:"disableOOMKiller,omitempty"`
	// Enables hierarchical memory accounting
	UseHierarchy *bool `json:"useHierarchy,omitempty"`
	// CheckBeforeUpdate enables checking if a new memory limit is lower
	// than the current usage during update, and if so, rejecting the new
	// limit.
	CheckBeforeUpdate *bool `json:"checkBeforeUpdate,omitempty"`
}

```

## 不同 Pedestal 的配额映射策略

### Baremetal  
- CPU核心直接分配

### Xen

vcpus 默认值为 1, maxcpus 默认 = 0,此时会转为 `maxvcpus=vcpus`
vcpus = 0会被设置为1

```c
// if maxvcpus is not set, use vcpus
max_vcpus = strtoul(nr_vcpus, &endptr, 10);

// maxcpus=0 => Error!
if (maxcpus == 0)
{
    LOGED(ERROR, domid, "Requested 0 VCPUs!");
    rc = ERROR_FAIL;
    goto out;
}

if (opt_dom0_max_vcpus_min == 0)
    opt_dom0_max_vcpus_min = 1;
```

**Roadmap**:

对每个 Pod Sandbox所用的 cpu资源都 对应一个 xen cpu pool;

分配的memory还有一个gap,Xen-PV的存在使得OS的保留内存体积更大

## 具体实现策略

1. 由于multi-shim情况，我们需要维护一个全局的管理器到共享内存中，跟踪已分配的CPU和内存
2. **OOM处理**: 
3. **资源回收机制**



## 完整映射表格

| LinuxContainerResources 字段 | 默认值 | Xen Pedestal 映射策略 | Baremetal Pedestal 映射策略 | 处理优先级 |
|------------------------------|--------|---------------------|---------------------------|----------|
| **CPU.** |
| `cpu_period` | 0 | 与`cpu_quota`结合计算vCPU数量和cap | 与`cpu_quota`结合计算CPU核心分配 | 高 |
| `cpu_quota` | 0 | 转换为Xen vCPU数量+cap百分比 | 转换为整数CPU核心数 | 高 |
| `cpu_shares` | 0 | 映射为Xen调度weight(1-65535) | 映射为Linux调度nice值或RT优先级 | 中 |
| `cpuset_cpus` | "" | CPUAffinity | 直接绑定物理核 | 中 |
| `cpuset_mems` | "" | 不确定 | 不确定 | 低 |
| **Memory.** |
| `memory_limit_in_bytes` | 0 | 转换为Xen domain内存分配(MB) | 转换为物理内存分配限制 | 高 |
| `memory_swap_limit_in_bytes` | 0 | **忽略?** | **忽略?** | 低 |
| **其他资源** |
| `oom_score_adj` | 0 | **部分忽略** (RTOS无OOM概念,但我们最好让micran即将OOM时，拦截新的container create需求) | **忽略** (RTOS无OOM概念) | 忽略 |
| `hugepage_limits` | nil | **暂时忽略** (未来可扩展) | **暂时忽略** (不太理解)| 忽略 |
| `unified` (cgroup v2) | nil | **完全忽略** (不适用) | **完全忽略** (不适用) | 忽略 |



## 具体策略

1. 由于multi-shim情况，我们需要维护一个全局的管理器到共享内存中，跟踪已分配的CPU和Mem；
1. OOM时 —— 
1. 资源回收 (标记回收, 实际回收交给xen, 这里有一个一致性隐患要解决)


# LinuxContainerResources 到 Micran 资源映射完整对照表

## 概览

本表格详细说明了如何将Kubernetes/containerd的LinuxContainerResources转换为适合不同pedestal环境的mica client资源配置。


## 详细转换策略

### 1. CPU 资源转换

#### A. Xen Pedestal

```go
type XenCPUMapping struct {
    VCPUs       int     // 虚拟CPU数量
    CPUWeight   int     // 调度权重 (1-65535, default=256)
    CPUCap      int     // CPU使用率上限 (每vCPU的百分比)
    CPUS        string  // cpuset cpus
    CPUAffinity []int   // CPU亲和性
}

func convertCPUResourcesXen(res *LinuxContainerResources, availableCPUs int) XenCPUMapping {
    mapping := XenCPUMapping{
        VCPUs:     1,    // 默认1个vCPU
        CPUWeight: 256,  // Xen默认权重
        CPUCap:    0,    // 0表示不限制
    }
    
    if res.CpuQuota > 0 && res.CpuPeriod > 0 {
        // 计算需要的CPU核心数
        requestedCores := float64(res.CpuQuota) / float64(res.CpuPeriod)
        
        // 分配vCPU数量 (向上取整)
        mapping.VCPUs = int(math.Ceil(requestedCores))
        if mapping.VCPUs > availableCPUs {
            mapping.VCPUs = availableCPUs
        }
        
        // 计算cap: 如果request 1.5 cores但分配2 vCPU，则每个vCPU cap=75%
        mapping.CPUCap = int((requestedCores / float64(mapping.VCPUs)) * 100)
        if mapping.CPUCap > 100 {
            mapping.CPUCap = 100
        }
    }
    
    // 2. CPU Shares 转换为权重
    if res.CpuShares > 0 {
        // cgroup默认1024 -> Xen默认256的比例转换
        mapping.CPUWeight = int((res.CpuShares * 256) / 1024)
        if mapping.CPUWeight < 1 {
            mapping.CPUWeight = 1
        } else if mapping.CPUWeight > 65535 {
            mapping.CPUWeight = 65535
        }
    }
    
    // 3. CPU Set 转换
    if res.CpusetCpus != "" {
        mapping.CPUAffinity = parseCPUSetForXen(res.CpusetCpus, availableCPUs)
    }
    
    return mapping
}
```

#### B. Baremetal Pedestal

```go
type BaremetalCPUMapping struct {
    // 目标配置
    CPUCores        []int  // 分配的物理CPU核心列表
    SchedulerPolicy int    // 调度策略 (SCHED_NORMAL, SCHED_RR, SCHED_FIFO)
    Priority        int    // RT优先级 (1-99) 或 nice值 (-20到19)
    CPUAffinity     []int  // CPU亲和性掩码
}

func convertCPUResourcesBaremetal(res *LinuxContainerResources, totalCPUs int) BaremetalCPUMapping {
    mapping := BaremetalCPUMapping{
        CPUCores:        []int{0}, // 默认分配CPU 0
        SchedulerPolicy: SCHED_NORMAL,
        Priority:        0,
    }
    
    // 1. CPU Period + Quota 转换为整数核心
    if res.CpuQuota > 0 && res.CpuPeriod > 0 {
        requestedCores := float64(res.CpuQuota) / float64(res.CpuPeriod)
        
        // 只能分配整数核心
        coreCount := int(math.Ceil(requestedCores))
        if coreCount > totalCPUs {
            coreCount = totalCPUs
        }
        
        // 分配连续的CPU核心
        mapping.CPUCores = make([]int, coreCount)
        for i := 0; i < coreCount; i++ {
            mapping.CPUCores[i] = i
        }
        
        // 如果请求的是小数核心，使用实时调度+时间片分割
        if requestedCores < float64(coreCount) {
            mapping.SchedulerPolicy = SCHED_RR  // 轮转调度
            // 计算时间片比例
            ratio := requestedCores / float64(coreCount)
            mapping.Priority = int(ratio * 50) // 映射到1-50的优先级范围
        }
    }
    
    // 2. CPU Shares 转换为调度优先级
    if res.CpuShares > 0 {
        // 1024为默认值，映射到nice值-5到15的范围
        niceValue := int(((res.CpuShares - 1024) * 20) / 1024)
        if niceValue < -20 {
            niceValue = -20
        } else if niceValue > 19 {
            niceValue = 19
        }
        mapping.Priority = niceValue
    }
    
    // 3. CPU Set 直接映射
    if res.CpusetCpus != "" {
        mapping.CPUAffinity = parseCPUSet(res.CpusetCpus)
        // 更新实际分配的核心列表
        mapping.CPUCores = mapping.CPUAffinity
    }
    
    return mapping
}
```

### 2. 内存资源转换

#### A. Xen Pedestal

```go
type XenMemoryMapping struct {
    Memory    int64  // 静态内存分配 (MB)
    MaxMemory int64  // 最大内存限制 (MB)  
}

func convertMemoryResourcesXen(res *LinuxContainerResources, availableMemoryMB int64) XenMemoryMapping {
    mapping := XenMemoryMapping{}
    
    if res.MemoryLimitInBytes > 0 {
        memoryMB := res.MemoryLimitInBytes / (1024 * 1024)
        
        // 确保不超过可用内存
        if memoryMB > availableMemoryMB {
            memoryMB = availableMemoryMB
        }
        
        // Xen通常使用静态内存分配
        mapping.Memory = memoryMB
        mapping.MaxMemory = memoryMB
    } else {
        // 默认分配策略
        defaultMem := availableMemoryMB / 4  // 25%的可用内存
        if defaultMem < 128 {
            defaultMem = 128  // 最小128MB
        }
        mapping.Memory = defaultMem
        mapping.MaxMemory = defaultMem
    }
    
    // memory_swap_limit_in_bytes 被忽略
    return mapping
}
```

#### B. Baremetal Pedestal

```go
type BaremetalMemoryMapping struct {
    MemoryLimitBytes int64  // 内存限制 (字节)
    SwapLimitBytes   int64  // swap限制 (字节)
    UseMemCgroup     bool   // 是否使用memory cgroup
}

func convertMemoryResourcesBaremetal(res *LinuxContainerResources) BaremetalMemoryMapping {
    mapping := BaremetalMemoryMapping{
        UseMemCgroup: true,  // 可以使用Linux cgroup进行内存限制
    }
    
    if res.MemoryLimitInBytes > 0 {
        mapping.MemoryLimitBytes = res.MemoryLimitInBytes
    }
    
    if res.MemorySwapLimitInBytes > 0 {
        mapping.SwapLimitBytes = res.MemorySwapLimitInBytes
    }
    
    return mapping
}
```

## 资源管理器实现

### 系统资源感知

```go
type PedestalResourceManager interface {
    GetAvailableResources() (cpus int, memoryMB int64)
    AllocateResources(cpus int, memoryMB int64) error
    ReleaseResources(cpus int, memoryMB int64) error
    GetResourceUsage() ResourceUsage
}

type XenResourceManager struct {
    dom0CPUs        int   // Dom0保留的CPU数
    dom0Memory      int64 // Dom0保留的内存(MB)
    totalCPUs       int   // 物理机总CPU数
    totalMemory     int64 // 物理机总内存(MB)
    
    mutex           sync.RWMutex
    allocatedCPUs   int   // 已分配的CPU数
    allocatedMemory int64 // 已分配的内存(MB)
}

func (xrm *XenResourceManager) GetAvailableResources() (int, int64) {
    xrm.mutex.RLock()
    defer xrm.mutex.RUnlock()
    
    availCPUs := (xrm.totalCPUs - xrm.dom0CPUs) - xrm.allocatedCPUs
    availMemory := (xrm.totalMemory - xrm.dom0Memory) - xrm.allocatedMemory
    
    if availCPUs < 0 {
        availCPUs = 0
    }
    if availMemory < 0 {
        availMemory = 0
    }
    
    return availCPUs, availMemory
}

type BaremetalResourceManager struct {
    totalCPUs       int
    totalMemory     int64
    
    mutex           sync.RWMutex
    allocatedCPUs   int
    allocatedMemory int64
}

func (brm *BaremetalResourceManager) GetAvailableResources() (int, int64) {
    brm.mutex.RLock()
    defer brm.mutex.RUnlock()
    
    // Baremetal下Linux可以看到全部资源
    availCPUs := brm.totalCPUs - brm.allocatedCPUs
    availMemory := brm.totalMemory - brm.allocatedMemory
    
    if availCPUs < 0 {
        availCPUs = 0
    }
    if availMemory < 0 {
        availMemory = 0
    }
    
    return availCPUs, availMemory
}
```

## 特殊考虑事项

### 1. Baremetal环境的CPU分片策略

在baremetal环境下，虽然CPU只能以整数分配，但仍可通过以下方式实现类似cgroup的细粒度控制：

1. **实时调度策略**: 使用`SCHED_RR`或`SCHED_FIFO`配合时间片
2. **CPU时间配额**: 通过定时器中断实现时间片轮转
3. **优先级调度**: 使用不同的调度优先级实现相对权重
4. **CPU绑定**: 将进程绑定到特定CPU核心

### 2. 并发安全保证

```go
// 共享内存资源计数器 (用于多shim实例)
type SharedResourceCounter struct {
    shmPath     string
    mutex       *sync.Mutex  // 跨进程互斥锁
    allocatedCPUs   *int64   // 共享内存中的CPU计数
    allocatedMemory *int64   // 共享内存中的内存计数
}
```

### 3. 不支持/忽略的字段处理

- `oom_score_adj`: RTOS环境无OOM killer，记录警告日志
- `hugepage_limits`: 依赖RTOS支持，暂时忽略  
- `unified`: cgroup v2特有，完全不适用
- `memory_swap_limit_in_bytes`: Xen环境下忽略，baremetal可支持

这个映射策略充分考虑了micran项目的特殊性，将Linux容器的资源概念转换为适合RTOS运行环境的资源分配策略。

## Micran 资源映射实现细节

### 核心映射关系

| Linux cgroup 参数 | Micran 映射目标 | 转换规则 |
|------------------|----------------|----------|
| `cpu.shares` | `CPUWeight` | Xen: `W(S) = max(1, min(⌊S/R⌋, 65535)); R=4`<br>其他: 直接使用 `cpu.shares` 值 |
| `cpu.quota`/`cpu.period` | `CpuCpacity` (百分比容量) | `capacity = (quota × 100) / period` |
| `cpuset.cpus` | `ClientCpuSet` + `Vcpu` | 直接传递CPU亲和性，从cpuset计算vCPU数量 |

### cpuset 对 CPU 容量的影响

Micran 遵循与 `runc` 相同的语义：**最终生效的 CPU 算力受 cpuset 和 quota/period 的双重约束**。

计算公式：
```
effective_capacity = min(
    (quota × 100) / period,   # quota/period 转换的百分比容量
    cpuset_size × 100          # cpuset 核心数 × 100%
)
```

实现位置：`pkg/pedestal/planner.go` 中的 `linuxResourceToEssential()` 函数。

### 处理优先级和语义

1. **cpuset 优先解析**：首先解析 `cpuset.cpus` 获取 `vcpuNum`（cpuset 中的核心数）
2. **quota/period 计算**：计算原始容量 `rawCapacity = (quota × 100) / period`
3. **容量限制应用**：如果 `vcpuNum > 0`，则 `effectiveCapacity = min(rawCapacity, vcpuNum × 100)`
4. **默认值处理**：
   - 无 quota/period 但有 cpuset：`capacity = vcpuNum × 100`
   - 无 cpuset 但有 quota/period：`capacity = rawCapacity`
   - 两者都无：`capacity = 0`（无限制）

### 代码实现示例

```go
func linuxResourceToEssential(spec *specs.Spec, convertShares bool) *EssentialResource {
    // 1. 解析 cpuset 获取 vcpuNum
    cpus, cpuSetVCpuNum := validateCPUSet(cpu.Cpus)
    if cpus != "" && cpuSetVCpuNum > 0 {
        res.ClientCpuSet = cpus
        vcpuNum = cpuSetVCpuNum
    }

    // 2. 处理 quota/period，应用 cpuset 限制
    if cpu.Quota != nil && *cpu.Quota > 0 && cpu.Period != nil && *cpu.Period > 0 {
        rawCapacity := uint32((*cpu.Quota * 100) / int64(*cpu.Period))
        if rawCapacity > 0 {
            if vcpuNum > 0 {
                maxByCpuset := vcpuNum * 100
                if rawCapacity > maxByCpuset {
                    rawCapacity = maxByCpuset
                }
            }
            *res.CpuCpacity = rawCapacity
        }
    } else if vcpuNum > 0 {
        // 无有效 quota/period，但有 cpuset
        *res.CpuCpacity = vcpuNum * 100
    }

    // 3. 处理 shares 转换
    if cpu.Shares != nil && *cpu.Shares > 0 {
        if convertShares {
            weight := ShareToWeight(*cpu.Shares)
            res.CPUWeight = &weight
        } else {
            share := uint32(*cpu.Shares)
            res.CPUWeight = &share
        }
    }
}
```

### 测试验证

测试用例覆盖场景：
1. **cpuset 限制更严格**：quota=200% (2.0核)，cpuset="0" (1核) → capacity=100%
2. **quota 限制更严格**：quota=50% (0.5核)，cpuset="0-3" (4核) → capacity=50%
3. **只有 cpuset**：无 quota/period，cpuset="0-1" (2核) → capacity=200%
4. **只有 quota**：quota=150% (1.5核)，无 cpuset → capacity=150%

### 与 runc 的语义对齐

通过上述实现，Micran 实现了与 `runc` 相同的资源限制语义：
- **cpuset**：作为物理天花板，限制容器可用的 CPU 核心数
- **quota/period**：作为逻辑上限，限制容器可使用的 CPU 时间片
- **最终限制**：取两者的最小值，确保配置的物理可实现性

这种设计保证了从 Kubernetes/containerd 传递的资源限制在 RTOS 环境中得到正确、安全的映射。
