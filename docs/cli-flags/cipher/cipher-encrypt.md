---
title: "ksail cipher encrypt"
parent: "ksail cipher"
grand_parent: "CLI Flags Reference"
---

# ksail cipher encrypt

```text
Encrypt a file using SOPS (Secrets OPerationS).

SOPS supports multiple key management systems:
  - age recipients
  - PGP fingerprints
  - AWS KMS
  - GCP KMS
  - Azure Key Vault
  - HashiCorp Vault

Example:
  ksail cipher encrypt secrets.yaml

Usage:
  ksail cipher encrypt <file> [flags]

Flags:
  -h, --help   help for encrypt

Global Flags:
      --timing   Show per-activity timing output
```
