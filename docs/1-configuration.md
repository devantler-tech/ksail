# Configuration

Everything that KSail does and can do is configurable in a declarative manner. This allows you to manage everything from the configuration of KSail itself, the configuration of the Kubernetes distribution you are using, or even the configuration of SOPS for managing secrets in Git repositories.

## KSail Configuration

KSail provides a fully declarative configuration for managing everything it does. This configuration is stored in a `ksail.yaml` file in the root of your project.

KSail uses the following priority when loading in the configuration:

1. The default configuration
2. The configuration in the `ksail.yaml` file in the root of your project
3. Command-line flags

So, you can always override the configuration in the `ksail.yaml` file with command-line flags, if you need to.

```yaml
apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  # The name of the cluster
  name: ksail-default

spec:
  # The path to the kubeconfig file
  kubeconfig: ~/.kube/config
  # The context to use
  context: kind-ksail-default
  # The time before timing out a command
  timeout: 5m
  # The directory where the manifests are stored
  manifestsDirectory: ./k8s
  # The directory where the root Kustomization is stored
  KustomizationDirectory: ./k8s/clusters/ksail-default/flux-system
  # The path to the kubernetes distribution's config file
  configPath: kind.yaml
  # The kubernetes distribution to use
  distribution: kind
  # The GitOps tool to use
  gitOpsTool: flux
  # The container engine to use
  containerEngine: docker
  # Whether to use SOPS for managing secrets
  sops: false

  # The registries to create and use both the GitOps
  registries: OCI source and mirror registries
      # The name of the registry
    - name: ksail-registry
      # The host port for the registry
      hostPort: 5555
      # Whether the registry is the GitOps OCI source
      isGitOpsOCISource: true
      # The provider to create the registry with
      provider: docker
    - name: registry.k8s.io
      hostPort: 5556
      isGitOpsOCISource: false
      # The proxy to use for the registry
      proxy:
        # The URL for the upstream registry
        url: https://registry.k8s.io/
        # Whether the registry is insecure
        insecure: false
      provider: docker
    - name: docker.io
      hostPort: 5557
      isGitOpsOCISource: false
      proxy:
        url: https://registry-1.docker.io/
        insecure: false
      provider: docker
    - name: ghcr.io
      hostPort: 5558
      isGitOpsOCISource: false
      proxy:
        url: https://ghcr.io/
        insecure: false
      provider: docker
    - name: gcr.io
      hostPort: 5559
      isGitOpsOCISource: false
      proxy:
        url: https://gcr.io/
        insecure: false
      provider: docker
    - name: mcr.microsoft.com
      hostPort: 5560
      isGitOpsOCISource: false
      proxy:
        url: https://mcr.microsoft.com/
        insecure: false
      provider: docker
    - name: quay.io
      hostPort: 5561
      isGitOpsOCISource: false
      proxy:
        url: https://quay.io/
        insecure: false
      provider: docker

  # The options for `ksail check`
  checkOptions: {}

  # The options for `ksail debug`
  debugOptions:
    # The text editor to use with K9s
    editor: nano

  # The options for `ksail down`
  downOptions:
    # Whether to destroy registries with the cluster
    registries: false

  # The options for `ksail gen`
  genOptions: {}

  # The options for `ksail init`
  initOptions:
    # Whether to use a declarative configuration for ksail
    declarativeConfig: true
    # Whether to include a few helm releases to get you started
    helmReleases: false
    # Whether to use flux post build variables for templating variables in manifests
    postBuildVariables: false
    # The path to the output directory
    outputDirectory: ./
    # The template to initialize the project with
    template: simple
    # The kustomize flows to generate. The first is dependent on the second, and so on
    kustomizeFlows:
      - apps
      - infrastructure
      - infrastructure/controllers
    # The kustomize hooks to generate. Places where you want to be able to hook into the kustomize flow to add/modify resources
    kustomizeHooks: []

  # The options for `ksail lint`
  lintOptions: {}

  # The options for `ksail list`
  listOptions:
    # Whether to list clusters from all supported distributions
    all: false

  # The options for `ksail sops`
  sopsOptions: {}

  # The options for `ksail start`
  startOptions: {}

  # The options for `ksail stop`
  stopOptions: {}

  # The options for `ksail up`
  upOptions:
    # Whether to destroy and recreate the cluster
    destroy: false
    # Whether to lint the manifests before creating the cluster
    lint: true
    # Whether to reconcile changes to the cluster after creating it
    reconcile: true

  # The options for `ksail update`
  updateOptions:
    # Whether to lint the manifests before updating the cluster
    lint: true
    # Whether to reconcile changes to the cluster after updating it
    reconcile: true
```

## Distribution Configuration

### Kind

To configure Kind, you use a `kind.yaml` file in the root of your project. To learn more about the configuration options for Kind, you can check out the [official Kind documentation](https://kind.sigs.k8s.io/docs/user/configuration/).

### K3d

To configure K3d, you use a `k3d.yaml` file in the root of your project. To learn more about the configuration options for K3d, you can check out the [official K3d documentation](https://k3d.io/v5.7.4/usage/configfile/#all-options-example).

## SOPS Configuration

To configure SOPS, you use a `.sops.yaml` file in the root of your project. To learn more about the configuration options for SOPS, you can check out the [official SOPS documentation](https://getsops.io/docs/#using-sopsyaml-conf-to-select-kms-pgp-and-age-for-new-files).
