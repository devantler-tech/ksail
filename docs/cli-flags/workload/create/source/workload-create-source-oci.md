---
title: "ksail workload create source oci"
parent: "ksail workload create source"
grand_parent: "ksail workload create"
---

# ksail workload create source oci

```text
Create or update an OCIRepository source using Flux APIs

Usage:
  ksail workload create source oci [name] [flags]

Examples:
  # Create a source for an OCI artifact
  ksail workload create source oci podinfo \
    --url=oci://ghcr.io/stefanprodan/manifests/podinfo \
    --tag=6.6.2 \
    --namespace=flux-system

  # Create a source with semver range
  ksail workload create source oci podinfo \
    --url=oci://ghcr.io/stefanprodan/manifests/podinfo \
    --tag-semver=">=6.6.0 <7.0.0"

Flags:
      --digest string       OCI artifact digest
      --export              export in YAML format to stdout
  -h, --help                help for oci
      --insecure            allow insecure connections
      --interval duration   source sync interval (default 1m0s)
      --provider string     OCI provider (default "generic")
      --secret-ref string   the name of an existing secret containing credentials
      --tag string          OCI artifact tag
      --tag-semver string   OCI artifact tag semver range
      --url string          OCI repository URL

Global Flags:
      --timing   Show per-activity timing output
```
