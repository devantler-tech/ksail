---
title: E2E Testing in CI/CD
parent: Use Cases
nav_order: 3
---

KSail enables CI/CD pipelines to create disposable Kubernetes clusters for integration testing. Use the same declarative configuration that developers use locally.

## Pipeline workflow

1. **Initialize configuration**

   ```bash
   ksail cluster init --distribution Kind --source-directory k8s --metrics-server Enabled
   ```

   Commit the config so CI only needs to run `ksail cluster create`.

2. **Create the cluster**

   ```bash
   ksail cluster create
   ksail cluster info
   ```

   Wait for the cluster to be ready before deploying workloads.

3. **Deploy and test**

   ```bash
   ksail workload apply -k k8s/
   ksail workload wait --for=condition=Available deployment/my-app --timeout=180s
   
   # Run your tests
   go test ./tests/e2e/... -count=1
   ```

   Replace the test command with your framework (JUnit, pytest, etc.).

4. **Collect diagnostics on failure**

   ```bash
   ksail workload logs deployment/my-app --since 5m
   ksail workload get events -A
   ```

   Save output as pipeline artifacts for debugging.

5. **Clean up**

   ```bash
   ksail cluster delete
   ```

   Always clean up to avoid resource leaks.

## CI Tips

- Use Kind for fastest cluster creation in CI
- Set reasonable timeouts for cluster creation and workload readiness
- Cache Docker images to speed up subsequent runs
- Use `--mirror-registry` flags to reduce external registry dependencies
- Collect cluster state before deletion for debugging failed runs

## Example GitHub Actions workflow

```yaml
name: e2e
on:
  pull_request:
    paths:
      - "k8s/**"
      - "docs/**"
      - "src/**"

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          # Assumes a go.mod at the repository root; otherwise, use "go-version" with an explicit Go version.
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

For hosted runners without Docker cache, consider pre-building images in a preceding job and pushing them to a registry that the KSail cluster can pull from. You can also run the workflow on self-hosted runners equipped with faster storage to keep end-to-end cycles under 10 minutes.

## Hardening recommendations

- Store test-only secrets with SOPS and decrypt them during the pipeline with `ksail cipher decrypt` so they never appear in plain text:
  - Keep SOPS/age private keys **out of the repository** and container images.
  - Provide decryption keys to the pipeline via your CI’s secret store (for example, GitHub Actions secrets such as `AGE_PRIVATE_KEY` or `SOPS_*` environment variables) or a supported KMS backend.
  - Configure the workflow to export these secrets as environment variables only for the steps that run `ksail cipher decrypt`.
- Use `--mirror-registry` flags if your registries require mirroring or you want to cache upstream images
- Add a nightly job that exercises the same pipeline against the default branch to catch drift in Kubernetes versions or base images
- Track cluster creation times to identify performance regressions
