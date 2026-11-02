---
description: 'Codifies devantler-tech engineering practices: agent-first development workflow, TDD, CI/CD pipelines, GitHub Flow, code quality gates, and Kubernetes workflows with ksail. Use when filing issues, planning work, setting up projects, configuring CI/CD, writing tests, debugging, or making architectural decisions.'
license: Apache-2.0
metadata:
    github-path: plugins/engineering-practices/skills/ways-of-working
    github-ref: refs/tags/v2.1.0
    github-repo: https://github.com/devantler-tech/agent-plugins
    github-tree-sha: d079ead0a238b051a081616f3bcb0c0b557f518d
    internal: true
name: ways-of-working
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
| **Linting** | Linters and linting runners (e.g. `golangci-lint`, `eslint`, `MegaLinter`) | CI on every PR |
| **Scanning** | Security & quality scanners (e.g. CodeQL, Trivy, SonarQube) | CI on every PR |
| **Unit tests** | SDK test frameworks (e.g. `go test`, `xunit`, `jest`) | CI on every PR |
| **Integration tests** | SDK test frameworks | CI on every PR |
| **E2E / system tests** | GitHub Actions workflows that exercise the app/binary/service as an end user would | CI on every PR; move to `merge_group` if the suite becomes too heavy |
| **Benchmark tests** | SDK benchmark frameworks (e.g. `BenchmarkDotNet`, `go test -bench`) | CI on every PR |

### Platform / Kubernetes Repositories

For platform engineering work, [ksail](https://github.com/devantler-tech/ksail) provides the build → run → publish loop and [Testkube](https://testkube.io/) runs the actual test suites inside the cluster:

| Type | Tooling | What it does |
|------|---------|--------------|
| **Linting** | Manifest linters (e.g. kubeconform, kube-linter) | Validate manifests statically — no cluster needed |
| **Scanning** | Security scanners (e.g. Trivy) | Scan manifests and images for vulnerabilities — no cluster needed |
| **Integration tests** | Ephemeral local cluster (`ksail cluster create`) + Testkube | Spin up a Docker cluster, deploy workloads, and run Testkube test suites against them |
| **E2E / system tests** | Ephemeral or real cluster + Testkube | Deploy to a full environment (local or remote) and run end-to-end Testkube test suites |

Platform lifecycle commands:

| Command | Purpose |
|---------|---------|
| `ksail cluster init` | Scaffold a new cluster configuration |
| `ksail cluster create` | Spin up the cluster locally (Docker) or on real infrastructure and verify workloads reconcile |
| `ksail workload push` | Push Kubernetes manifests as OCI artifacts to a container registry |

## Code Quality Gates (CI)

Every pull request **must** pass all linting and scanning checks defined in the [Testing](#testing) section. For application / library repositories, additionally:

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

Keep ordinary workflow entrypoints in the repository that owns their outcome, normally `ci.yaml`, `cd.yaml` and, where needed, `release.yaml`. Choose ownership by the actual production callers:

- **Organization-required workflows:** cross-product PR checks such as shared scanning and linting. A guard used by one product belongs in that product, enforced through its repository ruleset and native required checks.
- **Reusable workflows:** job-level logic called by at least two genuine production jobs. Two real entrypoints in one repository can qualify; matrix cells, test fixtures and invented callers do not establish reuse.
- **Ordinary workflows:** callsites for reusable jobs, or implementations used in one place. Keep a single-use implementation in its calling repository rather than exporting it from a shared automation repository.

Split manageable step-level units into local GitHub Actions under `.github/actions/` when useful:

- **Composite actions** — for simple, self-contained steps.
- **TypeScript actions** — for anything complex, because they are locally testable and avoid hard-to-read bash embedded in YAML.

### Application / Library Repositories

| Event | What runs |
|-------|-----------|
| `pull_request` | Linting, scanning, unit tests, integration tests, E2E tests, benchmarks, coverage & benchmark regression checks. Move E2E to `merge_group` only if the suite becomes too heavy. |
| `merge_group` | CD to **dev** environment. |
| `push` to `main` | **Publish** artifacts (packages, container images, etc.). |
| Semver tag (`vX.X.X`) | CD to **prod** environment. |

### Platform / Kubernetes Repositories

| Event | What runs |
|-------|-----------|
| `pull_request` | Linting, scanning, integration tests on an ephemeral local cluster (`ksail cluster create` + Testkube). Move integration tests to `merge_group` if the suite becomes too heavy. |
| `merge_group` | CD to **dev** — deploy to staging infrastructure (e.g. Hetzner). |
| Semver tag (`vX.X.X`) | CD to **prod** — `ksail cluster update`, `ksail workload push`, `ksail workload reconcile`. |

### Releases

Repositories using the shared release job invoke [`devantler-tech/.github/.github/workflows/create-release.yaml`](https://github.com/devantler-tech/.github/blob/main/.github/workflows/create-release.yaml) from a repository-owned caller that handles `push` to `main`. The shared workflow runs [semantic-release](https://semantic-release.gitbook.io/), calculates the next semver from [conventional commit](https://www.conventionalcommits.org/) history, and creates the tag and GitHub release, which can trigger the repository's CD pipeline. Preserve a repository-owned release implementation when its requirements differ; a release entrypoint does not by itself justify a new reusable workflow.

For application publishing, always use upstream releasers — e.g. [GoReleaser](https://goreleaser.com/) for Go or [dotnet-releaser](https://github.com/xoofx/dotnet-releaser) for .NET — rather than hand-rolling publish scripts.

## Data-Driven Improvement

- Use **benchmarking data** to guide performance improvements — don't optimise without numbers.
- Use **code coverage data** to guide where to add tests — target uncovered paths, not a vanity percentage.

## Reference Implementations

- [devantler-tech/ksail](https://github.com/devantler-tech/ksail) — Go CLI built with TDD, full CI/CD pipeline, benchmarks, and E2E system tests in GitHub Actions.
- [devantler-tech/platform](https://github.com/devantler-tech/platform) — Kubernetes platform using ksail for init → create → push OCI workflow, with progressive CI (Docker → Hetzner staging → Hetzner prod).
