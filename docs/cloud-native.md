# MIca in CLoud-native

runtime: micantainer(micrun)

## join k8s with micran

```yaml
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
    name: micran
handler: micran
```

## join k3s with micran

k3s uses a controlled, limited search path for runtime discovery that is different from
the system $PATH environment variable. Runtime binaries must be placed in one of the predefined
directories in runtimesPath to be discovered automatically:

- /usr/local/nvidia/toolkit
- /opt/kwasm/bin
- /usr/sbin, /usr/local/sbin
- /usr/bin, /usr/local/bin

Hence you need to place containerd-shim-mica-v2 binary in one of these directories.

## use Kubeedge to manage a micran-enabled edge node

### Tips before start


### Select a container engine

1. contaienrd
2. isulad

### Containerd as endpoint

1. configure the default runtime to org.openeuler.mica.v2

### iSulad as endpoint
