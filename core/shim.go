package core

import (
	"fmt"

	"github.com/containerd/containerd/pkg/shutdown"
	"github.com/containerd/containerd/plugin"
)

func RegisterPlugin() {
	plugin.Register(&plugin.Registration{
		Type: plugin.TTRPCPlugin,
		ID:   "task",
		Requires: []plugin.Type{
			plugin.InternalPlugin,
		},
		InitFn: func(ic *plugin.InitContext) (interface{}, error) {
			ss, err := ic.GetByID(plugin.InternalPlugin, "shutdown")
			if err != nil {
				return nil, fmt.Errorf("getting shutdown internal plugin: %w", err)
			}
			return newTaskService(ss.(shutdown.Service))
		},
	})
}