package cntr

import (
	ctrAnnotations "github.com/containerd/containerd/pkg/cri/annotations"
	podmanAnnotations "github.com/containers/podman/v4/pkg/annotations"
	dockershimAnnotations "github.com/kata-containers/kata-containers/src/runtime/virtcontainers/pkg/annotations/dockershim"
)

type annotationContainerType struct {
	annotation    string
	containerType ContainerType
}

// CRI types list reference: kata-containers
var (
	// CRIContainerTypeKeyList lists all the CRI keys that could define
	// the container type from annotations in the config.json.
	CRIContainerTypeKeyList = []string{ctrAnnotations.ContainerType, podmanAnnotations.ContainerType, dockershimAnnotations.ContainerTypeLabelKey}

	// CRISandboxNameKeyList lists all the CRI keys that could define
	// the sandbox ID (sandbox ID) from annotations in the config.json.
	CRISandboxNameKeyList = []string{ctrAnnotations.SandboxID, podmanAnnotations.SandboxID, dockershimAnnotations.SandboxIDLabelKey}

	// CRIContainerTypeList lists all the maps from CRI ContainerTypes annotations
	// to a virtcontainers ContainerType.
	CRIContainerTypeList = []annotationContainerType{
		{podmanAnnotations.ContainerTypeSandbox, PodSandbox},
		{podmanAnnotations.ContainerTypeContainer, PodContainer},
		{ctrAnnotations.ContainerTypeSandbox, PodSandbox},
		{ctrAnnotations.ContainerTypeContainer, PodContainer},
		{dockershimAnnotations.ContainerTypeLabelSandbox, PodSandbox},
		{dockershimAnnotations.ContainerTypeLabelContainer, PodContainer},
	}
)
