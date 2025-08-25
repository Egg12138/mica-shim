# Micran TODO

## Agent Removal Plan

### Overview
Remove the unnecessary Agent pattern inherited from kata-containers. Micran's simple architecture (Containerd → Micran → libmica → micad → RTOS) doesn't need this abstraction layer.

### Files to Remove
- [ ] `pkg/micantainer/agent.go` - RealAgent implementation
- [ ] `pkg/micantainer/mock_agent.go` - MockAgent implementation

### Files to Modify
- [ ] `pkg/micantainer/sandbox.go` - Remove agent field and calls
- [ ] `pkg/micantainer/interfaces.go` - Remove agent interface dependencies
- [ ] `go.mod` - Remove kata-containers dependency

### Current Agent Usage Analysis

#### Active Agent Methods (8 methods that actually do something):
1. `stopSandbox()` → calls `libmica.Stop()`
2. `startContainer()` → calls `libmica.Start()`
3. `createContainer()` → creates RTOSTask stub
4. `waitTask()` → calls `libmica.Stop()`
5. `vcpuSet()` → returns max CPU count
6. `getOOMEvent()` → returns empty string (stub)
7. `init()` → returns false, nil
8. `createSandbox()` → checks daemon status

#### No-op Methods (95% of agent - can be removed):
- All I/O methods (stdin, stdout, stderr)
- All VM management methods
- All network methods
- All resource hotplug methods
- All kata-containers compatibility methods

### Implementation Phases

#### Phase 1: Direct Migration
- [ ] Remove `agent RealAgent` field from Sandbox struct (line 140)
- [ ] Move active methods directly to Sandbox:
  - [ ] `stopSandbox()` → direct libmica.Stop() call
  - [ ] `startContainer()` → direct libmica.Start() call
  - [ ] `createContainer()` → inline RTOSTask creation
  - [ ] `waitTask()` → direct libmica.Stop() call
  - [ ] `vcpuSet()` → inline pedestal call
  - [ ] `getOOMEvent()` → remove or stub
  - [ ] `init()` → remove (returns false)
  - [ ] `createSandbox()` → inline daemon check
- [ ] Replace all agent method calls in sandbox.go:
  - [ ] Line 295: `s.agent.Cleanup(ctx)` → remove
  - [ ] Line 515: `s.agent.vcpuSet(ctx)` → direct call
  - [ ] Line 538: `s.agent.getOOMEvent(ctx)` → remove/stub
  - [ ] Line 712: `s.agent.stopSandbox(ctx, s)` → direct call
  - [ ] Line 779: `s.agent.init(ctx, s)` → remove
  - [ ] Line 784: `s.agent.createSandbox(ctx, s)` → inline
- [ ] Remove agent initialization in NewSandbox() and LoadSandbox()

#### Phase 2: Clean Dependencies
- [ ] Remove kata-containers imports:
  - [ ] `"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/pkg/agent/protocols/grpc"`
  - [ ] `"github.com/kata-containers/kata-containers/src/runtime/virtcontainers/types"`
- [ ] Remove unused type imports:
  - [ ] `types.Cmd`
  - [ ] `types.Capabilities`
  - [ ] `types.PciPath`
  - [ ] `grpc.*` types
- [ ] Update go.mod to remove kata-containers dependency

#### Phase 3: Simplify Code
- [ ] Delete agent.go and mock_agent.go files
- [ ] Remove any remaining agent references in comments
- [ ] Update documentation if needed

### Risks and Mitigation

#### Risks:
1. Breaking changes if external code uses agent interface
2. Lost functionality if any no-op methods are actually needed
3. Import errors from missing kata-containers types

#### Mitigation:
1. Verify no external dependencies on agent package
2. Keep stub implementations for potentially used methods
3. Incremental testing after each change
4. Maintain libmica abstraction for future flexibility

### Expected Benefits
- Remove ~500 lines of unnecessary code
- Simplified architecture
- Remove kata-containers dependency
- Clearer responsibility boundaries
- Easier maintenance

### Implementation Order
1. Create direct method implementations in sandbox.go
2. Replace agent calls with direct calls
3. Remove agent field and initialization
4. Delete agent files
5. Clean up imports
6. Test all functionality

---

## Other TODOs

### High Priority
- [ ] SCHED_CORE support
- [ ] Xen adapter implementation (remove jailhouse, baremetal placeholders)
- [ ] Complete task events implementation
- [ ] PTY support
- [ ] Network configuration
- [ ] Use vendor for dependency management

### Medium Priority
- [ ] k8s Pod IP management
- [ ] CPU core scheduling improvements
- [ ] Daemon lifecycle consistency
- [ ] Error handling improvements
- [ ] Performance optimization

### Low Priority
- [ ] Stateful service support
- [ ] Monitoring and observability
- [ ] Multiple RTOS types support
- [ ] Dynamic task management

### Known Issues
- [ ] Fix mock_micad memory leaks
- [ ] Improve error handling in libmica
- [ ] Add more comprehensive tests
- [ ] Documentation updates