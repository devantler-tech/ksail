---
title: "ksail cipher"
parent: "CLI Flags Reference"
---

# ksail cipher

```text
Cipher command provides access to SOPS (Secrets OPerationS) functionality
for encrypting and decrypting files.

SOPS supports multiple key management systems:
  - age recipients
  - PGP fingerprints
  - AWS KMS
  - GCP KMS
  - Azure Key Vault
  - HashiCorp Vault

Usage:
  ksail cipher [command]

Available Commands:
  decrypt     Decrypt a file with SOPS
  edit        Edit an encrypted file with SOPS
  encrypt     Encrypt a file with SOPS
  import      Import an age key to the system's SOPS key location

Flags:
  -h, --help   help for cipher

Global Flags:
      --timing   Show per-activity timing output

Use "ksail cipher [command] --help" for more information about a command.
```
