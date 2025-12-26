---
title: "ksail cluster start"
parent: "ksail cluster"
grand_parent: "CLI Flags Reference"
---

# ksail cluster start

```text
Start a previously stopped cluster.

Usage:
  ksail cluster start [flags]

Flags:
  -c, --context string                 Kubernetes context of cluster
  -d, --distribution Distribution      Kubernetes distribution to use (default Kind)
      --distribution-config string     Configuration file for the distribution
      --flux-interval duration         Flux reconciliation interval (e.g. 1m, 30s) (default 1m0s)
  -g, --gitops-engine GitOpsEngine     GitOps engine to use (None disables GitOps, Flux installs Flux controllers, ArgoCD installs Argo CD) (default None)
  -h, --help                           help for start
  -k, --kubeconfig string              Path to kubeconfig file (default "~/.kube/config")
      --local-registry LocalRegistry   Local registry behavior (Enabled provisions a registry; Disabled skips provisioning. Defaults to Enabled when a GitOps engine is configured) (default Disabled)
      --local-registry-port int32      Host port to expose the local OCI registry on (default 5111)

Global Flags:
      --timing   Show per-activity timing output
```
