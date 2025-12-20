---
title: "Use Cases"
nav_order: 4
---

# Use Cases

KSail focuses on fast, reproducible feedback loops for local Kubernetes development. The CLI targets developer desktops, CI pipelines, and learning environments where rapid provisioning is important.

All scenarios use the same configuration primitives documented in [Configuration](configuration.md). Start with `ksail cluster init` to scaffold a project, then follow the workflows below.

## Learning Kubernetes

KSail simplifies Kubernetes experimentation by providing a consistent interface over Kind and K3d. Focus on learning Kubernetes concepts without complex cluster setup.

### Quick Learning Session

```bash
# 1. Create a playground
ksail cluster init --distribution Kind --cni Cilium

# 2. Start the cluster
ksail cluster create

# 3. Try workloads
ksail workload gen deployment echo --image=hashicorp/http-echo:0.2.3 --port 5678
ksail workload apply -f echo.yaml
ksail workload wait --for=condition=Available deployment/echo --timeout=120s

# 4. Inspect with k9s
ksail cluster connect

# 5. Clean up and repeat
ksail cluster delete
```

### Learning Tips

- Switch between `--distribution Kind` and `--distribution K3d` to compare runtimes
- Try `--cni Cilium` to explore advanced networking features
- Use `--cert-manager Enabled` to learn about TLS certificate management
- Track configuration in Git to understand how changes affect cluster behavior

Reference `kubectl explain` and the [Kubernetes documentation](https://kubernetes.io/docs/) for deeper understanding.

## Local Development

KSail helps you run Kubernetes manifests locally using your container engine. The CLI provides a consistent workflow that matches your deployment configuration.

### Development Workflow

```bash
# 1. Scaffold project (commit these files for team consistency)
ksail cluster init --distribution Kind --cni Cilium --metrics-server Enabled

# 2. Create cluster
ksail cluster create

# 3. Apply workloads
ksail workload apply -k k8s/
ksail workload get pods

# 4. Debug and inspect
ksail workload logs deployment/my-app --tail 200
ksail workload exec deployment/my-app -- sh
ksail cluster connect  # Opens k9s

# 5. Clean up
ksail cluster delete
```

### Local Registry Workflow

For faster dev loops with local images:

```bash
# Initialize with local registry
ksail cluster init --local-registry Enabled --local-registry-port 5111

# Create cluster
ksail cluster create

# Build and push local images
docker build -t localhost:5111/my-app:dev .
docker push localhost:5111/my-app:dev

# Update manifests to reference localhost:5111/my-app:dev
ksail workload apply -k k8s/
```

### Development Tips

- Use `--cert-manager Enabled` if you need TLS certificates
- Configure `--mirror-registry` to cache upstream images and avoid rate limits
- Use `ksail workload gen` to create sample resource manifests
- Test manifests locally before committing to version control
- Commit `ksail.yaml` so your team inherits the same setup automatically

## E2E Testing in CI/CD

KSail enables CI/CD pipelines to create disposable Kubernetes clusters for integration testing using the same declarative configuration that developers use locally.

### Pipeline Workflow

```bash
# 1. Initialize (commit config so CI only needs to run create)
ksail cluster init --distribution Kind --metrics-server Enabled

# 2. Create cluster in CI
ksail cluster create
ksail cluster info

# 3. Deploy and test
ksail workload apply -k k8s/
ksail workload wait --for=condition=Available deployment/my-app --timeout=180s
go test ./tests/e2e/... -count=1

# 4. Collect diagnostics on failure
ksail workload logs deployment/my-app --since 5m
ksail workload get events -A

# 5. Clean up
ksail cluster delete
```

### Example GitHub Actions Workflow

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
      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Install ksail
        run: go install github.com/devantler-tech/ksail@latest
      - name: Create cluster
        run: ksail cluster create
      - name: Deploy workloads
        run: |
          ksail workload apply -k k8s/
          ksail workload wait --for=condition=Available deployment/my-app --timeout=180s
      - name: Run tests
        run: go test ./tests/e2e/... -count=1
      - name: Upload logs on failure
        if: failure()
        run: |
          ksail workload logs deployment/my-app --since 5m > logs.txt
          ksail workload get events -A > events.txt
      - name: Destroy cluster
        if: always()
        run: ksail cluster delete
```

### CI/CD Tips

- Use Kind for fastest cluster creation in CI environments
- Set reasonable timeouts for cluster creation and workload readiness
- Cache Docker images to speed up subsequent runs
- Use `--mirror-registry` flags to reduce external registry dependencies
- Collect cluster state before deletion for debugging failed runs

### Security Recommendations

- Store secrets with SOPS and decrypt during pipeline with `ksail cipher decrypt`
- Keep SOPS/age private keys out of repository and images
- Provide decryption keys via CI secret store (e.g., GitHub Actions secrets)
- Use `--mirror-registry` for registries requiring mirroring
- Add nightly jobs against default branch to catch drift
- Track cluster creation times to identify performance regressions
