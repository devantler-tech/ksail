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
ksail cipher import <key>      # Import age private key to default SOPS location
```

### Importing Age Keys

The `ksail cipher import` command simplifies age key management by automatically:

- Deriving the public key from your private key
- Installing the key to the platform-specific SOPS location:
  - **Linux**: `$XDG_CONFIG_HOME/sops/age/keys.txt` or `$HOME/.config/sops/age/keys.txt`
  - **macOS**: `$XDG_CONFIG_HOME/sops/age/keys.txt` or `$HOME/Library/Application Support/sops/age/keys.txt`
  - **Windows**: `%AppData%\sops\age\keys.txt`
- Adding metadata (creation timestamp and public key)
- Preserving any existing keys in the file

**Example:**

```bash
# Import an age private key
ksail cipher import AGE-SECRET-KEY-1ZYXWVUTSRQPONMLKJIHGFEDCBA...

# The command automatically derives the public key and creates:
# created: 2025-12-19T20:15:30Z
# public key: age1abc123...
# AGE-SECRET-KEY-1ZYXWVUTSRQPONMLKJIHGFEDCBA...
```

## Key Management Systems

SOPS supports multiple key management systems:

- age recipients (recommended for local development)
- PGP fingerprints
- AWS KMS
- GCP KMS
- Azure Key Vault
- HashiCorp Vault

## Configuration

Configure SOPS using a `.sops.yaml` file in your project directory. See the [SOPS documentation](https://github.com/getsops/sops#usage) for configuration details.

> **Note:** Full GitOps integration with automatic decryption is planned for future releases when Flux support is added.
