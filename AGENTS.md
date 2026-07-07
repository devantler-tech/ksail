# KSail - Kubernetes SDK for Local GitOps Development

KSail is a Go-based CLI application that provides a unified SDK for spinning up local Kubernetes clusters and managing workloads declaratively. It embeds common Kubernetes tools as Go libraries, requiring only Docker as an external dependency.

**ALWAYS reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.**

## Working Effectively

### Prerequisites and Dependencies

**CRITICAL**: Install Docker before using KSail:

- Docker is the only required external dependency for local clusters (the Docker provider)
- KSail embeds kubectl, helm, kind, k3d, vcluster, flux, and argocd as Go libraries
- No separate installation of these tools is needed
- The Hetzner provider is supported for Talos clusters and requires cloud access/credentials (e.g., `HCLOUD_TOKEN`)
- The Omni provider is supported for Talos clusters and requires a Sidero Omni account and credentials (e.g., `OMNI_SERVICE_ACCOUNT_KEY`, configurable via `spec.provider.omni.serviceAccountKeyEnvVar`)

**Required for Documentation**:

```bash
# Node.js for documentation builds
# CI uses Node.js 24 (see .github/workflows/ci.yaml)
cd /path/to/repo/docs
npm ci
```

### Build Commands

**Main Application Build**:

```bash
cd /path/to/repo
go build -o ksail
# Takes a few seconds on first run for Go module downloads

# For optimized builds (strips debug symbols):
go build -ldflags="-s -w" -o ksail-optimized
# Strips debug symbols and can significantly reduce binary size (in some cases by ~25–35%; see #2095 for an example benchmark; actual size varies by OS/arch, Go version, and dependencies)
# Note: release builds additionally inject version metadata via -X flags (Version, Commit, Date) through GoReleaser
```

**Run Unit Tests**:

```bash
cd /path/to/repo
go test ./...
# Runs all tests in the repository
```

**Build Documentation**:

```bash
cd /path/to/repo/docs
npm run build
# Takes ~2-3 seconds. Documentation builds to dist/ directory
```

**Build VSCode Extension**:

```bash
cd /path/to/repo/vsce
npm ci
npm run compile
# Package: npx @vscode/vsce package --no-dependencies
```

### Running the Application

**CLI Usage**:

```bash
cd /path/to/repo
./ksail --help                    # Show all commands
./ksail project init --help       # Show init options
./ksail project init              # Initialize default project
./ksail project init --distribution VCluster  # Initialize VCluster project
./ksail cluster create            # Create cluster (requires Docker)
./ksail cluster update            # Update cluster configuration
./ksail cluster info              # Show cluster info
./ksail cluster delete            # Destroy cluster
```

**Or build and run directly**:

```bash
go run main.go --help
```

## Validation

**ALWAYS validate changes by running through complete scenarios:**

1. **Build Validation**:

   ```bash
   cd /path/to/repo
   go build -o ksail  # Must succeed
   ```

2. **Test Validation**:

   ```bash
   cd /path/to/repo
   go test ./...  # Most tests should pass
   ```

3. **CLI Functional Validation**:

   ```bash
   cd /tmp && mkdir test-ksail && cd test-ksail
   /path/to/repo/ksail project init
   # Should create: ksail.yaml, kind.yaml, k8s/kustomization.yaml
   ```

4. **Documentation Validation**:

   ```bash
   cd /path/to/repo/docs
   npm ci
   npm run build  # Must succeed
   ls dist/  # Should contain generated HTML files
   ```

5. **VSCode Extension Validation** (optional):

   ```bash
   cd /path/to/repo/vsce
   npm ci
   npm run compile  # Must succeed
   ```

## Common Tasks

### Project Structure

```text
/
├── main.go                 # Main entry point
├── internal/               # Internal (import-restricted/private) packages
│   ├── buildmeta/          # Build-time version metadata (Version, Commit, Date) injected via ldflags
│   ├── controller/         # controller-runtime reconcilers for the KSail operator (Cluster CRs)
│   └── testutil/           # Shared test utilities (home-env isolation, root checks, snapshot helpers)
├── pkg/                    # Core packages
│   ├── toolgen/            # Tool generation for AI assistants
│   ├── apis/               # API types and schemas
│   ├── cli/                # CLI wiring, UI, and Cobra commands
│   │   ├── annotations/    # Command annotation constants
│   │   ├── cmd/            # CLI command implementations
│   │   ├── dockerutil/     # Docker client lifecycle management utilities
│   │   ├── editor/         # Editor configuration resolution
│   │   ├── flags/          # CLI flag handling utilities
│   │   ├── kubeconfig/     # Kubeconfig file path helpers
│   │   ├── lifecycle/      # Cluster lifecycle orchestration
│   │   ├── setup/          # Component setup (CNI, mirror registries, etc.)
│   │   └── ui/             # Terminal UI (ASCII art, chat TUI, confirmations)
│   ├── client/             # Tool clients (kubectl, helm, flux, argocd, sops, etc.; eksctl is a binary shim)
│   ├── di/                 # Dependency injection
│   ├── envvar/             # Environment variable utilities
│   ├── fsutil/             # Filesystem utilities (includes configmanager)
│   ├── k8s/                # Kubernetes helpers/templates
│   ├── notify/             # CLI notifications and progress display
│   ├── operator/           # Kubernetes operator manager and REST API server (reconcilers in internal/controller)
│   ├── runner/             # Cobra command execution helpers
│   ├── strutil/            # String utilities
│   ├── timer/              # Command timing and performance tracking
│   ├── webui/              # Embedded web UI assets (built from web/ui, served by `ksail open web` and the operator)
│   └── svc/                # Services (installers, managers, etc.)
│       ├── chat/           # AI chat integration (GitHub Copilot SDK)
│       ├── detector/       # Detects installed Kubernetes components (Helm releases, K8s API)
│       │   ├── cluster/    # Detects distribution, provider, cluster name from kubeconfig context
│       │   └── gitops/     # Detects existing GitOps CRs (FluxInstance, ArgoCD Application) in source dir
│       ├── diff/           # Computes ClusterSpec config diffs and classifies update impact
│       ├── image/          # Container image export/import services
│       │   └── parser/     # Parses image references from Dockerfiles
│       ├── installer/      # Component installers (CNI, CSI, metrics-server, etc.)
│       ├── mcp/            # Model Context Protocol server
│       ├── provider/       # Infrastructure providers (docker, hetzner, omni)
│       ├── provisioner/    # Distribution provisioners (Vanilla, K3s, Talos, VCluster, KWOK, EKS)
│       ├── registryresolver/ # OCI registry detection, credential resolution, and artifact push
│       └── state/          # Cluster state persistence for distributions without introspection
├── charts/                 # Helm charts
│   └── ksail-operator/     # Operator + embedded web UI chart (keep README.md in sync with values.yaml)
├── copilot-plugin/         # KSail plugin for GitHub Copilot CLI / Claude Code (MCP server + skill)
├── desktop/                # Native desktop app (separate Go module wrapping the web UI)
├── web/                    # Web UI source
│   └── ui/                 # Vite/React SPA, embedded into the binary via pkg/webui
├── docs/                   # Astro documentation source
│   ├── dist/               # Generated site (after npm run build)
│   └── package.json        # Node.js dependencies for documentation
├── schemas/                # JSON schema for ksail-config
│   ├── doc.go              # contains //go:generate go run gen_schema.go for schema generation
│   ├── gen_schema.go       # schema generator code invoked by go:generate; produces ksail-config.schema.json
│   └── ksail-config.schema.json  # JSON Schema for ksail.yaml — consumable by YAML language servers and editors (including VS Code YAML tooling)
├── vsce/                   # VSCode extension source
│   ├── src/                # Extension TypeScript source
│   └── package.json        # Extension manifest and dependencies
├── go.mod                  # Go module file
└── README.md               # Main repository documentation
```

### Key Configuration Files

- **go.mod**: Go module dependencies (includes embedded kubectl, helm, kind, k3d, vcluster, flux, argocd)
- **package.json**: Node.js dependencies for Astro documentation
- **.github/workflows/\*.yaml**: CI/CD pipelines

### CLI Commands Reference

Use `ksail --help` and the generated CLI reference (`docs/src/content/docs/cli-flags/`) as the
source of truth for the full command and flag inventory. Top-level command groups:

- `ksail cluster` — cluster lifecycle and operations (init, create, update, delete, diagnose, backup/restore, oidc, …); see `ksail cluster --help`. Includes `ksail cluster oidc` — OIDC authentication utilities (kubeconfig exec credential plugin)
- `ksail workload` — workload operations against a cluster (apply, get, logs, gen, push, reconcile, …), including `ksail workload cipher` for SOPS secret management; see `ksail workload --help`
- `ksail tenant` — multi-tenancy onboarding (RBAC isolation, GitOps sync resources, tenant repo scaffolding); see `ksail tenant --help`
- `ksail open` — open a KSail interface: `ksail open web` (local web server + browser), `ksail open desktop` (native desktop app), `ksail open chat` (AI chat powered by GitHub Copilot), and `ksail open mcp` (MCP server for AI assistants); see `ksail open --help`
- `ksail operator` — run the Kubernetes operator (normally deployed via the Helm chart); see `ksail operator --help`

### Init Command Options

Use the CLI help output as the source of truth:

```bash
ksail project init --help
# See also: docs/src/content/docs/cli-flags/cluster/cluster-init.mdx

# Supported distributions:
# --distribution Vanilla   # Standard Kubernetes via Kind
# --distribution K3s       # Lightweight K3s via K3d
# --distribution Talos     # Immutable Talos Linux
# --distribution VCluster  # Virtual clusters via Vind
# --distribution KWOK      # Simulated Kubernetes cluster via kwokctl
# --distribution EKS       # Managed Kubernetes on AWS via eksctl

```

### Troubleshooting Build Issues

**"Go version mismatch"**:

```bash
go version  # Should match the version in go.mod (go 1.26.1)
# If not, install/update Go from https://go.dev/dl/
```

**Docker Connection Issues**:

```bash
docker ps  # Should list running containers
# If not, ensure Docker daemon is running
```

### Making Changes

**Always build and test after making changes**:

```bash
cd /path/to/repo
go build -o ksail                      # Verify builds
go test ./...                          # Run unit tests
./ksail --help                         # Test CLI functionality
```

**For documentation changes**:

```bash
cd /path/to/repo/docs
npm ci                                 # Install dependencies
npm run build                          # Verify docs build
npm run dev                            # Test locally (if needed)
```

### Important Notes

- The project uses **Go 1.26.1+** (see `go.mod`)
- Kubernetes tools are embedded as Go libraries - only Docker is required externally for local clusters (the EKS distribution is the exception: it shells out to an external `eksctl` binary)
- Unit tests run quickly and should generally pass
- System tests in CI cover extensive scenarios with multiple tool combinations
- Documentation is built with Astro and uses the Starlight theme
- Build times: ~2-3 minutes for initial build (downloads dependencies), faster on subsequent builds
- **NEVER CANCEL** long-running builds - they need time to download packages and compile

## Architecture Overview

For a deeper dive into KSail's design and internals, refer to:

- [Architecture Guide](https://ksail.devantler.tech/architecture/) — Design principles, component architecture, provider/provisioner model, and state persistence
- [Development Guide](https://ksail.devantler.tech/development/) — Development environment setup, coding standards, testing patterns, and CI/CD workflows

**Providers vs Provisioners:**

- **Providers** (`pkg/svc/provider/`) manage infrastructure lifecycle (start/stop containers or cloud servers)
  - `docker.Provider`: Runs Kubernetes nodes as Docker containers
  - `hetzner.Provider`: Runs Kubernetes nodes as Hetzner Cloud servers
  - `omni.Provider`: Manages Talos cluster nodes through the Sidero Omni SaaS API
  - `aws.Provider`: Manages EKS clusters on Amazon Web Services
- **Provisioners** (`pkg/svc/provisioner/`) configure and manage Kubernetes distributions
  - `KindClusterProvisioner` (`pkg/svc/provisioner/cluster/kind/`): Uses Kind SDK for standard upstream Kubernetes
  - `K3dClusterProvisioner` (`pkg/svc/provisioner/cluster/k3d/`): Uses K3d via Cobra/SDK for lightweight K3s clusters
  - `TalosProvisioner` (`pkg/svc/provisioner/cluster/talos/`): Uses Talos SDK for immutable Talos Linux clusters
  - `VClusterProvisioner` (`pkg/svc/provisioner/cluster/vcluster/`): Uses vCluster Go SDK (Vind Docker driver) for virtual Kubernetes clusters
  - `KWOKProvisioner` (`pkg/svc/provisioner/cluster/kwok/`): Uses kwokctl for simulated Kubernetes clusters (lightweight, no real workloads)
  - `EKSProvisioner` (`pkg/svc/provisioner/cluster/eks/`): Shells out to an external `eksctl` binary (via `pkg/client/eksctl`) for managed EKS clusters on AWS — the one tool KSail does not embed

**Distribution Names (user-facing):**

| Distribution | Tool    | Provider              | Description                                      |
|--------------|---------|-----------------------|--------------------------------------------------|
| `Vanilla`    | Kind    | Docker                | Standard upstream Kubernetes                     |
| `K3s`        | K3d     | Docker                | Lightweight K3s in Docker                        |
| `Talos`      | Talos   | Docker, Hetzner, Omni | Immutable Talos Linux                            |
| `VCluster`   | Vind    | Docker                | Virtual clusters via vCluster (Vind) in Docker   |
| `KWOK`       | kwokctl | Docker                | Simulated Kubernetes cluster (no real workloads) |
| `EKS`        | eksctl  | AWS                   | Managed Kubernetes on Amazon Web Services        |

**Key Packages:**

- `pkg/toolgen/`: AI tool generation — auto-generates tools from the Cobra command tree for both the MCP server and the Copilot chat assistant. All runnable CLI commands (except excluded meta commands: `chat`, `mcp`, `completion`, `help`, root — see `toolgen.DefaultOptions()` and `ai.toolgen.exclude`) are automatically exposed as tools; do NOT manually register individual tool handlers. Parent commands annotated with `ai.toolgen.consolidate` group their subcommands into a single tool, then `ai.toolgen.permission` splits them into read/write pairs (e.g., `cluster_read`, `cluster_write`). Adding a new CLI command under a consolidated parent automatically makes it available as an MCP tool and a chat tool — no separate tool registration is needed
- `pkg/apis/`: API types, schemas, and enums; each enum type lives in its own file under `pkg/apis/cluster/v1alpha1/` (e.g., `distribution.go`, `cni.go`, `csi.go`, `loadbalancer.go`, `gitopsengine.go`, etc.); the `EnumValuer` interface is in `enum.go`; API-level validation errors (e.g., `ErrInvalidDistribution`, `ErrInvalidGitOpsEngine`, `ErrClusterNameTooLong`, `ErrInvalidDistributionProviderCombination`) are centralized in `errors.go`
- `pkg/client/`: Tool clients (argocd, docker, eksctl, flux, helm, k9s, klogutil, kubeconform, kubectl, kubescape, kustomize, netretry, oci, reconciler, sops) — all embedded as Go libraries except eksctl, which shells out to an external `eksctl` binary; distribution tools like kind, k3d, and vcluster are used directly via their SDKs in provisioners, not wrapped in `pkg/client/`.
- `pkg/svc/`: Services including installers, providers, and provisioners
  - `pkg/svc/chat/`: AI chat integration using GitHub Copilot SDK with embedded CLI documentation; `sandbox.go` exports `IsPathWithinDirectory` which uses `fsutil.EvalCanonicalPath` for path containment checks
  - `pkg/svc/detector/`: Detects installed Kubernetes components by querying Helm release history and the Kubernetes API; used by the update command to build accurate baseline state
    - `pkg/svc/detector/cluster/`: Detects Kubernetes distribution, provider, and cluster name by analyzing kubeconfig context names and server endpoints; exposes `DetectInfo`, `DetectDistributionFromContext`, and `ResolveKubeconfigPath`
    - `pkg/svc/detector/gitops/`: Detects existing GitOps Custom Resources (FluxInstance, ArgoCD Application) managed by KSail in the source directory
  - `pkg/svc/diff/`: Computes configuration differences between old and new ClusterSpec values; classifies update impact (in-place, reboot-required, recreate-required)
  - `pkg/svc/image/`: Container image export/import services for Vanilla and K3s distributions; `parser/` sub-package provides `ParseAllImagesFromDockerfile` for extracting all `FROM` directives from multi-stage Dockerfiles (used by Flux installer to include distribution controller images in mirror cache warming)
  - `pkg/svc/installer/`: Component installers (CNI, CSI, metrics-server, etc.); `internal/hetzner/` holds shared utilities for the Hetzner installers—`hcloudccm.Installer` is a type alias for `hetzner.Installer`, while `hetznercsi.Installer` is a thin wrapper that embeds `*hetzner.Installer` and adds a pre-install gate waiting for `hcloud-ccm` to label all nodes with `instance.hetzner.cloud/provided-by` (preventing a CSI topology registration race); both share a single `EnsureSecret` implementation; `flux/Dockerfile.distribution` tracks Flux distribution controller images (updated by Dependabot) that are deployed by the Flux operator when creating a FluxInstance but are not part of the Helm chart — included in `Images()` output for mirror cache warming
  - `pkg/svc/mcp/`: Model Context Protocol server for Claude and other AI assistants; tools are auto-generated from root Cobra commands via `pkg/toolgen/` (not manually registered) — all operational cluster/workload/tenant commands (both read and write) are consolidated into 5 tools via `ai.toolgen.consolidate` + `ai.toolgen.permission`: `cluster_read`, `cluster_write`, `workload_read`, `workload_write`, `tenant_write` (the `cipher` SOPS subcommands are nested under `workload`, so they fold into `workload_write`)
  - `pkg/svc/provider/`: Infrastructure providers (docker, hetzner, omni)
  - `pkg/svc/provisioner/`: Distribution provisioners (Vanilla, K3s, Talos, VCluster, KWOK, EKS)
  - `pkg/svc/registryresolver/`: OCI registry detection, resolution, credential merging from cluster secrets (Flux dockerconfigjson / ArgoCD repo secret), and artifact push utilities; `ErrExternalRegistryCredentialsIncomplete` is returned when a username is set (e.g. `GITHUB_ACTOR`) but the password/token is missing
  - `pkg/svc/state/`: Cluster state persistence for distributions that cannot introspect running configuration (Kind, K3d); stores spec as JSON in `~/.ksail/clusters/<name>/spec.json`
- `pkg/client/reconciler/`: Common base for GitOps reconciliation clients (Flux and ArgoCD)
- `pkg/di/`: Dependency injection for wiring components
- `pkg/k8s/`: Kubernetes helpers and templates
- `pkg/cli/`: CLI wiring, commands, and terminal UI components
- `pkg/envvar/`: Environment variable utilities
- `pkg/fsutil/`: Filesystem utilities (includes configmanager for configuration loading); exports `EvalCanonicalPath` (filepath.Abs + filepath.EvalSymlinks with parent fallback) for safe path canonicalization, and `ReadFileSafe` for path-traversal-safe file reads — **all user-supplied file path arguments in CLI commands must be canonicalized with `EvalCanonicalPath` before use** (resolves symlinks, prevents symlink-escape attacks); for output paths that may not yet exist, call `os.MkdirAll(filepath.Dir(outputPath), <mode>)` first, then `EvalCanonicalPath`; for constrained reads, use `ReadFileSafe` instead of reimplementing containment checks
- `pkg/notify/`: CLI notifications and progress display utilities
- `pkg/runner/`: Cobra command execution helpers
- `pkg/timer/`: Command timing and performance tracking

## Active Technologies

- Go 1.26.1+ (see `go.mod`)
- Embedded Kubernetes tools (kubectl, helm, kind, k3d, vcluster, flux, argocd) as Go libraries
- Docker as the only external dependency for local clusters (EKS additionally shells out to `eksctl`)
- Astro with Starlight for documentation (Node.js-based)

## Maintenance (autonomous AI assistant)

These conventions guide the autonomous **Daily AI Assistant** — and any agentic tool (Copilot,
Cursor, …) — doing repository maintenance. The **shared** cross-repo conventions (draft-PR
checkpoint, trusted-author merge policy, conventional commits, per-run worktrees, untrusted-input
handling, root-cause fixes, the `> 🤖 Generated by the Daily AI Assistant` prefix, etc.) are
defined centrally in the devantler-tech monorepo `AGENTS.md` and apply here too — follow that
document rather than relying on the summary below. In short: act on judgement and ship a **draft
PR** as the checkpoint (the maintainer's promotion to "ready" is the go-signal); **drive
trusted-author PRs to merge** (incl. dependency major bumps) once required checks are green and
threads resolved, **never merge external PRs** and never self-merge your own unreviewed drafts;
trusted authors are the GitHub logins `devantler`, `ksail-bot`, `dependabot[bot]`,
`github-actions[bot]`, and `renovate[bot]`; never push to `main`. This section adds
KSail-specifics.

**Recommended local validation before any PR** (matches `CONTRIBUTING.md`; CI re-runs equivalents
via the org-wide `validate-go-project` reusable workflow): `golangci-lint run --fix` to format and
auto-fix; then `go build -o /tmp/ksail-maint . && go test ./... && golangci-lint run --timeout 5m`.
Workflows and other files → `mega-linter-runner -f go` (MegaLinter runs `actionlint` on
`.github/workflows/`). Docs → `cd docs && ([ -d node_modules ] || npm ci) && npm run build`.

**Generated — never hand-edit; run the generator:** `make generate` regenerates everything in
dependency order and is THE regeneration command. The artifacts, for reference:
`schemas/ksail-config.schema.json` (`go generate ./schemas/...`), CRD + deepcopy
(`go generate ./pkg/apis/...`), `docs/src/content/docs/cli-flags/` +
`docs/src/content/docs/configuration/declarative-configuration.mdx` (`go generate ./docs/...`),
`pkg/svc/chat/docs_generated.go` (`go generate ./pkg/svc/chat/...`, after docs), `mocks.go` files
(`mockery`), and `web/ui/src/generated/ksail-config.ts` (`npm --prefix web/ui run gen:types`,
after schemas). See also `.github/instructions/`.
**Shared machine / autonomous worktrees:** only create/inspect/delete
clusters you created; build throwaway binaries to `/tmp` (not `./ksail`) to avoid polluting the
worktree. Maintainers building locally should still use the standard `make build` (`go build -o
ksail .`).

**Feature-flag / experimental gating** (portfolio convention — see monorepo#2059): a new,
not-yet-stable command ships **behind the experimental gate, off by default**, and is flipped on only
after validation. Wrap the command's constructor return in **`experimental.Guard(cmd)`**
(`pkg/cli/experimental`): it hides the command from `--help` + the MCP/tool surface and refuses to run
unless the user passes the global **`--experimental`** flag (`flags.ExperimentalFlagName`). Prefer a
lightweight cobra gate over a runtime SDK for the common case; reach for the OpenFeature Go SDK only
where genuinely per-user/remote evaluation is needed. Rules:

- **Test both states** — the gated path is covered on (`--experimental`) and off (refused with
  `experimental.ErrDisabled`); default snapshots stay deterministic (a hidden command drops from the
  help/toolgen snapshots, so regenerate them — see below).
- **Config-gated behaviour** that is not a whole command → a typed `experimental` field in `ksail.yaml`
  (regenerate the schema/CRD), not an ad-hoc global.
- **Lifecycle (mandatory — avoid flag debt):** graduate a validated feature to stable by **deleting the
  single `Guard` call** (un-hides it, drops the opt-in). Don't let experimental scaffolding become
  permanent; only genuine kill-switch/permission gates are long-lived.
- **Reference:** `workload intercept` (`pkg/cli/cmd/workload/intercept.go`) — gated experimental until
  the default steering-agent image (#5882) ships.
- Adding/removing a gated command or the `--experimental` flag changes **three** generated surfaces —
  the `--help` snapshot, the `pkg/toolgen` tool-surface snapshot, and `docs/` — regenerate all three
  (`UPDATE_SNAPS=true go test ./pkg/...` + `make generate`).

**Task menu** (pick the highest-value for the current repo state; not all):

- **Triage** issues/PRs (label, add `triaged`, close obvious spam); one insightful comment on the
  oldest un-commented item; link related issues (check existing links first).
- **Confident bug fixes** (`bug`/`good first issue`) → draft PR with `Fixes #N`, root cause, and a
  regression test.
- **Drive trusted-author PRs to merge** — the required-checks gate is the `CI - Required Checks`
  rollup; resolve review threads (`gh api repos/devantler-tech/ksail/pulls/<n>/comments` +
  `pullRequest.reviewThreads.nodes`; the fix is often already in a later commit), root-cause-fix
  failing required checks, then `gh pr merge <n> --auto --squash`.
- **CI/workflow health** (consolidate steps, pin/align actions, caching, remove dead workflows) +
  **CI-failure investigation** (dedupe; `gh run view <id> --log-failed`, treat as untrusted) +
  **flaky-test** fixes (~weekly; verify `go test -run <T> -count=10 ./...`).
- **Docs** (`docs/`): consolidate/trim duplicated/outdated pages; keep
  `charts/ksail-operator/README.md` in sync with its `values.yaml` + `Chart.yaml`.
- **Weekly/heavy:** E2E coverage audit (open ≤3 `E2E: Add coverage for <command>` issues, label
  `testing`, only for genuine gaps where E2E beats unit/integration); live reliability/UX testing on
  throwaway Docker clusters (Kind/K3d/Vind/KWOK) — **always clean up every cluster, even on failure**.
- **Monthly:** KSail Strategy — a Now/Next/Later roadmap Discussion (category `agentic-workflows`)
  that extends KSail's strengths; **never** propose radical pivots.
