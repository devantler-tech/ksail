---
title: CLI Options
parent: Configuration
layout: default
nav_order: 0
---

# KSail CLI Options

> [!IMPORTANT]
> This document is auto-generated, and is always up-to-date with the latest version of the KSail CLI.

KSail supports CLI options for configuring the behavior of KSail. These options can be used to override the default settings, or to alter the behavior of KSail in specific ways.

## `ksail`

```txt
Description:
  KSail is an SDK for Kubernetes. Ship k8s with ease!

Usage:
  ksail [command] [options]

Options:
  --version       Show version information
  -?, -h, --help  Show help and usage information

Commands:
  up       Create a cluster
  down     Destroy a cluster
  update   Update a cluster
  start    Start a cluster
  stop     Stop a cluster
  init     Initialize a cluster
  lint     Lint manifests for a cluster
  list     List active clusters
  debug    Debug a cluster (❤️ K9s)
  gen      Generate a resource.
  secrets  Manage secrets
```

## `ksail up`

```txt
Description:
  Create a cluster

Usage:
  ksail up [options]

Options:
  -c, --context <context>                           The kubernetes context to use. [default: kind-ksail-default]
  -k, --kubeconfig <kubeconfig>                     Path to kubeconfig file. [default: /Users/nikolaiemildamm/.kube/config]
  -t, --timeout <timeout>                           The time to wait for each kustomization to become ready. [default: 5m]
  -fsu, --flux-source-url <flux-source-url>         Flux source URL for reconciling GitOps resources. [default: oci://ksail-registry:5000/ksail-registry]
  -n, --name <name>                                 The name of the cluster. [default: ksail-default]
  --cni <Cilium|Default>                            The CNI to use. [default: Default]
  -dt, --deployment-tool <Flux>                     The Deployment tool to use for updating the state of the cluster. [default: Flux]
  -dc, --distribution-config <distribution-config>  Path to the distribution configuration file. [default: kind.yaml]
  -d, --distribution <K3s|Native>                   The distribution to use for the cluster. [default: Native]
  -p, --provider <Docker>                           The provider to use for provisioning the cluster. [default: Docker]
  -kp, --kustomization-path <kustomization-path>    The path to the root kustomization directory. [default: k8s]
  -mr, --mirror-registries                          Enable mirror registries for the project. [default: True]
  -sm, --secret-manager <None|SOPS>                 Configure which secret manager to use. [default: None]
  -l, --lint                                        Lit manifests. [default: True'
  -r, --reconcile                                   Reconcile manifests. [default: True]
  -?, -h, --help                                    Show help and usage information
```

## `ksail down`

```txt
Description:
  Destroy a cluster

Usage:
  ksail down [options]

Options:
  -fsu, --flux-source-url <flux-source-url>  Flux source URL for reconciling GitOps resources. [default: oci://ksail-registry:5000/ksail-registry]
  -n, --name <name>                          The name of the cluster. [default: ksail-default]
  -d, --distribution <K3s|Native>            The distribution to use for the cluster. [default: Native]
  -p, --provider <Docker>                    The provider to use for provisioning the cluster. [default: Docker]
  -mr, --mirror-registries                   Enable mirror registries for the project. [default: True]
  -?, -h, --help                             Show help and usage information
```

## `ksail update`

```txt
Description:
  Update a cluster

Usage:
  ksail update [options]

Options:
  -c, --context <context>                         The kubernetes context to use. [default: kind-ksail-default]
  -k, --kubeconfig <kubeconfig>                   Path to kubeconfig file. [default: /Users/nikolaiemildamm/.kube/config]
  -kp, --kustomization-path <kustomization-path>  The path to the root kustomization directory. [default: k8s]
  -l, --lint                                      Lit manifests. [default: True]
  -r, --reconcile                                 Reconcile manifests. [default: True]
  -?, -h, --help                                  Show help and usage information
```

## `ksail start`

```txt
Description:
  Start a cluster

Usage:
  ksail start [options]

Options:
  -c, --context <context>          The kubernetes context to use. [default: kind-ksail-default]
  -n, --name <name>                The name of the cluster. [default: ksail-default]
  -d, --distribution <K3s|Native>  The distribution to use for the cluster. [default: Native]
  -p, --provider <Docker>          The provider to use for provisioning the cluster. [default: Docker]
  -?, -h, --help                   Show help and usage information
```

## `ksail stop`

```txt
Description:
  Stop a cluster

Usage:
  ksail stop [options]

Options:
  -n, --name <name>                The name of the cluster. [default: ksail-default]
  -d, --distribution <K3s|Native>  The distribution to use for the cluster. [default: Native]
  -p, --provider <Docker>          The provider to use for provisioning the cluster. [default: Docker]
  -?, -h, --help                   Show help and usage information
```

## `ksail init`

```txt
Description:
  Initialize a cluster

Usage:
  ksail init [options]

Options:
  -o, --overwrite                                   Overwrite existing files. [default: False]
  -n, --name <name>                                 The name of the cluster. [default: ksail-default]
  --cni <Cilium|Default>                            The CNI to use. [default: Default]
  -c, --config <config>                             The path to the ksail configuration file. [default: ksail.yaml]
  -dc, --distribution-config <distribution-config>  Path to the distribution configuration file. [default: kind.yaml]
  -d, --distribution <K3s|Native>                   The distribution to use for the cluster. [default: Native]
  -p, --provider <Docker>                           The provider to use for provisioning the cluster. [default: Docker]
  -kp, --kustomization-path <kustomization-path>    The path to the root kustomization directory. [default: k8s]
  -mr, --mirror-registries                          Enable mirror registries for the project. [default: True]
  -sm, --secret-manager <None|SOPS>                 Configure which secret manager to use. [default: None]
  -?, -h, --help                                    Show help and usage information
```

## `ksail lint`

```txt
Description:
  Lint manifests for a cluster

Usage:
  ksail lint [options]

Options:
  -kp, --kustomization-path <kustomization-path>  The path to the root kustomization directory. [default: k8s]
  -?, -h, --help                                  Show help and usage information
```

## `ksail list`

```txt
Description:
  List active clusters

Usage:
  ksail list [options]

Options:
  -p, --provider <Docker>          The provider to use for provisioning the cluster. [default: Docker]
  -d, --distribution <K3s|Native>  The distribution to use for the cluster. [default: Native]
  -a, --all                        List clusters from all distributions. [default: False]
  -?, -h, --help                   Show help and usage information
```

## `ksail debug`

```txt
Description:
  Debug a cluster (❤️ K9s)

Usage:
  ksail debug [options]

Options:
  -k, --kubeconfig <kubeconfig>  Path to kubeconfig file. [default: /Users/nikolaiemildamm/.kube/config]
  -c, --context <context>        The kubernetes context to use. [default: kind-ksail-default]
  -e, --editor <Nano|Vim>        Editor to use. [default: Nano]
  -?, -h, --help                 Show help and usage information
```

## `ksail gen`

```txt
Description:
  Generate a resource.

Usage:
  ksail gen [command] [options]

Options:
  -o, --overwrite  Overwrite existing files. [default: False]
  -?, -h, --help   Show help and usage information

Commands:
  cert-manager  Generate a CertManager resource.
  config        Generate a configuration file.
  flux          Generate a Flux resource.
  kustomize     Generate a Kustomize resource.
  native        Generate a native Kubernetes resource.
```

## `ksail gen cert-manager`

```txt
Description:
  Generate a CertManager resource.

Usage:
  ksail gen cert-manager [command] [options]

Options:
  -o, --overwrite  Overwrite existing files. [default: False]
  -?, -h, --help   Show help and usage information

Commands:
  certificate     Generate a 'cert-manager.io/v1/Certificate' resource.
  cluster-issuer  Generate a 'cert-manager.io/v1/ClusterIssuer' resource.
```

## `ksail gen cert-manager certificate`

```txt
Description:
  Generate a 'cert-manager.io/v1/Certificate' resource.

Usage:
  ksail gen cert-manager certificate [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./certificate.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen cert-manager cluster-issuer`

```txt
Description:
  Generate a 'cert-manager.io/v1/ClusterIssuer' resource.

Usage:
  ksail gen cert-manager cluster-issuer [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./cluster-issuer.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen config`

```txt
Description:
  Generate a configuration file.

Usage:
  ksail gen config [command] [options]

Options:
  -o, --overwrite  Overwrite existing files. [default: False]
  -?, -h, --help   Show help and usage information

Commands:
  k3d    Generate a 'k3d.io/v1alpha5/Simple' resource.
  ksail  Generate a 'ksail.io/v1alpha1/Cluster' resource.
  sops   Generate a SOPS configuration file.
```

## `ksail gen config k3d`

```txt
Description:
  Generate a 'k3d.io/v1alpha5/Simple' resource.

Usage:
  ksail gen config k3d [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./k3d.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen config ksail`

```txt
Description:
  Generate a 'ksail.io/v1alpha1/Cluster' resource.

Usage:
  ksail gen config ksail [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./ksail.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen config sops`

```txt
Description:
  Generate a SOPS configuration file.

Usage:
  ksail gen config sops [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./.sops.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen flux`

```txt
Description:
  Generate a Flux resource.

Usage:
  ksail gen flux [command] [options]

Options:
  -o, --overwrite  Overwrite existing files. [default: False]
  -?, -h, --help   Show help and usage information

Commands:
  helm-release     Generate a 'helm.toolkit.fluxcd.io/v2/HelmRelease' resource.
  helm-repository  Generate a 'source.toolkit.fluxcd.io/v1/HelmRepository' resource.
  kustomization    Generate a 'kustomize.toolkit.fluxcd.io/v1/Kustomization' resource.
```

## `ksail gen flux helm-release`

```txt
Description:
  Generate a 'helm.toolkit.fluxcd.io/v2/HelmRelease' resource.

Usage:
  ksail gen flux helm-release [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./helm-release.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen flux helm-repository`

```txt
Description:
  Generate a 'source.toolkit.fluxcd.io/v1/HelmRepository' resource.

Usage:
  ksail gen flux helm-repository [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./helm-repository.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen flux kustomization`

```txt
Description:
  Generate a 'kustomize.toolkit.fluxcd.io/v1/Kustomization' resource.

Usage:
  ksail gen flux kustomization [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./flux-kustomization.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen kustomize`

```txt
Description:
  Generate a Kustomize resource.

Usage:
  ksail gen kustomize [command] [options]

Options:
  -o, --overwrite  Overwrite existing files. [default: False]
  -?, -h, --help   Show help and usage information

Commands:
  component      Generate a 'kustomize.config.k8s.io/v1alpha1/Component' resource.
  kustomization  Generate a 'kustomize.config.k8s.io/v1beta1/Kustomization' resource.
```

## `ksail gen kustomize component`

```txt
Description:
  Generate a 'kustomize.config.k8s.io/v1alpha1/Component' resource.

Usage:
  ksail gen kustomize component [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./kustomization.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen kustomize kustomization`

```txt
Description:
  Generate a 'kustomize.config.k8s.io/v1beta1/Kustomization' resource.

Usage:
  ksail gen kustomize kustomization [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./kustomization.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native`

```txt
Description:
  Generate a native Kubernetes resource.

Usage:
  ksail gen native [command] [options]

Options:
  -o, --overwrite  Overwrite existing files. [default: False]
  -?, -h, --help   Show help and usage information

Commands:
  cluster-role-binding       Generate a 'rbac.authorization.k8s.io/v1/ClusterRoleBinding' resource.
  cluster-role               Generate a 'rbac.authorization.k8s.io/v1/ClusterRole' resource.
  namespace                  Generate a 'core/v1/Namespace' resource.
  network-policy             Generate a 'networking.k8s.io/v1/NetworkPolicy' resource.
  persistent-volume          Generate a 'core/v1/PersistentVolume' resource.
  resource-quota             Generate a 'core/v1/ResourceQuota' resource.
  role-binding               Generate a 'rbac.authorization.k8s.io/v1/RoleBinding' resource.
  role                       Generate a 'rbac.authorization.k8s.io/v1/Role' resource.
  service-account            Generate a 'core/v1/ServiceAccount' resource.
  config-map                 Generate a 'core/v1/ConfigMap' resource.
  persistent-volume-claim    Generate a 'core/v1/PersistentVolumeClaim' resource.
  secret                     Generate a 'core/v1/Secret' resource.
  horizontal-pod-autoscaler  Generate a 'autoscaling/v2/HorizontalPodAutoscaler' resource.
  pod-disruption-budget      Generate a 'policy/v1/PodDisruptionBudget' resource.
  priority-class             Generate a 'scheduling.k8s.io/v1/PriorityClass' resource.
  ingress                    Generate a 'networking.k8s.io/v1/Ingress' resource.
  service                    Generate a 'core/v1/Service' resource.
  cron-job                   Generate a 'batch/v1/CronJob' resource.
  daemon-set                 Generate a 'apps/v1/DaemonSet' resource.
  deployment                 Generate a 'apps/v1/Deployment' resource.
  job                        Generate a 'batch/v1/Job' resource.
  stateful-set               Generate a 'apps/v1/StatefulSet' resource.
```

## `ksail gen native cluster-role-binding`

```txt
Description:
  Generate a 'rbac.authorization.k8s.io/v1/ClusterRoleBinding' resource.

Usage:
  ksail gen native cluster-role-binding [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./cluster-role-binding.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native cluster-role`

```txt
Description:
  Generate a 'rbac.authorization.k8s.io/v1/ClusterRole' resource.

Usage:
  ksail gen native cluster-role [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./cluster-role.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native namespace`

```txt
Description:
  Generate a 'core/v1/Namespace' resource.

Usage:
  ksail gen native namespace [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./namespace.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native network-policy`

```txt
Description:
  Generate a 'networking.k8s.io/v1/NetworkPolicy' resource.

Usage:
  ksail gen native network-policy [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./network-policy.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native persistent-volume`

```txt
Description:
  Generate a 'core/v1/PersistentVolume' resource.

Usage:
  ksail gen native persistent-volume [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./persistent-volume.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native resource-quota`

```txt
Description:
  Generate a 'core/v1/ResourceQuota' resource.

Usage:
  ksail gen native resource-quota [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./resource-quota.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native role-binding`

```txt
Description:
  Generate a 'rbac.authorization.k8s.io/v1/RoleBinding' resource.

Usage:
  ksail gen native role-binding [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./role-binding.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native role`

```txt
Description:
  Generate a 'rbac.authorization.k8s.io/v1/Role' resource.

Usage:
  ksail gen native role [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./role.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native service-account`

```txt
Description:
  Generate a 'core/v1/ServiceAccount' resource.

Usage:
  ksail gen native service-account [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./service-account.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native config-map`

```txt
Description:
  Generate a 'core/v1/ConfigMap' resource.

Usage:
  ksail gen native config-map [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./config-map.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native persistent-volume-claim`

```txt
Description:
  Generate a 'core/v1/PersistentVolumeClaim' resource.

Usage:
  ksail gen native persistent-volume-claim [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./persistent-volume-claim.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native secret`

```txt
Description:
  Generate a 'core/v1/Secret' resource.

Usage:
  ksail gen native secret [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./secret.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native horizontal-pod-autoscaler`

```txt
Description:
  Generate a 'autoscaling/v2/HorizontalPodAutoscaler' resource.

Usage:
  ksail gen native horizontal-pod-autoscaler [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./horizontal-pod-autoscaler.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native pod-disruption-budget`

```txt
Description:
  Generate a 'policy/v1/PodDisruptionBudget' resource.

Usage:
  ksail gen native pod-disruption-budget [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./pod-disruption-budget.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native priority-class`

```txt
Description:
  Generate a 'scheduling.k8s.io/v1/PriorityClass' resource.

Usage:
  ksail gen native priority-class [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./priority-class.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native ingress`

```txt
Description:
  Generate a 'networking.k8s.io/v1/Ingress' resource.

Usage:
  ksail gen native ingress [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./ingress.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native service`

```txt
Description:
  Generate a 'core/v1/Service' resource.

Usage:
  ksail gen native service [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./service.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native cron-job`

```txt
Description:
  Generate a 'batch/v1/CronJob' resource.

Usage:
  ksail gen native cron-job [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./cron-job.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native daemon-set`

```txt
Description:
  Generate a 'apps/v1/DaemonSet' resource.

Usage:
  ksail gen native daemon-set [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./daemon-set.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native deployment`

```txt
Description:
  Generate a 'apps/v1/Deployment' resource.

Usage:
  ksail gen native deployment [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./deployment.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native job`

```txt
Description:
  Generate a 'batch/v1/Job' resource.

Usage:
  ksail gen native job [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./job.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail gen native stateful-set`

```txt
Description:
  Generate a 'apps/v1/StatefulSet' resource.

Usage:
  ksail gen native stateful-set [options]

Options:
  -o, --output <output>  A file or directory path. [default: ./stateful-set.yaml]
  -o, --overwrite        Overwrite existing files. [default: False]
  -?, -h, --help         Show help and usage information
```

## `ksail secrets`

```txt
Description:
  Manage secrets

Usage:
  ksail secrets [command] [options]

Options:
  -sm, --secret-manager <None|SOPS>  Configure which secret manager to use. [default: None]
  -?, -h, --help                     Show help and usage information

Commands:
  encrypt <path>       Encrypt a file
  decrypt <path>       Decrypt a file
  edit <path>          Edit an encrypted file
  add                  Add a new encryption key
  rm <public-key>      Remove an existing encryption key
  list                 List keys
  import <key>         Import a key from stdin or a file
  export <public-key>  Export a key to a file
```

## `ksail secrets encrypt`

```txt
Description:
  Encrypt a file

Usage:
  ksail secrets encrypt <path> [options]

Arguments:
  <path>  The path to the file to encrypt.

Options:
  -pk, --public-key <public-key>     The public key.
  -ip, --in-place                    In-place decryption/encryption. [default: False]
  -o, --output <output>              A file or directory path. []
  -sm, --secret-manager <None|SOPS>  Configure which secret manager to use. [default: None]
  -?, -h, --help                     Show help and usage information
```

## `ksail secrets decrypt`

```txt
Description:
  Decrypt a file

Usage:
  ksail secrets decrypt <path> [options]

Arguments:
  <path>  The path to the file to decrypt.

Options:
  -ip, --in-place                    In-place decryption/encryption. [default: False]
  -o, --output <output>              A file or directory path. []
  -sm, --secret-manager <None|SOPS>  Configure which secret manager to use. [default: None]
  -?, -h, --help                     Show help and usage information
```

## `ksail secrets edit`

```txt
Description:
  Edit an encrypted file

Usage:
  ksail secrets edit <path> [options]

Arguments:
  <path>  The path to the file to edit.

Options:
  -e, --editor <Nano|Vim>            Editor to use. [default: Nano]
  -sm, --secret-manager <None|SOPS>  Configure which secret manager to use. [default: None]
  -?, -h, --help                     Show help and usage information
```

## `ksail secrets add`

```txt
Description:
  Add a new encryption key

Usage:
  ksail secrets add [options]

Options:
  -sm, --secret-manager <None|SOPS>  Configure which secret manager to use. [default: None]
  -?, -h, --help                     Show help and usage information
```

## `ksail secrets rm`

```txt
Description:
  Remove an existing encryption key

Usage:
  ksail secrets rm <public-key> [options]

Arguments:
  <public-key>  Public key matching existing encryption key

Options:
  -sm, --secret-manager <None|SOPS>  Configure which secret manager to use. [default: None]
  -?, -h, --help                     Show help and usage information
```

## `ksail secrets list`

```txt
Description:
  List keys

Usage:
  ksail secrets list [options]

Options:
  -spk, --show-private-keys          Show private keys. [default: False]
  -a, --all                          Show all keys. [default: False]
  -sm, --secret-manager <None|SOPS>  Configure which secret manager to use. [default: None]
  -?, -h, --help                     Show help and usage information
```

## `ksail secrets import`

```txt
Description:
  Import a key from stdin or a file

Usage:
  ksail secrets import <key> [options]

Arguments:
  <key>  The encryption key to import

Options:
  -sm, --secret-manager <None|SOPS>  Configure which secret manager to use. [default: None]
  -?, -h, --help                     Show help and usage information
```

## `ksail secrets export`

```txt
Description:
  Export a key to a file

Usage:
  ksail secrets export <public-key> [options]

Arguments:
  <public-key>  The public key for the encryption key to export

Options:
  -o, --output <output>              A file or directory path. []
  -sm, --secret-manager <None|SOPS>  Configure which secret manager to use. [default: None]
  -?, -h, --help                     Show help and usage information
```
