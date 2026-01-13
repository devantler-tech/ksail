---
title: "Concepts"
nav_order: 5
---

# Concepts

KSail builds upon established Kubernetes technologies and patterns. This page provides an overview of key concepts with links to upstream documentation for deeper understanding.

## Kubernetes

[Kubernetes](https://kubernetes.io/) is an open-source container orchestration platform for automating deployment, scaling, and management of containerized applications.

**Key resources:**

- [Kubernetes Documentation](https://kubernetes.io/docs/home/)
- [Kubernetes Concepts](https://kubernetes.io/docs/concepts/)
- [kubectl Reference](https://kubernetes.io/docs/reference/kubectl/)

## Distributions

Kubernetes distributions package the Kubernetes components with additional tooling for specific use cases. KSail supports three distributions that can run on the Docker provider.

### Vanilla (implemented with Kind)

Vanilla uses [Kind](https://kind.sigs.k8s.io/) (Kubernetes in Docker) to run standard upstream Kubernetes clusters using Docker containers as nodes. This distribution provides upstream Kubernetes without modifications, making it ideal for testing against standard Kubernetes behavior.

**Key resources:**

- [Kind Documentation](https://kind.sigs.k8s.io/)
- [Kind Configuration](https://kind.sigs.k8s.io/docs/user/configuration/)
- [Kind Quick Start](https://kind.sigs.k8s.io/docs/user/quick-start/)

### K3s (implemented with K3d)

[K3s](https://k3s.io/) is a lightweight, certified Kubernetes distribution built for resource-constrained environments. KSail uses [K3d](https://k3d.io/) to run K3s clusters in Docker containers. K3s includes sensible defaults like an embedded load balancer, local storage provisioner, and metrics server.

**Key resources:**

- [K3s Documentation](https://docs.k3s.io/)
- [K3d Documentation](https://k3d.io/)
- [K3d Configuration](https://k3d.io/stable/usage/configfile/)

### Talos

[Talos Linux](https://www.talos.dev/) is a minimal, immutable operating system designed specifically for Kubernetes. Provides enhanced security through API-driven configuration with no shell access.

**Key resources:**

- [Talos Documentation](https://www.talos.dev/latest/)
- [Talos Configuration Reference](https://www.talos.dev/latest/reference/configuration/)
- [Talos Getting Started](https://www.talos.dev/latest/introduction/getting-started/)

## Providers

Providers are the infrastructure backends that run cluster nodes. KSail abstracts provider-specific operations so you can use the same workflow regardless of where your cluster runs.

### Docker

The Docker provider runs Kubernetes nodes as Docker containers on your local machine. This is the default provider for all distributions and requires only Docker to be installed.

**Supported distributions:** Vanilla, K3s, Talos

**Key resources:**

- [Docker Documentation](https://docs.docker.com/)
- [Docker Desktop](https://www.docker.com/products/docker-desktop/)

### Hetzner (planned)

KSail aims to support provisioning **Talos** clusters on **Hetzner** in the future.

See the [roadmap](https://github.com/devantler-tech/ksail/issues) for current status.

## Container Network Interface (CNI)

[CNI](https://www.cni.dev/) is a specification for configuring network interfaces in Linux containers. CNI plugins provide pod networking, network policies, and observability.

### Cilium

[Cilium](https://cilium.io/) is an eBPF-based CNI providing networking, security, and observability. Offers advanced features like transparent encryption and service mesh.

**Key resources:**

- [Cilium Documentation](https://docs.cilium.io/)
- [Cilium Concepts](https://docs.cilium.io/en/stable/overview/intro/)
- [Network Policies with Cilium](https://docs.cilium.io/en/stable/security/policy/)

### Calico

[Calico](https://www.tigera.io/project-calico/) provides networking and network security for Kubernetes. Known for its network policy enforcement capabilities.

**Key resources:**

- [Calico Documentation](https://docs.tigera.io/calico/latest/about/)
- [Calico Network Policy](https://docs.tigera.io/calico/latest/network-policy/)
- [Calico Getting Started](https://docs.tigera.io/calico/latest/getting-started/)

## Container Storage Interface (CSI)

[CSI](https://kubernetes-csi.github.io/docs/) is a standard for exposing storage systems to containerized workloads. CSI drivers provide persistent storage for stateful applications.

### Local Path Provisioner

[Local Path Provisioner](https://github.com/rancher/local-path-provisioner) creates PersistentVolumes using local storage on nodes. Suitable for development and single-node clusters.

**Key resources:**

- [Local Path Provisioner GitHub](https://github.com/rancher/local-path-provisioner)
- [Kubernetes Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
- [Storage Classes](https://kubernetes.io/docs/concepts/storage/storage-classes/)

## Metrics Server

[Metrics Server](https://github.com/kubernetes-sigs/metrics-server) collects resource metrics from kubelets and exposes them via the Kubernetes API. Required for Horizontal Pod Autoscaler (HPA) and `kubectl top`.

**Key resources:**

- [Metrics Server GitHub](https://github.com/kubernetes-sigs/metrics-server)
- [Resource Metrics Pipeline](https://kubernetes.io/docs/tasks/debug/debug-cluster/resource-metrics-pipeline/)
- [Horizontal Pod Autoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/)

## Kubelet CSR Approver

[Kubelet CSR Approver](https://github.com/postfinance/kubelet-csr-approver) automatically approves Certificate Signing Requests (CSRs) for kubelet serving certificates. When `serverTLSBootstrap: true` is enabled on kubelets, they request proper TLS certificates via CSR instead of using self-signed certificates. This controller approves those requests, enabling secure TLS communication between components like metrics-server and kubelets.

**Why it matters:**

- Metrics-server requires secure TLS communication with kubelets
- Without approved CSRs, kubelets use self-signed certificates that metrics-server rejects
- KSail automatically installs kubelet-csr-approver when metrics-server is enabled on Vanilla or Talos

**Key resources:**

- [Kubelet CSR Approver GitHub](https://github.com/postfinance/kubelet-csr-approver)
- [Kubernetes TLS Bootstrapping](https://kubernetes.io/docs/reference/access-authn-authz/kubelet-tls-bootstrapping/)
- [Certificate Signing Requests](https://kubernetes.io/docs/reference/access-authn-authz/certificate-signing-requests/)

## cert-manager

[cert-manager](https://cert-manager.io/) automates TLS certificate management in Kubernetes. Supports ACME (Let's Encrypt), self-signed, and external CA certificates.

**Key resources:**

- [cert-manager Documentation](https://cert-manager.io/docs/)
- [cert-manager Concepts](https://cert-manager.io/docs/concepts/)
- [Issuer Types](https://cert-manager.io/docs/configuration/)

## Policy Engines

Policy engines enforce security, compliance, and best practices in Kubernetes clusters through admission control and continuous validation.

### Kyverno

[Kyverno](https://kyverno.io/) is a Kubernetes-native policy engine designed for ease of use. Policies are written as Kubernetes resources using YAML, without requiring a new language.

**Key resources:**

- [Kyverno Documentation](https://kyverno.io/docs/)
- [Kyverno Policies](https://kyverno.io/policies/)
- [Policy Reports](https://kyverno.io/docs/policy-reports/)

### Gatekeeper

[OPA Gatekeeper](https://open-policy-agent.github.io/gatekeeper/) brings Open Policy Agent (OPA) to Kubernetes as an admission controller. Policies are written in Rego, a declarative policy language.

**Key resources:**

- [Gatekeeper Documentation](https://open-policy-agent.github.io/gatekeeper/website/docs/)
- [OPA Documentation](https://www.openpolicyagent.org/docs/latest/)
- [Gatekeeper Library](https://open-policy-agent.github.io/gatekeeper-library/website/)

## OCI Registries

[OCI Distribution](https://github.com/opencontainers/distribution-spec) defines a standard for storing and distributing container images and other artifacts.

**Key resources:**

- [OCI Distribution Specification](https://github.com/opencontainers/distribution-spec)
- [Docker Registry](https://distribution.github.io/distribution/)
- [OCI Artifacts](https://github.com/opencontainers/artifacts)

## GitOps

[GitOps](https://opengitops.dev/) is an operational framework using Git as the single source of truth for declarative infrastructure and applications.

### Flux

[Flux](https://fluxcd.io/) is a GitOps toolkit for Kubernetes that keeps clusters in sync with configuration stored in Git or OCI registries.

**Key resources:**

- [Flux Documentation](https://fluxcd.io/docs/)
- [Flux Concepts](https://fluxcd.io/flux/concepts/)
- [FluxInstance CRD](https://fluxcd.io/flux/components/)

### ArgoCD

[Argo CD](https://argo-cd.readthedocs.io/) is a declarative GitOps continuous delivery tool with a web UI for visualizing application state.

**Key resources:**

- [Argo CD Documentation](https://argo-cd.readthedocs.io/)
- [Argo CD Concepts](https://argo-cd.readthedocs.io/en/stable/core_concepts/)
- [Application CRD](https://argo-cd.readthedocs.io/en/stable/operator-manual/declarative-setup/)

## SOPS

[SOPS](https://github.com/getsops/sops) (Secrets OPerationS) is an editor for encrypted files supporting multiple key management backends.

**Key resources:**

- [SOPS Documentation](https://github.com/getsops/sops)
- [age Encryption](https://age-encryption.org/)
- [SOPS with Flux](https://fluxcd.io/flux/guides/mozilla-sops/)

### Key Management Systems

SOPS supports multiple key management backends:

| Provider        | Documentation                                                                |
|-----------------|------------------------------------------------------------------------------|
| age             | [age-encryption.org](https://age-encryption.org/)                            |
| PGP             | [GnuPG](https://gnupg.org/)                                                  |
| AWS KMS         | [AWS KMS Documentation](https://docs.aws.amazon.com/kms/)                    |
| GCP KMS         | [Cloud KMS Documentation](https://cloud.google.com/kms/docs)                 |
| Azure Key Vault | [Azure Key Vault Documentation](https://docs.microsoft.com/azure/key-vault/) |
| HashiCorp Vault | [Vault Documentation](https://developer.hashicorp.com/vault/docs)            |

## Kustomize

[Kustomize](https://kustomize.io/) is a template-free customization tool for Kubernetes manifests. It uses overlays to patch base configurations.

**Key resources:**

- [Kustomize Documentation](https://kubectl.docs.kubernetes.io/references/kustomize/)
- [Kustomize Examples](https://github.com/kubernetes-sigs/kustomize/tree/master/examples)
- [Kustomization File Reference](https://kubectl.docs.kubernetes.io/references/kustomize/kustomization/)

## Helm

[Helm](https://helm.sh/) is the package manager for Kubernetes. It uses charts to define, install, and upgrade applications.

**Key resources:**

- [Helm Documentation](https://helm.sh/docs/)
- [Helm Charts](https://helm.sh/docs/topics/charts/)
- [Artifact Hub](https://artifacthub.io/) – Find and publish Helm charts

## Next Steps

- **[Features](features.md)** – Explore KSail capabilities
- **[Use Cases](use-cases.md)** – Practical workflows and examples
- **[Configuration](configuration/index.md)** – Complete configuration reference
