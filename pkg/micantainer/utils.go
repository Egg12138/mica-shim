package micantainer

import (
	"context"

	"github.com/containerd/errdefs"
)

func createContainerInSandbox(ctx context.Context, sandbox SandboxTraits, config *ContainerConfig) (*RTOSTask, error) {
	return nil, errdefs.ErrNotImplemented
}