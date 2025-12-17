# Secret Manager

KSail-Go integrates [SOPS](https://github.com/getsops/sops) for encrypting manifests. Enable it with `ksail cluster init --secret-manager SOPS` or keep it disabled with `--secret-manager None`. The declarative equivalent lives under `spec.security.secretManager`.

> **Tip:** Enable the secret manager during initialization so KSail-Go can generate an Age keypair and bootstrap the `.sops.yaml` configuration automatically.

## None

No secret management resources are provisioned. Choose this when you rely on plaintext manifests or have a separate secret workflow.

## SOPS

Selecting `SOPS` generates `.sops.yaml`, caches your Age private key locally, and installs the required controller support for Flux-based deployments. Use the `ksail cipher` commands to encrypt, edit, and decrypt secret files.

> **Important:** Flux is currently the only deployment tool with built-in decryption support in KSail-Go. For kubectl workflows you can still encrypt files, but you must handle decryption manually (for example with [`ksops`](https://github.com/viaduct-ai/kustomize-sops)).
