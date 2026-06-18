# Free Disk Space

A composite action that reclaims disk on `ubuntu-latest` runners before disk-heavy CI jobs run.

## Purpose

Disk-heavy jobs can exhaust the stock runner's root filesystem (`/`). When `/` fills up, the runner
process itself crashes mid-job with `System.IO.IOException: No space left on device` (while writing
its own `_diag` logs) — a **transient, non-deterministic** failure that depends on how much free
space the runner happened to start with. This action removes large pre-installed hosted-tooling
directories that KSail's CI never uses, reclaiming ~30 GB of headroom up front.

It centralizes the disk-reclamation step that was previously duplicated inline across the `generate`
and `system-test-docker` jobs, so every disk-heavy job inherits the same fix (and future jobs only
need to add a single `uses:` line).

## What It Does

1. Records free space on `/`.
2. `sudo rm -rf` of the largest unused directories (Android SDK, .NET, GHC, Boost, Swift, CodeQL,
   PyPy, Ruby, Python, Chromium, PowerShell, Julia, AWS CLI, Gradle, Azure CLI, miniconda). Each
   removal is best-effort — missing paths are not an error.
3. Optionally runs `docker system prune -af --volumes` (see `docker-prune`).
4. Logs the before → after free space and the amount gained.

Targeted `rm -rf` is deliberately used instead of the slower `apt-get remove` path (e.g.
`endersonmenezes/free-disk-space`), which previously consumed 3–9 min per job.

## Inputs

| Name           | Required | Default | Description                                                                                      |
|----------------|----------|---------|--------------------------------------------------------------------------------------------------|
| `docker-prune` | No       | `false` | Also run `docker system prune -af --volumes`. Set to `true` for jobs that use the Docker daemon. |

## Usage

```yaml
- name: 📄 Checkout
  uses: actions/checkout@v6
  with:
    persist-credentials: false

# Reclaim disk before the heavy work (build / image mirroring / cluster spin-up).
- name: 🧹 Free disk space
  uses: ./.github/actions/free-disk-space

# Docker-based jobs that may have images/volumes to reclaim:
- name: 🧹 Free disk space
  uses: ./.github/actions/free-disk-space
  with:
    docker-prune: "true"
```

## Where It Is Used

Disk-heavy jobs in [`ci.yaml`](../../workflows/ci.yaml): `build-artifact`, `generate`,
`operator-chart-e2e`, `warm-helm-cache`, `warm-mirror-cache`, and `system-test-docker`.
