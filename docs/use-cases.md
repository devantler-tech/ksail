---
title: "Use Cases"
nav_order: 6
---

# Use Cases

Fast, reproducible workflows for local Kubernetes development, CI pipelines, and learning environments.

All scenarios use configuration primitives from [Declarative Configuration](configuration/declarative-configuration.md) or [CLI Flags](configuration/cli-flags/root.md). Start with `ksail cluster init`.

## Learning Kubernetes

Experiment with Kubernetes without complex setup.

```bash
# Create playground
ksail cluster init --distribution Vanilla --cni Cilium
ksail cluster create

# Try workloads
ksail workload gen deployment echo --image=hashicorp/http-echo:0.2.3 --port 5678
ksail workload validate -f echo.yaml
ksail workload apply -f echo.yaml
ksail workload wait --for=condition=Available deployment/echo --timeout=120s

# Inspect
ksail cluster connect

# Clean up
ksail cluster delete
```

**Tips:** Switch distributions (`--distribution K3s`), try `--cni Cilium` or `--cert-manager Enabled`, commit config to track changes.

## Local Development

Run Kubernetes manifests locally with consistent workflow. See [Workload Management](features.md#workload-management) for command details.

```bash
# Scaffold project
ksail cluster init --distribution Vanilla --cni Cilium --metrics-server Enabled

# Create cluster
ksail cluster create

# Validate and apply workloads
ksail workload validate k8s/
ksail workload apply -k k8s/
ksail workload get pods

# Debug
ksail workload logs deployment/my-app --tail 200
ksail workload exec deployment/my-app -- sh
ksail cluster connect

# Clean up
ksail cluster delete
```

### Local Registry

For local images (see [Registry Management](features.md#registry-management) for details):

```bash
ksail cluster init --local-registry Enabled --local-registry-port 5050
ksail cluster create

# Build and push
docker build -t localhost:5050/my-app:dev .
docker push localhost:5050/my-app:dev

# Reference in manifests: localhost:5050/my-app:dev
ksail workload apply -k k8s/
```

**Tips:** Use `--csi LocalPathStorage` for persistent volumes, `--cert-manager Enabled` for TLS, `--mirror-registry` to avoid rate limits, validate before applying, commit `ksail.yaml` for team consistency.

## E2E Testing in CI/CD

Disposable Kubernetes clusters for integration testing.

```bash
# Initialize (commit config)
ksail cluster init --distribution Vanilla --metrics-server Enabled

# Create cluster in CI
ksail cluster create
ksail cluster info

# Deploy and test
ksail workload validate k8s/
ksail workload apply -k k8s/
ksail workload wait --for=condition=Available deployment/my-app --timeout=180s
go test ./tests/e2e/... -count=1

# Collect diagnostics on failure
ksail workload logs deployment/my-app --since 5m
ksail workload get events -A

# Clean up
ksail cluster delete
```

### GitHub Actions Example

```yaml
name: e2e
on:
  pull_request:
    paths: ["k8s/**", "src/**"]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go install github.com/devantler-tech/ksail@latest
      - run: ksail cluster create
      - run: |
          ksail workload validate k8s/
          ksail workload apply -k k8s/
          ksail workload wait --for=condition=Available deployment/my-app --timeout=180s
      - run: go test ./tests/e2e/... -count=1
      - if: failure()
        run: |
          ksail workload logs deployment/my-app --since 5m > logs.txt
          ksail workload get events -A > events.txt
      - if: always()
        run: ksail cluster delete
```

**Tips:** Use Vanilla for speed, `--csi LocalPathStorage` for PVCs, set reasonable timeouts, cache images, use `--mirror-registry`, collect state before deletion.

## GitOps Development Workflow

Local GitOps workflows with [Flux or ArgoCD](concepts.md#gitops). See [GitOps Workflows](features.md#gitops-workflows) for command reference.

### Flux

```bash
# Initialize
ksail cluster init --gitops-engine Flux --local-registry Enabled
ksail cluster create

# Edit manifests in k8s/

# Push and reconcile
ksail workload push
ksail workload reconcile

# Verify
ksail workload get pods
ksail cluster connect
```

### ArgoCD

```bash
# Initialize
ksail cluster init --gitops-engine ArgoCD --local-registry Enabled
ksail cluster create

# Edit manifests in k8s/

# Push and reconcile
ksail workload push
ksail workload reconcile --timeout 10m
```

**Tips:** Push after every manifest change, `reconcile` waits for completion and triggers both Flux and ArgoCD, Flux auto-detects new artifacts from the OCI registry while ArgoCD relies on `ksail workload reconcile`, use `--timeout` for long deployments, test locally before production, commit `ksail.yaml`.

### Security

Secure your GitOps workflow with [SOPS secret management](features.md#secret-management):

- Encrypt secrets with SOPS: `ksail cipher encrypt`
- Decrypt in pipeline: `ksail cipher decrypt`
- Keep age keys out of repo and images
- Provide keys via CI secrets
- Use `--mirror-registry` as needed
- Add nightly jobs for drift detection
