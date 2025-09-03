# fatals

- [x] bundle/rootfs/rootfs, 这里多了一层
- [x] bundle rootfs mount空
1. delete未执行
- [x]. 初始的时候，仍然尝试restore sandbox,这肯定会导致错误的。
- [x] cleanupContainer时，sandbox state.json 不存在时的错误——根本没创建起来?
- [x] shimService.Cleanup仍然遇到state.json问题
- [x] network type mismatched
- [x] newContainer empty container id arguments incorrect
- [x] shim.create.createSandbox len(containers != 1)
- [x] xen pedestal requires a image*.bin file for boot
- [x] containerPath not resolved!
- [ ] stuck after forward events, libmica根本没有被调用
- [x] libmica.stop 在socket不存在时，不应该错误(workaround)
- [x] 状态一致性问题严重
- [x] stop没有发送libmica.stop
- [ ] `ctr task start demo` 没有自动执行完
- [ ] kill如果正常执行，可以保证幂等，然而如果kill的是UNKNOWN状态的容器，会[3] container not found, mark status to UNKNOWN



# flaws

- [x]. sandbox.Restore() 空指针解引用
- [x] ctr t task demo: [2] empty container id: unknown

```go
newContainer recover container from containerconfig, the id is empty
```

- [ ] `ctr t start demo` invalid state: runnign (expected: ready)
- [x] bundle rootfs 全空
- [x] clientpath这一项解析为空
- [x] clientpath firmwarepath的值如果是绝对路径开头的，要处理掉
- [x] validMicaContainer before mount Rootfs
- [x] defer nil error: when shim.Kill()
- [x] 新版mock_micad行为错误：received buffer size mismatch  with struct create message
- [ ] micad status, micran status (in-memory), containerd status 一致性