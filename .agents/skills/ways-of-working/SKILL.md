---
name: ways-of-working
description: >-
  Codifies devantler-tech engineering practices: agent-first development
  workflow, TDD, CI/CD pipelines, GitHub Flow, code quality gates,
  and Kubernetes workflows with ksail. Use when filing issues, planning
  work, setting up projects, configuring CI/CD, writing tests, debugging,
  or making architectural decisions.
license: Apache-2.0
---

# Ways of Working

## Development Workflow

Development is agent-first — every change starts as a structured issue and flows through issue → plan → implement → test → review:

1. **Create an issue** using the [devantler-tech/.github issue templates](https://github.com/devantler-tech/.github/tree/main/.github/ISSUE_TEMPLATE):
   - **Feature** — user story with acceptance criteria.
   - **Bug** — expected vs actual behavior with reproduction steps.
   - **Chore** — user story with acceptance criteria for general tasks.
   - **Kata** — Improvement Kata for continuous improvement (problem → definition of awesome → target condition → actions).
2. **Plan** — create a structured implementation plan from the issue before writing code.
3. **Implement** — execute the plan following the practices in this skill (TDD, quality gates, GitHub Flow, CI/CD).
4. **Manual test** — validate behavior hands-on before merging. Focus on UX: output must be well-presented and every piece of feedback (errors, warnings, prompts) must be actionable.
5. **Code review** (optional) — CI's strict linting, scanning, and quality gates are the primary feedback loop, so manual review is not always required.

## Testing

Follow the [test-driven-development](https://github.com/obra/superpowers/tree/main/skills/test-driven-development) skill rigorously — write the test first, watch it fail, write minimal code to pass.

### Application / Library Repositories

| Type | Tooling | When to run |
|------|---------|-------------|
| **Unit tests** | SDK test frameworks (e.g. `go test`, `xunit`, `jest`) | CI on every PR |
| **Integration tests** | SDK test frameworks | CI on every PR |
| **E2E / system tests** | GitHub Actions workflows that exercise the app/binary/service as an end user would | CI on every PR; move to `merge_group` if the suite becomes too heavy |
| **Benchmark tests** | SDK benchmark frameworks (e.g. `BenchmarkDotNet`, `go test -bench`) | CI on every PR |

### Platform / Kubernetes Repositories

For platform engineering work, [ksail](https://github.com/devantler-tech/ksail) provides the build → run → publish loop and [Testkube](https://testkube.io/) runs the actual test suites inside the cluster:

| Type | App-repo equivalent | Tooling | What it does |
|------|---------------------|---------|--------------|
| **Linting / scanning** | Unit tests | Standard linters & scanners (e.g. kubeconform, kube-linter, Trivy) | Validate manifests statically — no cluster needed |
| **Integration tests** | Integration tests | Ephemeral local cluster (`ksail cluster create`) + Testkube | Spin up a Docker cluster, deploy workloads, and run Testkube test suites against them |
| **E2E / system tests** | E2E / system tests | Ephemeral or real cluster + Testkube | Deploy to a full environment (local or remote) and run end-to-end Testkube test suites |

Platform lifecycle commands:

| Command | Purpose |
|---------|---------|
| `ksail cluster init` | Scaffold a new cluster configuration |
| `ksail cluster create` | Spin up the cluster locally (Docker) or on real infrastructure and verify workloads reconcile |
| `ksail workload push` | Push Kubernetes manifests as OCI artifacts to a container registry |

## Code Quality Gates (CI)

Every pull request **must** pass:

- **Strict linting** — language-specific linters with zero-tolerance for warnings.
- **Strict security scanning** — e.g. CodeQL, Trivy, zizmor.
- **Strict code quality scanning** — e.g. SonarQube, MegaLinter.
- **No code coverage regression** — coverage must not decrease compared to the base branch.
- **No benchmark regression** — benchmark results must not regress compared to the base branch.

## Problem Solving

- **Fix at the root cause** — never patch symptoms. Trace every bug or failure to its origin and fix it there.
- **Workarounds are always temporary** — if a workaround is unavoidable, mark it clearly (e.g. `// WORKAROUND:` comment with a linked issue) and schedule its removal.
- **Upstream first** — when a fix belongs in a dependency or upstream project, contribute it there. Only carry a local patch until the upstream change is released.

## Libraries

Prefer popular, well-maintained third-party libraries over writing custom implementations. Only roll your own when no suitable library exists or when the dependency would be disproportionately heavy.

## Refactoring & Design

Follow <https://refactoring.guru> guidelines for refactoring techniques and design patterns.

## Branching & Flow

Use **GitHub Flow**:

1. Create a feature branch from `main`.
2. Open a pull request.
3. Pass all CI checks.
4. Merge via merge queue (`merge_group`) when the repository is configured for it; otherwise merge the pull request normally.

## CI/CD Pipeline

Each repository has two core workflow files — `ci.yaml` and `cd.yaml` — plus a thin `release.yaml` that calls a reusable workflow (see [Releases](#releases)). Reusable logic is extracted into local GitHub Actions under `.github/actions/`:

- **Composite actions** — for simple, self-contained steps.
- **TypeScript actions** — for anything complex, because they are locally testable and avoid hard-to-read bash embedded in YAML.

### Application / Library Repositories

| Event | What runs |
|-------|-----------|
| `pull_request` | All tests (unit, integration, E2E, benchmarks), linting, security & quality scanning, coverage & benchmark regression checks. Move E2E to `merge_group` only if the suite becomes too heavy. |
| `merge_group` | CD to **dev** environment. |
| `push` to `main` | **Publish** artifacts (packages, container images, etc.). |
| Semver tag (`vX.X.X`) | CD to **prod** environment. |

### Platform / Kubernetes Repositories

| Event | What runs |
|-------|-----------|
| `pull_request` | Linting & scanning (kubeconform, kube-linter, Trivy, etc.) plus integration tests on an ephemeral local cluster (`ksail cluster create` + Testkube). Move integration tests to `merge_group` if the suite becomes too heavy. |
| `merge_group` | CD to **dev** — deploy to staging infrastructure (e.g. Hetzner). |
| Semver tag (`vX.X.X`) | CD to **prod** — `ksail cluster update`, `ksail workload push`, `ksail workload reconcile`. |

### Releases

Releases are automated via the [`devantler-tech/reusable-workflows/.github/workflows/create-release.yaml`](https://github.com/devantler-tech/reusable-workflows/blob/main/.github/workflows/create-release.yaml) reusable workflow. It runs [semantic-release](https://semantic-release.gitbook.io/) on `push` to `main`, calculates the next semver from [conventional commit](https://www.conventionalcommits.org/) history, and creates the tag + GitHub release — which in turn triggers the CD pipeline above.

For application publishing, always use upstream releasers — e.g. [GoReleaser](https://goreleaser.com/) for Go or [dotnet-releaser](https://github.com/xoofx/dotnet-releaser) for .NET — rather than hand-rolling publish scripts.

## Data-Driven Improvement

- Use **benchmarking data** to guide performance improvements — don't optimise without numbers.
- Use **code coverage data** to guide where to add tests — target uncovered paths, not a vanity percentage.

## Reference Implementations

- [devantler-tech/ksail](https://github.com/devantler-tech/ksail) — Go CLI built with TDD, full CI/CD pipeline, benchmarks, and E2E system tests in GitHub Actions.
- [devantler-tech/platform](https://github.com/devantler-tech/platform) — Kubernetes platform using ksail for init → create → push OCI workflow, with progressive CI (Docker → Hetzner staging → Hetzner prod).
