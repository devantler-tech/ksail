---
title: "Secret Manager"
parent: Core Concepts
grand_parent: Overview
nav_order: 11
---

# Secret Manager

KSail integrates [SOPS](https://github.com/getsops/sops) for encrypting manifests through the `ksail cipher` commands.

## Using SOPS with KSail

The `ksail cipher` commands provide access to SOPS functionality:

```bash
ksail cipher encrypt <file>    # Encrypt a file with SOPS
ksail cipher decrypt <file>    # Decrypt a file with SOPS
ksail cipher edit <file>       # Edit an encrypted file with SOPS
```

SOPS supports multiple key management systems:
- age recipients
- PGP fingerprints
- AWS KMS
- GCP KMS
- Azure Key Vault
- HashiCorp Vault

## Configuration

Configure SOPS using a `.sops.yaml` file in your project directory. See the [SOPS documentation](https://github.com/getsops/sops#usage) for configuration details.

> **Note:** Full GitOps integration with automatic decryption is planned for future releases when Flux support is added.
