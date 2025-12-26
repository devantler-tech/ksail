---
title: "ksail workload validate"
parent: "ksail workload"
grand_parent: "CLI Flags Reference"
---

# ksail workload validate

```text
Validate Kubernetes manifest files and kustomizations using kubeconform.

This command validates individual YAML files and kustomizations in the specified path.
If no path is provided, it validates the current directory.

The validation process:
1. Validates individual YAML files
2. Validates kustomizations by building them with kustomize and validating the output

By default, Kubernetes Secrets are skipped to avoid validation failures due to SOPS fields.

Usage:
  ksail workload validate [PATH] [flags]

Flags:
  -h, --help                     help for validate
      --ignore-missing-schemas   Ignore resources with missing schemas (default true)
      --skip-secrets             Skip validation of Kubernetes Secrets (default true)
      --strict                   Enable strict validation mode (default true)
      --verbose                  Enable verbose output

Global Flags:
      --timing   Show per-activity timing output
```
