# Configuration

## Micran configuration

## Container configurations (optimize the parsing logic for performance)

### source by priority

priority():
1. CRI annotations
1. Containerd annotations
1. default configuration files:(drop-in confs override default_conf)
> `MICRAN_CONF_DIR/*.toml` (default: /etc/micran/conf.d/*.toml)
>
> `MICRAN_DEFAULT_CONF` (default: /etc/micran/conf.toml)

### k8s view

## OCI image configurations

### source by priority

* ocispec
* customized annotations
* <rootfs>/client.conf

### content

#### mica client

OCI image provides in `client.conf`:
* OS name (runtime do not care it)
* OS firmware path (relative path)
* Pedestal needed configs ()
* Limits of the os
* Blacklist Pedestal

name= // useless, we alway pass shortID(containerID) to micad
os=os
maxcpu= // used  by runtime
ped= // useless, ped is alway the host ped type
pedConf= // for xen, pedconfg points to relativepath of <image.bin>
devconf=...


## Mica configuration


mica daemon lives longer than containers does.It is not recommended to configurate mica daemon dynamically.

The configuration procedure will be skipped until mica daemon is detected not running.



---


# Kata configurations

Override Priority (highest to lowest):
1. Container Annotations (OCI spec annotations)
2. Runtime Configuration (TOML configuration files + drop-ins)
3. Default Values (built-in defaults)

Configuration Processing Function Chain

```
LoadConfiguration(configPath)
├── initConfig() - creates default RuntimeConfig
├── decodeConfig(configPath) - parses TOML files
│   ├── getDefaultConfigFile() or ResolvePath()
│   ├── toml.Decode() - main config
│   └── decodeDropIns() - config.d/*.toml files
├── updateRuntimeConfig() - applies TOML settings
│   ├── updateRuntimeConfigHypervisor()
│   └── updateRuntimeConfigAgent()
└── Returns RuntimeConfig

SandboxConfig(ociSpec, runtimeConfig, ...)
├── ContainerConfig() - creates container config from OCI
├── Creates SandboxConfig with runtimeConfig values
└── addAnnotations() - applies OCI annotations
    ├── addAssetAnnotations()
    ├── addHypervisorConfigOverrides()
    ├── addRuntimeConfigOverrides()
    └── addAgentConfigOverrides()
```

Configuration Structures

Main Configuration Types:
- RuntimeConfig (src/runtime/pkg/oci/utils.go:111) - Aggregates all runtime settings
- SandboxConfig (src/runtime/virtcontainers/sandbox.go:131) - Sandbox-level configuration
- ContainerConfig (src/runtime/virtcontainers/container.go:237) - Container-level configuration
- HypervisorConfig (src/runtime/virtcontainers/hypervisor.go:318) - Hypervisor-specific
settings

Configuration Sources

1. TOML Configuration Files:
- Main config: /etc/kata-containers/configuration.toml
- Drop-ins: /etc/kata-containers/configuration.d/*.toml
- Structure: tomlConfig in src/runtime/pkg/katautils/config.go:70

2. OCI Annotations:
- Prefix: io.katacontainers.config.hypervisor.*
- Examples: io.katacontainers.config.hypervisor.path,
io.katacontainers.config.hypervisor.kernel
- Processing: addAnnotations() in src/runtime/pkg/oci/utils.go:419

3. OCI Image Configuration:
- Container spec: oci.Spec from bundle's config.json
- Processing: ContainerConfig() in src/runtime/pkg/oci/utils.go:1191

Override Mechanism

Annotations override TOML configuration as demonstrated in:
// src/runtime/pkg/oci/utils.go:582
if value, ok := ocispec.Annotations[vcAnnotations.HypervisorPath]; ok {
    if !checkPathIsInGlobs(runtime.HypervisorConfig.HypervisorPathList, value) {
        return fmt.Errorf("hypervisor %v required from annotation is not valid", value)
    }
    config.HypervisorConfig.HypervisorPath = value
}

Configuration Processing Flow

1. Runtime Initialization: LoadConfiguration() loads TOML files and drop-ins
2. Sandbox Creation: SandboxConfig() creates sandbox config from runtime config
3. Annotation Processing: addAnnotations() applies OCI annotations, overriding TOML values
4. Container Creation: Container configs are added to sandbox config
5. Resource Management: Static vs dynamic resource sizing based on configuration

Security and Validation

- Path validation: Annotations must match allowlists in runtime config
- Annotation enablement: Only enabled annotations (via enable_annotations) are processed
- Type checking: Proper parsing of boolean, numeric, and string annotations
- Asset verification: Hash-based verification of hypervisor assets

This architecture provides a flexible, hierarchical configuration system where container-level
settings can override global defaults while maintaining security through validation and
allowlists.
