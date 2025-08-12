---
title: E2E Testing in CI/CD
parent: Use Cases
nav_order: 2
---

# E2E Testing in CI/CD

KSail can also be used for end-to-end (E2E) testing in CI/CD pipelines. As easily as you can create a local Kubernetes cluster, you can also create ephemeral clusters in your CI/CD pipelines. As you have already configured your cluster locally, it is as simple as running `ksail up` in your pipeline to create the cluster. This allows you to validate that project files do not contain errors or typos, that your cluster spins up correctly, and that your workloads reconcile as expected.

If you want to go that extra mile, you can even run validations against the cluster after it has reconciled its workloads. With such an approach, you can validate data flows, health checks, or whatever your heart desires.

Running KSail in CI/CD pipelines is a great way to ensure unintended changes to your Kubernetes are caught in Pull Requests, and that your workloads always stay healthy between deployments. This is super valuable in teams where multiple changes happen in parallel, and where the risk of causing issues server-side is high.

By migrating to a traditional GitHub Flow, where you create a Pull Request for every change, and where you run KSail in your CI/CD pipeline, you can build super stable and reliable clusters while never having to connect to a remote cluster.

## Example of GitHub Workflow

Below is an example of a GitHub workflow that runs KSail in a CI/CD pipeline. It provisions a cluster, and tears it down after the tests have run. The workflow is triggered on every Pull Request.

```yaml
name: GitOps Test

on:
  workflow_call:
    secrets:
      KSAIL_SOPS_KEY:
        required: false
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: 📑 Checkout
        uses: actions/checkout@v4
        with:
          persist-credentials: false
      - name: ⚙️ Setup KSail
        uses: devantler-tech/composite-actions/setup-ksail-action@44620f6c6e9bc2046c7959932fbd104a74d6b1a5 # v1.9.1
      - name: 🔑 Import Age key
        env:
          KSAIL_SOPS_KEY: ${{ secrets.KSAIL_SOPS_KEY }}
        if: ${{ env.KSAIL_SOPS_KEY != '' }}
        run: ksail secrets import "${{ secrets.KSAIL_SOPS_KEY }}"
      - name: 🚀 Provision cluster
        run: ksail up
      - name: 🔥 Teardown cluster
        if: always()
        run: ksail down
```

The above workflow is a great starting point for running KSail in your CI/CD pipeline, and it is publicly available, so you are free to use it yourself:

```yaml
name: Run Devantler's GitOps Test Workflow
  workflow_dispatch:
  push:
    branches:
      - main
  pull_request:

jobs:
  test:
    uses: devantler-tech/reusable-workflows/.github/workflows/ci-gitops-test@main
    secrets:
      KSAIL_SOPS_KEY: ${{ secrets.KSAIL_SOPS_KEY }}
```
