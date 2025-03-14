# KSail CLI

## `ksail`

```txt
Description:
  KSail is an SDK for building GitOps enabled clusters.

Usage:
  ksail [command] [options]

Options:
  --version       Show version information
  -?, -h, --help  Show help and usage information

Commands:
  up      Create a cluster
  update  Update a cluster
  start   Start a cluster
  stop    Stop a cluster
  down    Destroy a cluster
  init    Initialize a cluster
  lint    Lint manifests for a cluster
  list    List active clusters
  debug   Debug a cluster (❤️ K9s)
  gen     Generate a resource.
```

## `ksail debug`

```txt
Description:
  Debug a cluster (❤️ K9s)

Usage:
  ksail debug [options]

Options:
  -k, --kubeconfig <kubeconfig>  Path to kubeconfig file
  -c, --context <context>        The kubernetes context to use
  -e, --editor <Nano|Vim>        Editor to use
  -?, -h, --help                 Show help and usage information
```

## `ksail down`

```txt
Description:
  Destroy a cluster

Usage:
  ksail down [options]

Options:
  -n, --name <name>              The name of the cluster.
  -d, --distribution <K3d|Kind>  The distribution to use for the cluster.
  -r, --registries               Delete registries
  -?, -h, --help                 Show help and usage information
```

## `ksail gen`

```txt
Description:
  Generate a resource.

Usage:
  ksail gen [command] [options]

Options:
  -?, -h, --help  Show help and usage information

Commands:
  cert-manager  Generate a CertManager resource.
  config        Generate a configuration file.
  flux          Generate a Flux resource.
  kustomize     Generate a Kustomize resource.
  native        Generate a native Kubernetes resource from one of the available categories.
```

## `ksail init`

```txt
Description:
  Initialize a cluster

Usage:
  ksail init [options]

Options:
  -n, --name <name>              The name of the cluster.
  -dc, --declarative-config      Generate a ksail.yaml file, to configure the KSail CLI declaratively.
  -fpbv, --flux-post-build-variables   Generate ConfigMaps and Secrets for flux post-build-variables.
  -d, --distribution <K3d|Kind>  The distribution to use for the cluster.
  -o, --output <output>          Location to place the generated cluster output.
  -s, --sops                     Enable SOPS support.
  -t, --template <Simple>        The template to use for the initialized cluster.
  -?, -h, --help                 Show help and usage information
```

## `ksail lint`

```txt
Description:
  Lint manifests for a cluster

Usage:
  ksail lint [options]

Options:
  -n, --name <name>  The name of the cluster.
  -p, --path <path>  Path to the manifests directory
  -?, -h, --help     Show help and usage information
```

## `ksail list`

```txt
Description:
  List active clusters

Usage:
  ksail list [options]

Options:
  -a, --all       List clusters from all distributions
  -?, -h, --help  Show help and usage information
```

## `ksail start`

```txt
Description:
  Start a cluster

Usage:
  ksail start [options]

Options:
  -n, --name <name>  The name of the cluster.
  -?, -h, --help     Show help and usage information
```

## `ksail stop`

```txt
Description:
  Stop a cluster

Usage:
  ksail stop [options]

Options:
  -n, --name <name>  The name of the cluster.
  -?, -h, --help     Show help and usage information
```

## `ksail up`

```txt
Description:
  Create a cluster

Usage:
  ksail up [options]

Options:
  -n, --name <name>                               The name of the cluster.
  -c, --config <config>                           Path to the cluster configuration file
  -d, --distribution <K3d|Kind>                   The distribution to use for the cluster.
  -p, --path <path>                               Path to the manifests directory
  -kp, --kustomization-path <kustomization-path>  Path to the root kustomization directory
  -t, --timeout <timeout>                         The time to wait for each kustomization to become ready.
  -s, --sops                                      Enable SOPS support.
  -l, --lint                                      Lint manifests before pushing an update
  -r, --reconcile                                 Reconcile manifests after pushing an update
  -?, -h, --help                                  Show help and usage information
```

## `ksail update`

```txt
Description:
  Update a cluster

Usage:
  ksail update [options]

Options:
  -n, --name <name>        The name of the cluster.
  -p, --path <path>        Path to the manifests directory
  -l, --lint               Lint manifests before pushing an update
  -r, --reconcile          Reconcile manifests after pushing an update
  -t, --timeout <timeout>  The time to wait for each kustomization to become ready.
  -?, -h, --help           Show help and usage information
```
