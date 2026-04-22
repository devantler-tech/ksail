# Contributing

This project accepts contributions in the form of [**bug reports**](https://github.com/devantler-tech/ksail/issues/new/choose), [**feature requests**](https://github.com/devantler-tech/ksail/issues/new/choose), and **pull requests** (see below). If you are looking to contribute code, please follow the guidelines outlined in this document.

## Your First Contribution

New to KSail? Welcome! Here's how to get started:

1. **Find an issue** — Look for issues labeled [`good first issue`](https://github.com/devantler-tech/ksail/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22) for beginner-friendly tasks, or [`help wanted`](https://github.com/devantler-tech/ksail/issues?q=is%3Aissue+is%3Aopen+label%3A%22help+wanted%22) for tasks where the maintainers are looking for help.
2. **Fork & clone** — [Fork the repository](https://github.com/devantler-tech/ksail/fork) and clone it locally.
3. **Set up your environment** — Follow the [Prerequisites](#prerequisites) section below.
4. **Make your changes** — Create a branch, implement your fix or feature, and write tests.
5. **Submit a PR** — Open a pull request against `main` with a clear description of what changed and why.

If you have questions, don't hesitate to ask in [GitHub Discussions](https://github.com/devantler-tech/ksail/discussions).

## Getting Started

To get started with contributing to KSail, you'll need to set up your development environment, and understand the codebase, the CI setup and its requirements.

To understand the codebase it is recommended to read the [.github/copilot-instructions.md](.github/copilot-instructions.md) file, which provides an overview of the project structure and key components. You can also use GitHub Copilot to assist you in navigating the codebase and understanding its functionality.

For a deeper dive into KSail's design and internals, refer to:

- [Architecture Guide](https://ksail.devantler.tech/architecture/) — Design principles, component architecture, provider/provisioner model, and state persistence
- [Development Guide](https://ksail.devantler.tech/development/) — Development environment setup, coding standards, testing patterns, and CI/CD workflows

### Code Documentation

For detailed package and API documentation, refer to the Go documentation at [pkg.go.dev/github.com/devantler-tech/ksail/v7](https://pkg.go.dev/github.com/devantler-tech/ksail/v7). This provides comprehensive documentation for all exported packages, types, functions, and methods.

### Prerequisites

**Runtime Requirements:**

- [Docker](https://www.docker.com/get-started/) — The only required external dependency for running KSail

**Development Requirements:**

Before you begin developing, ensure you have the following installed:

- [Go (v1.26.1+)](https://go.dev/doc/install)
- [mockery (v3.5+)](https://vektra.github.io/mockery/v3.5/installation/)
- [golangci-lint](https://golangci-lint.run/docs/welcome/install/)
- [mega-linter](https://github.com/oxsecurity/megalinter/tree/main/mega-linter-runner#installation)
- [Node.js (v24+)](https://nodejs.org/en/download/) — Required for building documentation (matches CI)

### Lint

KSail uses mega-linter with the go flavor, and uses a strict configuration to ensure code quality and consistency. You can run the linter with the following command:

```sh
# working-directory: ./
mega-linter-runner -f go
```

The same configuration is used in CI, so you can expect the same linting behavior in your local environment as in the CI pipeline.

MegaLinter also checks Markdown files. Markdown lint rules are configured in `.markdownlint.json` (some rules are relaxed to accommodate Astro/Starlight front matter and documentation formatting).

### Build

```sh
# working-directory: ./
# Build the ksail binary (development build)
go build -o ksail

# Or: compile all packages (no binary output)
go build ./...

# For optimized builds (strips debug symbols):
go build -ldflags="-s -w" -o ksail-optimized
```

> **Note:** Release builds use `-ldflags="-s -w -X github.com/devantler-tech/ksail/v7/internal/buildmeta.Version=... -X .../buildmeta.Commit=... -X .../buildmeta.Date=..."`, where `-s -w` strips debug symbols and the `-X` flags inject version metadata. The `-s -w` options can significantly reduce binary size (in some cases by ~25–35%; see [#2095](https://github.com/devantler-tech/ksail/pull/2095) for an example benchmark where Darwin/AMD64 binaries went from 302MB → 217MB, ~28%), while the metadata flags themselves may slightly increase size compared to a build that only uses `-s -w`. Actual size varies by OS/arch, Go version, and dependencies. Development builds include debug symbols for a better debugging experience.

### Test

#### Generating mocks

```sh
# working-directory: ./
mockery
```

#### Unit tests

```sh
# working-directory: ./
go test ./...
```

#### Benchmarks

KSail includes Go benchmarks for performance-critical code paths. When making performance-related changes, run benchmarks to validate improvements:

```sh
# working-directory: ./
# Run all benchmarks
go test -bench=. -benchmem ./...

# Run benchmarks for specific package (e.g., resource polling)
go test -bench=. -benchmem -run=^$ ./pkg/k8s/readiness/...

# Compare before/after performance
go test -bench=. -benchmem -run=^$ ./pkg/k8s/readiness/... > before.txt
# (make changes)
go test -bench=. -benchmem -run=^$ ./pkg/k8s/readiness/... > after.txt
benchstat before.txt after.txt
```

PRs that modify Go code are automatically benchmarked against `main` and the comparison is posted as a PR comment. See [docs/BENCHMARK-REGRESSION.md](docs/BENCHMARK-REGRESSION.md) for details on interpreting results.

See package-specific BENCHMARKS.md files (e.g., `pkg/apis/cluster/v1alpha1/BENCHMARKS.md`, `pkg/cli/cmd/cipher/BENCHMARKS.md`, `pkg/cli/cmd/cluster/BENCHMARKS.md`, `pkg/client/argocd/BENCHMARKS.md`, `pkg/client/docker/BENCHMARKS.md`, `pkg/client/flux/BENCHMARKS.md`, `pkg/client/helm/BENCHMARKS.md`, `pkg/client/kubectl/BENCHMARKS.md`, `pkg/client/kustomize/BENCHMARKS.md`, `pkg/fsutil/configmanager/ksail/BENCHMARKS.md`, `pkg/fsutil/marshaller/BENCHMARKS.md`, `pkg/k8s/readiness/BENCHMARKS.md`, `pkg/svc/diff/BENCHMARKS.md`, `pkg/svc/image/BENCHMARKS.md`) for detailed benchmark documentation, baseline results, and performance optimization opportunities.

### Documentation

The project documentation is built using [Astro](https://astro.build/) with the [Starlight](https://starlight.astro.build/) theme and is located in the `docs/` directory.

#### Auto-generated reference docs

Some documentation is generated from source and should **not** be edited manually:

- **CLI flags reference:** `docs/src/content/docs/cli-flags/` (generated by `go generate ./docs/...` from the Cobra command tree)
- **Configuration reference:** `docs/src/content/docs/configuration/declarative-configuration.mdx` (generated by `go generate ./docs/...` from v1alpha1 types)
- **Configuration schema:** `schemas/ksail-config.schema.json` (generated by `go generate ./schemas/...`)

To regenerate locally:

```sh
# working-directory: ./

# Generate reference docs (CLI flags + configuration reference)
go generate ./docs/...

# Generate JSON schema
go generate ./schemas/...
```

#### Building the documentation

```sh
# working-directory: ./docs

# Install dependencies (first time only or when package-lock.json changes)
npm ci

# Build the site
npm run build

# Serve the site locally with live reload (optional)
npm run dev
# Visit http://localhost:4321 to view the site
```

The built site will be available in `docs/dist/`. Note that `docs/dist/` and `docs/node_modules/` are excluded from git via `.gitignore`.

### VSCode Extension

The VSCode extension is located in the `vsce/` directory and provides cluster management capabilities directly in VSCode.

#### Building the extension

```sh
# working-directory: ./vsce

# Install dependencies (first time only or when package-lock.json changes)
npm ci

# Compile TypeScript to JavaScript
npm run compile

# Package as VSIX for distribution
npx @vscode/vsce package --no-dependencies
```

#### Testing locally

1. Open the `vsce` folder in VSCode
2. Press `F5` to launch Extension Development Host
3. Test commands from the Command Palette (`Cmd+Shift+P` / `Ctrl+Shift+P`)

The extension source is organized as follows:

```text
vsce/
├── src/
│   ├── extension.ts          # Entry point, command registration
│   ├── commands/
│   │   ├── index.ts          # Command handlers (command registry)
│   │   └── prompts.ts        # Interactive wizard implementations
│   ├── ksail/
│   │   ├── clusters.ts       # KSail CLI wrapper functions
│   │   ├── binary.ts         # KSail binary discovery and execution
│   │   ├── kubectl.ts        # kubectl wrapper for cluster status queries
│   │   └── index.ts          # Module exports
│   ├── kubernetes/
│   │   ├── cloudProvider.ts              # Cloud Explorer tree provider (KSail clusters in Clouds view)
│   │   ├── clusterExplorerContributor.ts # Annotates KSail contexts in Cluster Explorer
│   │   ├── clusterProvider.ts            # Create Cluster wizard (HTML-based)
│   │   ├── contextNames.ts               # Shared helpers for parsing kubeconfig context names
│   │   └── index.ts                      # Module exports
│   └── mcp/
│       ├── serverProvider.ts  # MCP server definition provider
│       ├── schemaClient.ts    # MCP schema client for KSail
│       └── index.ts           # Module exports
├── dist/                      # Compiled output
└── package.json               # Extension manifest
```

See [vsce/README.md](vsce/README.md) for end-user feature documentation, or [ksail.devantler.tech/vscode-extension](https://ksail.devantler.tech/vscode-extension/) for the full docs site page.

## Project Structure

The repository is organized around the top-level CLI entry point (`main.go`) and the public packages in `pkg/`.

- **main.go** - CLI entry point
- **pkg/cli/cmd/** - CLI command implementations
- **pkg/** - Public packages (importable by external projects)
- **docs/** - Astro documentation site
- **vsce/** - VSCode extension

### Key Packages in pkg/

- **apis/** - API types, schemas, and enums (distribution/provider values)
- **client/** - Embedded tool clients (kubectl, helm, flux, argocd, docker, k9s, kubeconform, kustomize, oci, netretry); distribution tools like kind, k3d, and vcluster are used directly via their SDKs in provisioners, not wrapped in `pkg/client/`
- **client/reconciler/** - Common base for GitOps reconciliation clients (Flux and ArgoCD)
- **svc/detector/** - Detects installed Kubernetes components (Helm releases and Kubernetes API); used by the update command to build accurate baseline cluster state
- **svc/diff/** - Computes configuration differences between ClusterSpec values; classifies update impact (in-place, reboot-required, recreate-required)
- **svc/image/** - Container image export/import services for Vanilla and K3s distributions
- **svc/installer/** - Component installers (CNI, CSI, metrics-server, etc.)
- **svc/provider/** - Infrastructure providers (e.g., `docker.Provider` for running nodes as containers)
- **svc/provisioner/** - Distribution provisioners (Vanilla, K3s, Talos, VCluster)
- **svc/registryresolver/** - OCI registry detection, resolution, and artifact push utilities
- **svc/state/** - Cluster state persistence for distributions that cannot introspect their running configuration (Kind, K3d)
- **di/** - Dependency injection for wiring components

### Architecture: Providers vs Provisioners

KSail separates infrastructure management from distribution configuration:

- **Providers** manage the infrastructure lifecycle (start/stop containers)
- **Provisioners** configure and manage Kubernetes distributions

| Distribution | Provisioner            | Tool  | Provider              | Description                                    |
|--------------|------------------------|-------|-----------------------|------------------------------------------------|
| `Vanilla`    | KindClusterProvisioner | Kind  | Docker                | Standard upstream Kubernetes                   |
| `K3s`        | K3dClusterProvisioner  | K3d   | Docker                | Lightweight K3s in Docker                      |
| `Talos`      | TalosProvisioner       | Talos | Docker, Hetzner, Omni | Immutable Talos Linux                          |
| `VCluster`   | VClusterProvisioner    | Vind  | Docker                | Virtual clusters via vCluster (Vind) in Docker |

This project strives to be fully open-source friendly. All core functionality is implemented in the `pkg/` directory so external projects can import and use any package under `pkg/`. The `internal/` directory is intentionally minimal — it holds only `internal/buildmeta`, which carries build-time version metadata injected via ldflags (not useful to external consumers).

For detailed package and API documentation, refer to [pkg.go.dev/github.com/devantler-tech/ksail/v7](https://pkg.go.dev/github.com/devantler-tech/ksail/v7).

## CI

### GitHub Workflows

#### Unit Tests

```sh
# working-directory: ./
go test ./...
```

#### System Tests

System tests exercise full cluster lifecycle scenarios across all supported distributions (Vanilla, K3s, Talos, VCluster) and providers (Docker, Hetzner, Omni). They are configured in `.github/workflows/ci.yaml` and the composite action at `.github/actions/ksail-system-test/action.yaml`.

**When they run:**

System tests run in GitHub’s **merge queue** (`merge_group` event) and do **not** run on regular `pull_request` checks. This is intentional:

- **Cost**: The test matrix spans 44+ jobs (4 distributions × 2 init modes × 5 config variants + cloud providers), consuming 6–11 CPU-hours per run. Running this on every PR push would be prohibitively expensive.
- **Feedback time**: A full system test run takes 20–30 minutes. Deferring to the merge queue keeps PR feedback loops fast (unit tests, linting, and build run on every PR push instead).
- **Flakiness**: Cloud provider tests (Hetzner, Omni) are inherently flaky due to network and infrastructure variability. Running them on PRs would produce noisy failures unrelated to code changes.

**Manual trigger:**

You can manually trigger system tests from any branch using `workflow_dispatch`:

```sh
gh workflow run ci.yaml --ref your-branch --field run_system_tests=true
```

This is useful for validating infrastructure-sensitive changes before entering the merge queue.

**Lightweight tests on every PR with code changes:**

The `gen-smoke-test` job runs on every PR that has Go source changes (`needs.changes.outputs.code == 'true'`) and validates:

- Most `workload gen` subcommands (manifest generation + schema validation); `clusterrole` and `role` require live API-server discovery and are covered by system tests instead
- Smoke tests for `chat --help` and `mcp --help`

These tests do not require Docker or a cluster and complete in under a minute.

Note: cipher encrypt/decrypt E2E testing is not currently possible because the encrypt command uses hardcoded empty key groups (no `.sops.yaml` config loading). Cipher commands are covered by unit tests and benchmarks in `pkg/cli/cmd/cipher/`.

**What the system test covers:**

Each matrix job runs a full cluster lifecycle: `init` → `create` → workload deployment → `update` (regression detection) → `stop` → `start` → `delete`, along with workload read operations (`get`, `describe`, `logs`), scaling, and cleanup. See `.github/actions/ksail-system-test/action.yaml` for the complete test sequence.

**If system tests fail in the merge queue:**

The merge is blocked until the failure is resolved. The CI includes a comprehensive debug action (`.github/actions/debug-kubernetes-failure/`) that collects Kubernetes diagnostics (node status, pod status, events, component logs) to aid investigation.

#### Hetzner Provider Testing

To test the Hetzner provider locally, you need:

- **`HCLOUD_TOKEN`** – Hetzner Cloud API token with read/write permissions
- **Talos ISO** – A Talos Linux ISO must be available in your Hetzner Cloud project. The ISO ID is specific to your project and may change over time; KSail currently assumes a default ID of `122630`, but you should look up the actual ID under **Images → ISOs** in the Hetzner Cloud Console and configure/use that value in your environment.

**Note:** Some unit tests and CLI code paths enable Hetzner functionality when `HCLOUD_TOKEN` is set. If you’re not intentionally testing Hetzner, unset `HCLOUD_TOKEN` (or set it to an empty value) before running `go test ./...` to keep tests hermetic.

**Note:** Hetzner tests incur cloud costs. Use `ksail cluster delete` to clean up resources.

**Note:** CI includes a safety-net cleanup job (`cleanup-hetzner`) that runs after system tests and deletes any Hetzner resources labeled `ksail.owned=true`. This is implemented as a GitHub Action at `.github/actions/cleanup-hetzner/action.yaml` and is not intended for local execution.

**Warning:** The cleanup action is destructive and will delete all KSail-managed Hetzner resources (servers, placement groups, firewalls, and networks) in your project that are labeled `ksail.owned=true`. Manual cleanup of any remaining resources should be done via the Hetzner Cloud Console or `hcloud` CLI if needed.

#### Omni Provider Testing

To test the Omni provider locally, you need:

- **`OMNI_SERVICE_ACCOUNT_KEY`** – A Sidero Omni service account key with cluster management permissions. The environment variable name is configurable via `spec.provider.omni.serviceAccountKeyEnvVar` in `ksail.yaml`.
- **Omni endpoint** – The URL of your Sidero Omni instance, configured via `spec.provider.omni.endpoint` in `ksail.yaml` (there is no CLI flag for this value).

**Note:** Omni requires a [Sidero Omni](https://www.siderolabs.com/omni/) account and does not run locally. Omni manages the Talos machine lifecycle; `StartNodes` and `StopNodes` are no-ops in the Omni provider.

**CI integration:** Omni system tests run as part of the `system-test` matrix in `.github/workflows/ci.yaml` alongside Docker and Hetzner tests. They execute the same broader system-test workflow against a live Omni endpoint, including cluster lifecycle, workload, backup/restore, and start/stop validation steps. Omni test failures **block merge** (they are not optional). The following repository secret and variable must be configured for CI:

- **`secrets.OMNI_SERVICE_ACCOUNT_KEY`** – Repository secret containing the Omni service account key.
- **`vars.OMNI_ENDPOINT`** – Repository variable containing the Omni instance URL.

The workflow also sets **`KSAIL_SPEC_CLUSTER_OMNI_MACHINECLASS`** to `ksail` via `env`; this specifies the Omni machine class used for test nodes.

**Note:** CI includes a safety-net cleanup job (`cleanup-omni`) that runs after system tests and deletes the known system-test clusters remaining in Omni. This is implemented as a GitHub Action at `.github/actions/cleanup-omni/action.yaml` and is not intended for local execution.

#### Scheduled Workflows

| Workflow        | Schedule                   | Purpose                            |
|-----------------|----------------------------|------------------------------------|
| `update-skills` | Daily (06:00 UTC)          | Copilot skills upgrades            |
| `maintenance`   | Monthly (1st, 00:00 UTC)   | Old workflow run and image cleanup |
| `sync-labels`   | Weekly (Monday, 07:00 UTC) | Label synchronization              |

#### Agentic Workflows

KSail uses [GitHub Agentic Workflows](https://github.github.com/gh-aw/) (`.github/workflows/*.md`) to automate continuous improvement tasks. These are AI-driven workflows that run on a schedule or on dispatch:

| Workflow                     | Schedule                                    | Purpose                                                                    |
|------------------------------|---------------------------------------------|----------------------------------------------------------------------------|
| `repo-assist`                | Every 12h / On `/repo-assist`               | Issue triage, code quality, building, planning, and repository maintenance |
| `daily-workflow-maintenance` | Daily (18:00 UTC)                           | CI/CD workflow updates, optimization, CI coaching, and dependency upgrades |
| `daily-docs`                 | Daily (22:00 UTC) / On push / On `/unbloat` | Documentation sync with code changes, bloat reduction, and link fixing     |
| `weekly-strategy`            | Weekly (Mon + Wed) / On dispatch            | Market research, roadmap planning (Mon), and project promotion (Wed)       |
| `ci-doctor`                  | On CI failure                               | CI failure investigation, diagnostics, and Go-specific analysis            |

Each agentic workflow creates a GitHub Discussion to coordinate its work and, depending on its purpose, may open draft PRs or create issues with incremental improvements. You can control them using the [`gh aw`](https://github.com/github/gh-aw) CLI extension:

```sh
# Install the gh-aw extension (prerequisite)
gh ext install github/gh-aw

# Manage a specific workflow
gh aw disable daily-workflow-maintenance --repo devantler-tech/ksail
gh aw enable daily-workflow-maintenance --repo devantler-tech/ksail
gh aw run daily-workflow-maintenance --repo devantler-tech/ksail
gh aw logs daily-workflow-maintenance --repo devantler-tech/ksail
```

## CD

### Release Process

The release process for KSail is fully automated and split across two GitHub Actions workflows:

1. **Release** (`.github/workflows/release.yaml`) runs on pushes to `main` and creates the next semantic version tag (`vX.Y.Z`) based on Conventional Commits (typically the PR title / squash-merge commit message).
2. **CD** (`.github/workflows/cd.yaml`) runs on tag pushes (`v*`) and uses **GoReleaser** to build and publish release artifacts, followed by MCP registry publishing, documentation deployment to GitHub Pages, VSCode extension publishing, and a Homebrew tap PR.

Versioning conventions:

- **fix:** Patch release (e.g. 1.0.1)
- **feat:** Minor release (e.g. 1.1.0)
- **BREAKING CHANGE** or **`!`**: Major release (e.g. 2.0.0)

The changelog is generated by **GoReleaser** from the commit history, so keep PR titles and commit messages clear and descriptive.

#### Atomic Draft Release Workflow

The CD workflow implements an atomic publication strategy to ensure users never see incomplete releases with missing artifacts:

1. **Draft Creation**: **GoReleaser** creates a **draft release** (configured in `.goreleaser.yaml`) with:
   - Compiled binaries for multiple platforms (Darwin arm64, Linux/Windows on amd64/arm64)
   - Docker images published to GHCR
   - Generated changelog from commit history

   GoReleaser also opens a separate PR to update the Homebrew cask in [`devantler-tech/homebrew-tap`](https://github.com/devantler-tech/homebrew-tap) (branch pattern: `goreleaser/ksail-vX.Y.Z`).

2. **VSCode Extension Upload**: A separate job builds the VSCode extension and uploads it as a release asset to the same draft release.

3. **Atomic Publication**: A final `publish-release` job waits for both the `goreleaser` and `vscode-extension` jobs to complete successfully, then publishes the draft release.

This workflow ensures that:

- Releases are only published after **all artifacts** are uploaded
- Users never encounter partial releases with missing binaries or extensions
- If any job fails, the draft remains unpublished and can be deleted or fixed manually
