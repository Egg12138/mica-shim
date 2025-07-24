# MICA-Shim 容器资源配置解析 - 大模型生成

## 概述

本文档说明 mica-shim 如何解析和处理容器的 CPU 和内存资源限制配置。

## OCI 规范配置示例

### 完整的 config.json 示例

```json
{
  "ociVersion": "1.0.2",
  "process": {
    "terminal": true,
    "user": {
      "uid": 0,
      "gid": 0
    },
    "args": [
      "/bin/sh"
    ],
    "env": [
      "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
    ],
    "cwd": "/",
    "capabilities": {
      "bounding": [
        "CAP_AUDIT_WRITE",
        "CAP_KILL",
        "CAP_NET_BIND_SERVICE"
      ],
      "effective": [
        "CAP_AUDIT_WRITE", 
        "CAP_KILL",
        "CAP_NET_BIND_SERVICE"
      ],
      "inheritable": [
        "CAP_AUDIT_WRITE",
        "CAP_KILL", 
        "CAP_NET_BIND_SERVICE"
      ],
      "permitted": [
        "CAP_AUDIT_WRITE",
        "CAP_KILL",
        "CAP_NET_BIND_SERVICE"
      ],
      "ambient": [
        "CAP_NET_BIND_SERVICE"
      ]
    },
    "rlimits": [
      {
        "type": "RLIMIT_NOFILE",
        "hard": 1024,
        "soft": 1024
      }
    ],
    "noNewPrivileges": true
  },
  "root": {
    "path": "rootfs",
    "readonly": true
  },
  "hostname": "mica-container",
  "mounts": [
    {
      "destination": "/proc",
      "type": "proc",
      "source": "proc"
    },
    {
      "destination": "/dev",
      "type": "tmpfs",
      "source": "tmpfs",
      "options": [
        "nosuid",
        "strictatime",
        "mode=755",
        "size=65536k"
      ]
    }
  ],
  "linux": {
    "resources": {
      "cpu": {
        "shares": 1024,
        "quota": 100000,
        "period": 100000,
        "cpus": "0-1",
        "mems": "0"
      },
      "memory": {
        "limit": 536870912,
        "reservation": 268435456,
        "swap": 1073741824,
        "kernel": 134217728,
        "swappiness": 60,
        "disableOOMKiller": false
      }
    },
    "namespaces": [
      {
        "type": "pid"
      },
      {
        "type": "network"
      },
      {
        "type": "ipc"
      },
      {
        "type": "uts"
      },
      {
        "type": "mount"
      }
    ],
    "maskedPaths": [
      "/proc/acpi",
      "/proc/asound",
      "/proc/kcore",
      "/proc/keys",
      "/proc/latency_stats",
      "/proc/timer_list",
      "/proc/timer_stats",
      "/proc/sched_debug",
      "/sys/firmware",
      "/proc/scsi"
    ],
    "readonlyPaths": [
      "/proc/bus",
      "/proc/fs",
      "/proc/irq",
      "/proc/sys",
      "/proc/sysrq-trigger"
    ]
  },
  "annotations": {
    "org.opencontainer...." : "abc"
  }
}
```

## 资源配置解析


### CPU 资源配置

MICA-Shim 从 `linux.resources.cpu` 部分解析以下 CPU 资源配置：

1. **CPU Shares** (`cpu.shares`): 相对权重，用于 CPU 调度
   - 默认值: 1024
   - 范围: 2-262144

2. **CPU Quota/Period** (`cpu.quota`, `cpu.period`): 绝对 CPU 限制
   - Quota: 每个周期允许使用的 CPU 时间（微秒）
   - Period: 调度周期长度（微秒），通常为 100000（100ms）
   - CPU 核数限制 = quota / period

3. **CPU Set** (`cpu.cpus`): 指定可使用的 CPU 核心
   - 格式: "0-3" 或 "0,1,3"

4. **CPU Memory Nodes** (`cpu.mems`): NUMA 内存节点

### 内存资源配置

MICA-Shim 从 `linux.resources.memory` 部分解析以下内存资源配置：

1. **Memory Limit** (`memory.limit`): 内存硬限制（字节）
2. **Memory Reservation** (`memory.reservation`): 内存软限制（字节）
3. **Memory + Swap Limit** (`memory.swap`): 内存+交换分区总限制（字节）
4. **Kernel Memory Limit** (`memory.kernel`): 内核内存限制（字节）
5. **Memory Swappiness** (`memory.swappiness`): 交换分区使用倾向（0-100）
6. **OOM Killer Disable** (`memory.disableOOMKiller`): 是否禁用 OOM Killer

### 运行时级别配置

通过 annotations 配置运行时级别的资源管理策略：

1. **调试模式**: `org.openeuler.mica.runtime.debug`
2. **沙箱资源**: `org.openeuler.mica.runtime.sandbox.cpus/memory`
3. **容器资源上限**: `org.openeuler.mica.runtime.max_container_cpus/memory`
4. **调度策略**: `org.openeuler.mica.runtime.cpu_scheduler_policy`
5. **内存超分配**: `org.openeuler.mica.runtime.memory_overcommit`

## 配置位置和优先级

### ContainerConfig vs RuntimeConfig

- **ContainerConfig**: 存储每个容器的具体资源限制
  - 从 OCI spec 的 `linux.resources` 解析
  - 容器级别的 CPU/内存配置
  - 生命周期与容器绑定

- **RuntimeConfig**: 存储运行时级别的全局配置
  - 从 OCI spec 的 `annotations` 解析
  - 运行时策略和默认值
  - 影响所有容器

### 配置优先级

1. **OCI Spec 资源限制** (最高优先级)
2. **容器配置文件** (client.conf)
3. **运行时注解配置**
4. **系统默认值** (最低优先级)

## 资源验证

MICA-Shim 会验证以下资源配置：

1. **CPU 限制验证**:
   - CPU 限制不超过系统 CPU 数量
   - CPU period 在 1000-1000000 微秒范围内
   - CPU quota 至少 1000 微秒

2. **内存限制验证**:
   - 内存限制不超过系统可用内存
   - Memory swappiness 在 0-100 范围内

3. **资源冲突检测**:
   - 检查资源配置之间的冲突
   - 验证资源分配的合理性

## 使用示例

```shell
# example
ctr run --runtime org.openeuler.mica.v1   --memory-limit xxx(Bytes) --cpu-quota 50000 --cpu-period 100000 localhost:5000/zephyr-mica:openamp damn
```

### 查看资源使用情况

```bash
ctr container info damn
cat /proc/meminfo
cat /proc/cpuinfo
```

## 日志输出示例

```
INFO[2025-07-15T10:30:15.123Z] Container resource limits - CPU: limit=1 cores, quota=0.50 cores, cpuset=0-1, Memory: limit=256.0 MB
DEBUG[2025-07-15T10:30:15.124Z] Parsed CPU limit from quota/period: 0 (quota: 50000, period: 100000)
DEBUG[2025-07-15T10:30:15.125Z] Parsed memory limit: 268435456 bytes
DEBUG[2025-07-15T10:30:15.126Z] Using container CPU limit from OCI spec: 0
DEBUG[2025-07-15T10:30:15.127Z] Using container memory limit from OCI spec: 268435456 bytes
```

## 故障排除

### 常见错误和解决方案

1. **CPU 限制超出系统容量**
   ```
   ERROR: container CPU limit 8 exceeds system CPU count 4
   ```
   解决方案: 调整 CPU 限制或检查系统 CPU 配置

2. **内存限制超出系统容量**
   ```
   ERROR: container memory limit 8589934592 bytes exceeds system memory 4294967296 bytes
   ```
   解决方案: 调整内存限制或检查系统内存配置

3. **无效的 CPU period 值**
   ```
   ERROR: invalid CPU period 500, must be between 1000 and 1000000 microseconds
   ```
   解决方案: 调整 CPU period 到有效范围内

## 性能建议

1. **CPU 配置**:
   - 为 RTOS 容器预留专用 CPU 核心
   - 避免 CPU 超分配
   - 使用 cpuset 进行 CPU 隔离

2. **内存配置**:
   - 为主机系统预留足够内存
   - 合理设置内存软限制
   - 监控内存使用情况

3. **调度策略**:
   - 选择适合工作负载的调度策略
   - 定期监控资源使用情况
   - 调整资源限制以优化性能

## 相关文档

- [OCI Runtime Specification](https://github.com/opencontainers/runtime-spec)
- [Linux cgroups documentation](https://www.kernel.org/doc/Documentation/cgroup-v1/)
- [MICA Runtime Architecture](./archs.md) 
