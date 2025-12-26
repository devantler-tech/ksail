---
title: "ksail workload create secret"
parent: "ksail workload create"
grand_parent: "ksail workload"
---

# ksail workload create secret

```text
Create a secret with specified type.

 A docker-registry type secret is for accessing a container registry.

 A generic type secret indicate an Opaque secret type.

 A tls type secret holds TLS certificate and its associated key.

Usage:
  ksail workload create secret (docker-registry | generic | tls)
  ksail workload create secret [command]

Available Commands:
  docker-registry Create a secret for use with a Docker registry
  generic         Create a secret from a local file, directory, or literal value
  tls             Create a TLS secret

Flags:
  -h, --help   help for secret

Global Flags:
      --timing   Show per-activity timing output

Use "ksail workload create secret [command] --help" for more information about a command.
```
