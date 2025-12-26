---
title: "cluster init"
parent: "cluster"
grand_parent: "CLI Flags Reference"
---

# cluster init

```text
Initialize a new project in the specified directory (or current directory if none specified).

Usage:
  ksail cluster init [flags]

Flags:
      --cert-manager CertManager       Cert-Manager configuration (Enabled: install, Disabled: skip) (default Disabled)
      --cni CNI                        Container Network Interface (CNI) to use (default Default)
  -c, --context string                 Kubernetes context of cluster
      --control-planes int32           Number of control-planes for TalosInDocker cluster (default 1)
      --csi CSI                        Container Storage Interface (CSI) to use (default Default)
  -d, --distribution Distribution      Kubernetes distribution to use (default Kind)
      --distribution-config string     Configuration file for the distribution
      --flux-interval duration         Flux reconciliation interval (e.g. 1m, 30s) (default 1m0s)
  -f, --force                          Overwrite existing files
  -g, --gitops-engine GitOpsEngine     GitOps engine to use (None disables GitOps, Flux installs Flux controllers, ArgoCD installs Argo CD) (default None)
  -h, --help                           help for init
  -k, --kubeconfig string              Path to kubeconfig file (default "~/.kube/config")
      --local-registry LocalRegistry   Local registry behavior (Enabled provisions a registry; Disabled skips provisioning. Defaults to Enabled when a GitOps engine is configured) (default Disabled)
      --local-registry-port int32      Host port to expose the local OCI registry on (default 5111)
      --metrics-server MetricsServer   Metrics Server configuration (Enabled: install, Disabled: uninstall) (default Enabled)
      --mirror-registry strings        Configure mirror registries with format 'host=upstream' (e.g., docker.io=https://registry-1.docker.io).
  -o, --output string                  Output directory for the project
  -s, --source-directory string        Directory containing workloads to deploy (default "k8s")
      --workers int32                  Number of workers for TalosInDocker cluster

Global Flags:
      --timing   Show per-activity timing output
```
