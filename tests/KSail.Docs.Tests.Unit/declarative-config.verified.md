﻿```yaml
###################################################################
## autogenerated by src/KSail.Docs/DeclarativeConfigGenerator.cs ##
###################################################################
# The API version where the KSail Cluster object is defined. [default: ksail.io/v1alpha1]
apiVersion: ksail.io/v1alpha1
# The KSail Cluster object kind. [default: Cluster]
kind: Cluster
# The metadata of the KSail Cluster object.
metadata:
  # The name of the KSail object. [default: ksail-default]
  name: ksail-default
# The spec of the KSail Cluster object.
spec:
  # The options for connecting to the KSail cluster.
  connection:
    # The path to the kubeconfig file. [default: ~/.kube/config]
    kubeconfig: ~/.kube/config
    # The kube context. [default: kind-ksail-default]
    context: kind-ksail-default
    # The timeout for operations (10s, 5m, 1h). [default: 5m]
    timeout: 5m
  # The options for the KSail project.
  project:
    # The path to the ksail configuration file. [default: ksail.yaml]
    configPath: ksail.yaml
    # The path to the distribution configuration file. [default: kind.yaml]
    distributionConfigPath: kind.yaml
    # The path to the root kustomization directory. [default: k8s]
    kustomizationPath: k8s
    # The provider to use for running the KSail cluster. [default: Docker]
    containerEngine: Docker
    # The Kubernetes distribution to use. [default: Kind]
    distribution: Kind
    # The Deployment tool to use. [default: Kubectl]
    deploymentTool: Kubectl
    # The CNI to use. [default: Default]
    cni: Default
    # The CSI to use. [default: Default]
    csi: Default
    # The Ingress Controller to use. [default: Default]
    ingressController: Default
    # The Gateway Controller to use. [default: Default]
    gatewayController: Default
    # Whether to install Metrics Server. [default: true]
    metricsServer: true
    # Whether to use a secret manager. [default: None]
    secretManager: None
    # The editor to use for viewing files while debugging. [default: Nano]
    editor: Nano
    # Whether to set up mirror registries for the project. [default: true]
    mirrorRegistries: true
  # The options for the deployment tool.
  deploymentTool:
    # The options for the Flux deployment tool.
    flux:
      # The source for reconciling GitOps resources.
      source:
        # The URL of the repository. [default: oci://ksail-registry:5000/ksail-registry]
        url: oci://ksail-registry:5000/ksail-registry
  # The options for the distribution.
  distribution:
    # Show clusters from all supported distributions. [default: false]
    showAllClustersInListings: false
  # The options for the Secret Manager.
  secretManager:
    # The options for the SOPS secret manager.
    sops:
      # Public key used for encryption. [default: null]
      publicKey: ''
      # Use in-place decryption/encryption. [default: false]
      inPlace: false
      # Show all keys in the listed keys. [default: false]
      showAllKeysInListings: false
      # Show private keys in the listed keys. [default: false]
      showPrivateKeysInListings: false
  # The local registry for storing deployment artifacts.
  localRegistry:
    # The name of the registry. [default: ksail-registry]
    name: ksail-registry
    # The host port of the registry (if applicable). [default: 5555]
    hostPort: 5555
    # The registry provider. [default: Docker]
    provider: Docker
  # The options for the generator.
  generator:
    # Overwrite existing files. [default: false]
    overwrite: false
  # The mirror registries to create for the KSail cluster. [default: registry.k8s.io-proxy, docker.io-proxy, ghcr.io-proxy, gcr.io-proxy, mcr.microsoft.com-proxy, quay.io-proxy]
  mirrorRegistries:
  - # A proxy for the registry to use to proxy and cache images.
    proxy:
      # The URL of the upstream registry to proxy and cache images from. [default: https://registry-1.docker.io]
      url: https://registry.k8s.io/
    # The name of the registry. [default: ksail-registry]
    name: registry.k8s.io-proxy
    # The host port of the registry (if applicable). [default: 5555]
    hostPort: 5556
    # The registry provider. [default: Docker]
    provider: Docker
  - # A proxy for the registry to use to proxy and cache images.
    proxy:
      # The URL of the upstream registry to proxy and cache images from. [default: https://registry-1.docker.io]
      url: https://registry-1.docker.io/
    # The name of the registry. [default: ksail-registry]
    name: docker.io-proxy
    # The host port of the registry (if applicable). [default: 5555]
    hostPort: 5557
    # The registry provider. [default: Docker]
    provider: Docker
  - # A proxy for the registry to use to proxy and cache images.
    proxy:
      # The URL of the upstream registry to proxy and cache images from. [default: https://registry-1.docker.io]
      url: https://ghcr.io/
    # The name of the registry. [default: ksail-registry]
    name: ghcr.io-proxy
    # The host port of the registry (if applicable). [default: 5555]
    hostPort: 5558
    # The registry provider. [default: Docker]
    provider: Docker
  - # A proxy for the registry to use to proxy and cache images.
    proxy:
      # The URL of the upstream registry to proxy and cache images from. [default: https://registry-1.docker.io]
      url: https://gcr.io/
    # The name of the registry. [default: ksail-registry]
    name: gcr.io-proxy
    # The host port of the registry (if applicable). [default: 5555]
    hostPort: 5559
    # The registry provider. [default: Docker]
    provider: Docker
  - # A proxy for the registry to use to proxy and cache images.
    proxy:
      # The URL of the upstream registry to proxy and cache images from. [default: https://registry-1.docker.io]
      url: https://mcr.microsoft.com/
    # The name of the registry. [default: ksail-registry]
    name: mcr.microsoft.com-proxy
    # The host port of the registry (if applicable). [default: 5555]
    hostPort: 5560
    # The registry provider. [default: Docker]
    provider: Docker
  - # A proxy for the registry to use to proxy and cache images.
    proxy:
      # The URL of the upstream registry to proxy and cache images from. [default: https://registry-1.docker.io]
      url: https://quay.io/
    # The name of the registry. [default: ksail-registry]
    name: quay.io-proxy
    # The host port of the registry (if applicable). [default: 5555]
    hostPort: 5561
    # The registry provider. [default: Docker]
    provider: Docker
  # Options for publication of manifests.
  publication:
    # Publish manifests before applying changes to an existing cluster. [default: true]
    publishOnUpdate: true
  # Options for validating the KSail cluster.
  validation:
    # Validate the project files and configuration before creating a new cluster. [default: true]
    validateOnUp: true
    # Wait for reconciliation to succeed on a new cluster. [default: true]
    reconcileOnUp: true
    # Validate the project files and configuration before applying changes to an existing cluster. [default: true]
    validateOnUpdate: true
    # Wait for reconciliation to succeed on an existing cluster. [default: true]
    reconcileOnUpdate: true
    # Verbose output for validation or status checks. [default: false]
    verbose: false
```
