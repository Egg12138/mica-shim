# micrun 容器资源配置解析

## 概述

本文档说明 micrun 如何解析和处理容器的 CPU 和内存资源限制配置。

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


micrun 从 `linux.resources.cpu` 部分解析以下 CPU 资源配置：
1. **CPU Shares** (`cpu.shares`): 相对权重，用于 CPU 调度
   - 默认值: 1024
   - 范围: 2-262144
2. **CPU Quota/Period** (`cpu.quota`, `cpu.period`): 绝对 CPU 限制
   - Quota: 每个周期允许使用的 CPU 时间（微秒）
   - Period: 调度周期长度（微秒），通常为 100000（100ms）
   - CPU 核数限制 = quota / period
3. **CPU Set** (`cpu.cpus`): 指定可使用的 CPU 核心
   - 格式: "0-3" 或 "0,1,3"
4. **CPU Memory Nodes** (`cpu.mems`): NUMA 内存节点, 暂时不处理
映射关系:
```
Container CPU Share <==(放缩比例1024:256)==> RTOS Client CPU Weight
Container Quota/Period <==(放缩比例1:100)==> RTOS Client CPU Capacity(百分比，占满单核100%)
Container cpuset <====> RTOS Client CPUS
```


micrun 从 `linux.resources.memory` 部分解析以下内存资源配置：
映射关系：

```
No in Container Memory resource                     <====> RTOS Client pedestal max memory
Container memory limit <====> RTOS Client memory limit
Contianer memory reservation < memory limit <====> RTOS Client memory min
```

memory的资源映射是一个容易出错的过程，我们应该这样规范：在Container 语境下，使用 container memory limit, minimal memory, 
在libmica语境下，使用RTOS Client memoyr resource. 

container.me.records 记录了 libmica 语境下的资源量
container.me.memoryThreshold 设计为单调递增的, 因此它仅在 新的 memory threshold 出现时才会正更新
ped EssentialResource中并不记录 memoryThreshold, memorymaxmb 就是 oci spec mem.Limit, mem min 就是 oci spec mem.Reservation
因为该类型记录的是实际资源，因此仅在micaexecutor中记录 memory threshold, 也保证了简单——只有memory threshold >= container memory limit
memorymaxmb 对应的是 mica create message 中的 memory

## 相关文档

- [OCI Runtime Specification](https://github.com/opencontainers/runtime-spec)
- [Linux cgroups documentation](https://www.kernel.org/doc/Documentation/cgroup-v1/)
- [MicRun Runtime Architecture](./archs.md)
