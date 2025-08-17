package oci

import (
	"fmt"

	defs "mica-shim/definitions"
	cntr "mica-shim/pkg/micantainer"

	ctrAnnotations "github.com/containerd/containerd/pkg/cri/annotations"
	podmanAnnotations "github.com/containers/podman/v4/pkg/annotations"
	dockershimAnnotations "github.com/kata-containers/kata-containers/src/runtime/virtcontainers/pkg/annotations/dockershim"
	"github.com/opencontainers/runtime-spec/specs-go"
)

type annotationContainerType struct {
	annotation    string
	containerType cntr.ContainerType
}

// CRI types list reference: kata-containers.
var (
	// CRIContainerTypeKeyList lists all the CRI keys that could define
	// the container type from annotations in the config.json.
	CRIContainerTypeKeyList = []string{ctrAnnotations.ContainerType, podmanAnnotations.ContainerType, dockershimAnnotations.ContainerTypeLabelKey}

	// CRISandboxNameKeyList lists all the CRI keys that could define
	// the sandbox ID from annotations in the config.json.
	CRISandboxNameKeyList = []string{ctrAnnotations.SandboxID, podmanAnnotations.SandboxID, dockershimAnnotations.SandboxIDLabelKey}

	// CRIContainerTypeList lists all the maps from CRI ContainerTypes annotations
	// to a virtcontainers ContainerType.
	CRIContainerTypeList = []annotationContainerType{
		{podmanAnnotations.ContainerTypeSandbox, cntr.PodSandbox},
		{podmanAnnotations.ContainerTypeContainer, cntr.PodContainer},
		{ctrAnnotations.ContainerTypeSandbox, cntr.PodSandbox},
		{ctrAnnotations.ContainerTypeContainer, cntr.PodContainer},
		{dockershimAnnotations.ContainerTypeLabelSandbox, cntr.PodSandbox},
		{dockershimAnnotations.ContainerTypeLabelContainer, cntr.PodContainer},
	}
)

func GetContainerType(spec *specs.Spec) (cntr.ContainerType, error) {
	for _, key := range CRIContainerTypeKeyList {
		containerType, ok := spec.Annotations[key]
		if !ok {
			continue
		}

		for _, t := range CRIContainerTypeList {
			if t.annotation == containerType {
				return t.containerType, nil
			}
		}
		return cntr.UnknownContainerType, fmt.Errorf("unknown container type: %s", containerType)
	}
	return cntr.SingleContainer, nil
}

func GetSandboxID(spec *specs.Spec) (string, error) {
	for _, key := range CRISandboxNameKeyList {
		sandboxID, ok := spec.Annotations[key]
		if ok {
			return sandboxID, nil
		}
	}
	return "", fmt.Errorf("sandbox ID not found in annotations")
}

func GetSandboxConfigPath(annotations map[string]string) string {
	return annotations[defs.SandboxConfigPathKey]
}
