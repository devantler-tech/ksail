# E2E Testing in CI/CD

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
jobs:
  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Create cluster
        run: ksail cluster create
      - name: Deploy workloads
        run: ksail workload apply -k k8s/
      - name: Run tests
        run: go test ./tests/e2e/...
      - name: Cleanup
        if: always()
        run: ksail cluster delete
```

## GitHub Actions example

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
          go-version-file: go.mod
      - name: Install ksail
        run: go install ./cmd/ksail
      - name: Create cluster
        run: |
          ksail cluster create --wait --timeout 10m
      - name: Deploy workloads
        run: |
          ksail workload reconcile -f k8s/overlays/ci
          ksail workload wait --for=condition=Available deployment/my-app --timeout=180s
      - name: Run tests
        run: go test ./tests/e2e/... -count=1
      - name: Upload logs on failure
        if: failure()
        run: |
          ksail workload logs deployment/my-app --since 5m > logs.txt
          kubectl get events --all-namespaces > events.txt
      - name: Destroy cluster
        if: always()
        run: ksail cluster delete
```

For hosted runners without Docker cache, consider pre-building images in a preceding job and pushing them to a registry that the KSail-Go cluster can pull from. You can also run the workflow on self-hosted runners equipped with faster storage to keep end-to-end cycles under 10 minutes.

## Hardening recommendations

- Store test-only secrets with SOPS and decrypt them during the pipeline with `ksail cipher decrypt` so they never appear in plain text.
- Use `ksail cluster create --registry local --mirror-registries true` if your registries require mirroring or authentication on private runners.
- Add a nightly job that exercises the same pipeline against the default branch to catch drift in Kubernetes versions or base images.
- Track time-to-ready metrics by wrapping `ksail cluster create` with timestamps and pushing results to your observability stack.
