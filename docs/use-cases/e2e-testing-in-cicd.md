# E2E Testing in CI/CD

KSail-Go enables pull-request pipelines to stand up disposable Kubernetes clusters, run integration suites, and tear everything down before merging. Because the CLI encapsulates cluster provisioning, you can reuse the same declarative configuration that developers run locally. Review the [CLI options](../configuration/cli-options.md) if you need to override settings dynamically within your pipeline.

## Pipeline essentials

1. **Check out the repository and install dependencies**

   ```bash
   ksail cluster init --distribution Kind --source-directory ci/k8s --metrics-server true --gateway-controller default
   ```

   Commit the generated config so your CI job only needs to run `ksail cluster create` without additional flags.
2. **Create the cluster and wait for readiness**

   ```bash
   ksail cluster create --wait --timeout 10m
   ksail cluster status --output table
   ```

   The extended timeout accounts for container pulls on cold runners. `status` verifies controllers are healthy before tests begin.
3. **Deploy workloads and run tests**

   ```bash
   ksail workload reconcile -f k8s/overlays/ci
   ksail workload wait --for=condition=Available deployment/my-app --timeout=180s
   go test ./tests/e2e/... -count=1
   ```

   Swap the test command for your framework of choice (JUnit, pytest, etc.) while leaving KSail-Go responsible for cluster state.
4. **Collect diagnostics on failure**

   ```bash
   ksail workload logs deployment/my-app --since 5m
   kubectl get events --all-namespaces
   ```

   Persist the output as pipeline artifacts so engineers can inspect issues without rerunning the job immediately.
5. **Destroy the cluster**

   ```bash
   ksail cluster delete
   ```

   Always clean up to keep shared runners fast and avoid leaking Docker resources.

## GitHub Actions example

```yaml
name: e2e
on:
  pull_request:
    paths:
      - 'k8s/**'
      - 'docs/**'
      - 'src/**'

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Install ksail-go
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
