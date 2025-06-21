package core

import (
	"fmt"

	"github.com/containerd/containerd/pkg/shutdown"
	"github.com/containerd/containerd/plugin"
)

func ttrpcService(ic *plugin.InitContext) (interface{}, error) {
	ss, err := ic.GetByID(plugin.InternalPlugin, "shutdown")
	if err != nil {
		return nil, fmt.Errorf("failed to get dependency: shutdown internal plugin: %w", err)
	}
	return newTaskService(ss.(shutdown.Service))
}

func RegisterPlugin() {
	plugin.Register(&plugin.Registration{
		Type: plugin.TTRPCPlugin,
		ID:   "task",
		Requires: []plugin.Type{
			plugin.InternalPlugin,
		},
		InitFn: ttrpcService,
	})
}
