package shim

import "context"

const (
	DefaultConfig = "micran.toml"
)

// Configuration related types
type DynamicConfigs struct {
}

type Plugin struct {
}

// HotReload is intended to reload mica daemon without breaking container running status
// 1. travel the container list
//   - lock it
//   - store container state --- currently we do not support persist, thus there
//     is less concerns about consistency issue
//
// 2. handle the rtos management temporarily
// 3. load micran configs
// 4. restart daemon
// 5. travel the container list
//   - recover containers from hotreload dir
//   - change Daemon pid field
func HotReload(ctx context.Context) error {
	return nil
}
