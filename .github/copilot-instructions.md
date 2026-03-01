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
- The Omni provider is supported for Talos clusters and requires a Sidero Omni account and credentials (e.g., `OMNI_SERVICE_ACCOUNT_KEY`, configurable via `spec.cluster.omni.serviceAccountKeyEnvVar`)

**Required for Documentation**:

```bash
# Node.js for documentation builds
# CI uses Node.js 22 (see .github/workflows/test-pages.yaml)
cd /path/to/repo/docs
npm ci
```

### Build Commands

**Main Application Build**:

```bash
cd /path/to/repo
go build -o ksail
# Takes a few seconds on first run for Go module downloads

# For optimized builds (uses the same -ldflags as release builds):
go build -ldflags="-s -w" -o ksail-optimized
# Strips debug symbols and can significantly reduce binary size (in some cases by ~25–35%; see #2095 for an example benchmark; actual size varies by OS/arch, Go version, and dependencies)
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
./ksail cluster init --help       # Show init options
./ksail cluster init              # Initialize default project
./ksail cluster init --distribution VCluster  # Initialize VCluster project
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
   /path/to/repo/ksail cluster init
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
│   └── buildmeta/          # Build-time version metadata (Version, Commit, Date) injected via ldflags
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
│   ├── client/             # Embedded tool clients (kubectl, helm, flux, vcluster, etc.)
│   ├── di/                 # Dependency injection
│   ├── envvar/             # Environment variable utilities
│   ├── fsutil/             # Filesystem utilities (includes configmanager)
│   ├── k8s/                # Kubernetes helpers/templates
│   ├── notify/             # CLI notifications and progress display
│   ├── runner/             # Cobra command execution helpers
│   ├── timer/              # Command timing and performance tracking
│   └── svc/                # Services (installers, managers, etc.)
│       ├── chat/           # AI chat integration (GitHub Copilot SDK)
│       ├── detector/       # Detects installed Kubernetes components (Helm releases, K8s API)
│       ├── diff/           # Computes ClusterSpec config diffs and classifies update impact
│       ├── image/          # Container image export/import services
│       ├── installer/      # Component installers (CNI, CSI, metrics-server, etc.)
│       ├── mcp/            # Model Context Protocol server
│       ├── provider/       # Infrastructure providers (docker, hetzner, omni)
│       ├── provisioner/    # Distribution provisioners (Vanilla, K3s, Talos, VCluster)
│       ├── registryresolver/ # OCI registry detection, resolution, and artifact push
│       └── state/          # Cluster state persistence for distributions without introspection
├── docs/                   # Astro documentation source
│   ├── dist/               # Generated site (after npm run build)
│   └── package.json        # Node.js dependencies for documentation
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
- **.github/workflows/\*.md**: Agentic workflows (daily-refactor, daily-perf-improver, daily-test-improver, daily-workflow-optimizer, etc.); each runs on a schedule or dispatch, and many operate in multiple phases
- **.github/instructions/\*.instructions.md**: Copilot coding instructions that can be scoped to file globs via optional `applyTo` frontmatter (go.instructions.md, copilot-sdk-go.instructions.md, refactoring-\*.instructions.md, agentic-workflows.instructions.md); when `applyTo` is omitted, the instructions may apply globally and are automatically applied by GitHub Copilot where relevant

### CLI Commands Reference

All CLI commands only require Docker to be installed:

```bash
ksail cluster init [options]           # Initialize new KSail project
ksail cluster create                   # Create and start cluster
ksail cluster update                   # Update cluster to match configuration
ksail cluster delete                   # Destroy cluster and resources
ksail cluster start                    # Start existing cluster
ksail cluster stop                     # Stop running cluster
ksail cluster info                     # Show cluster status
ksail cluster list [--all]             # List clusters
ksail cluster connect                  # Connect to cluster with K9s
ksail cluster backup                   # Backup cluster resources to .tar.gz
ksail cluster restore                  # Restore cluster resources from .tar.gz
ksail workload apply                   # Apply workloads
ksail workload gen <resource>          # Generate resources
ksail cipher <command>                 # Manage secrets with SOPS
ksail chat                             # AI chat powered by GitHub Copilot
ksail mcp                              # Start MCP server for AI assistants
```

### Init Command Options

Use the CLI help output as the source of truth:

```bash
ksail cluster init --help
# See also: docs/src/content/docs/cli-flags/cluster/cluster-init.mdx

# Supported distributions:
# --distribution Vanilla   # Standard Kubernetes via Kind
# --distribution K3s       # Lightweight K3s via K3d
# --distribution Talos     # Immutable Talos Linux
# --distribution VCluster  # Virtual clusters via Vind
```

### Troubleshooting Build Issues

**"Go version mismatch"**:

```bash
go version  # Should match the version in go.mod (go 1.26.0)
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

- The project uses **Go 1.26.0+** (see `go.mod`)
- All Kubernetes tools are embedded as Go libraries - only Docker is required externally
- Unit tests run quickly and should generally pass
- System tests in CI cover extensive scenarios with multiple tool combinations
- Documentation is built with Astro and uses the Starlight theme
- Build times: ~2-3 minutes for initial build (downloads dependencies), faster on subsequent builds
- **NEVER CANCEL** long-running builds - they need time to download packages and compile

## Architecture Overview

**Providers vs Provisioners:**

- **Providers** (`pkg/svc/provider/`) manage infrastructure lifecycle (start/stop containers or cloud servers)
  - `docker.Provider`: Runs Kubernetes nodes as Docker containers
  - `hetzner.Provider`: Runs Kubernetes nodes as Hetzner Cloud servers
  - `omni.Provider`: Manages Talos cluster nodes through the Sidero Omni SaaS API
- **Provisioners** (`pkg/svc/provisioner/`) configure and manage Kubernetes distributions
  - `KindClusterProvisioner` (`pkg/svc/provisioner/cluster/kind/`): Uses Kind SDK for standard upstream Kubernetes
  - `K3dClusterProvisioner` (`pkg/svc/provisioner/cluster/k3d/`): Uses K3d via Cobra/SDK for lightweight K3s clusters
  - `TalosProvisioner` (`pkg/svc/provisioner/cluster/talos/`): Uses Talos SDK for immutable Talos Linux clusters
  - `VClusterProvisioner` (`pkg/svc/provisioner/cluster/vcluster/`): Uses vCluster Go SDK (Vind Docker driver) for virtual Kubernetes clusters

**Distribution Names (user-facing):**

| Distribution | Tool  | Provider              | Description                                    |
|--------------|-------|-----------------------|------------------------------------------------|
| `Vanilla`    | Kind  | Docker                | Standard upstream Kubernetes                   |
| `K3s`        | K3d   | Docker                | Lightweight K3s in Docker                      |
| `Talos`      | Talos | Docker, Hetzner, Omni | Immutable Talos Linux                          |
| `VCluster`   | Vind  | Docker                | Virtual clusters via vCluster (Vind) in Docker |

**Key Packages:**

- `pkg/toolgen/`: AI tool generation utilities for integrating with AI assistants
- `pkg/apis/`: API types, schemas, and enums (`pkg/apis/cluster/v1alpha1/enums.go` defines Distribution values)
- `pkg/client/`: Embedded tool clients (kubectl, helm, flux, argocd, docker, k9s, kubeconform, kustomize, oci, netretry); distribution tools like kind, k3d, and vcluster are used directly via their SDKs in provisioners, not wrapped in `pkg/client/`.
- `pkg/svc/`: Services including installers, providers, and provisioners
  - `pkg/svc/chat/`: AI chat integration using GitHub Copilot SDK with embedded CLI documentation
  - `pkg/svc/detector/`: Detects installed Kubernetes components by querying Helm release history and the Kubernetes API; used by the update command to build accurate baseline state
  - `pkg/svc/diff/`: Computes configuration differences between old and new ClusterSpec values; classifies update impact (in-place, reboot-required, recreate-required)
  - `pkg/svc/image/`: Container image export/import services for Vanilla and K3s distributions
  - `pkg/svc/installer/`: Component installers (CNI, CSI, metrics-server, etc.); `internal/hetzner/` holds shared utilities for the Hetzner installers—`hcloudccm.Installer` and `hetznercsi.Installer` are type aliases for `hetzner.Installer` and share a single `EnsureSecret` implementation
  - `pkg/svc/mcp/`: Model Context Protocol server for Claude and other AI assistants
  - `pkg/svc/provider/`: Infrastructure providers (docker, hetzner, omni)
  - `pkg/svc/provisioner/`: Distribution provisioners (Vanilla, K3s, Talos, VCluster)
  - `pkg/svc/registryresolver/`: OCI registry detection, resolution, and artifact push utilities
  - `pkg/svc/state/`: Cluster state persistence for distributions that cannot introspect running configuration (Kind, K3d); stores spec as JSON in `~/.ksail/clusters/<name>/spec.json`
- `pkg/client/reconciler/`: Common base for GitOps reconciliation clients (Flux and ArgoCD)
- `pkg/di/`: Dependency injection for wiring components
- `pkg/k8s/`: Kubernetes helpers and templates
- `pkg/cli/`: CLI wiring, commands, and terminal UI components
- `pkg/envvar/`: Environment variable utilities
- `pkg/fsutil/`: Filesystem utilities (includes configmanager for configuration loading)
- `pkg/notify/`: CLI notifications and progress display utilities
- `pkg/runner/`: Cobra command execution helpers
- `pkg/timer/`: Command timing and performance tracking

## Active Technologies

- Go 1.26.0+ (see `go.mod`)
- Embedded Kubernetes tools (kubectl, helm, kind, k3d, vcluster, flux, argocd) as Go libraries
- Docker as the only external dependency
- Astro with Starlight for documentation (Node.js-based)

## Recent Changes

- Flattened package structure: moved from nested to flat organization in `pkg/`
- **Provider/Provisioner Architecture**: Separated infrastructure providers (Docker, Hetzner, Omni) from distribution provisioners (Vanilla, K3s, Talos, VCluster)
- **VCluster Support**: Added VCluster as the fourth supported distribution via VClusterProvisioner, enabling virtual Kubernetes clusters within Docker using the Vind driver
- **Hetzner Provider**: Added support for running Talos clusters on Hetzner Cloud
- **Omni Provider**: Added support for managing Talos clusters through the Sidero Omni SaaS API (`pkg/svc/provider/omni/`); requires `spec.cluster.omni.endpoint` and a service account key env var (default: `OMNI_SERVICE_ACCOUNT_KEY`, configurable via `spec.cluster.omni.serviceAccountKeyEnvVar`)
- **Registry Authentication**: Added support for external registries with username/password authentication
- **Default Registry Mirrors**: Enabled docker.io, ghcr.io, quay.io, and registry.k8s.io mirrors by default to avoid rate limits and improve CI/CD performance (`pkg/cli/setup/mirrorregistry/defaults.go`)
- **Distribution Naming**: Changed user-facing names from `Kind`/`K3d` to `Vanilla`/`K3s` to focus on the Kubernetes distribution rather than the underlying tool
- **VSCode Extension**: Added VSCode extension for managing KSail clusters from the editor with interactive wizards and MCP server support
- **AI Chat Integration**: Added `ksail chat` command powered by GitHub Copilot SDK for interactive cluster configuration and troubleshooting (`pkg/svc/chat/`)
  - **Chat Modes**: Three distinct modes - Agent (`</>`), Plan (`≡`), and Ask (`?`)
    - **Agent Mode**: Full tool execution with permission prompts for write operations
    - **Plan Mode**: No execution; AI describes steps without making changes
    - **Ask Mode**: Read-only investigation; write tools are blocked
  - **Mode Switching**: Press Tab to cycle between modes during chat sessions
  - **TUI Implementation**: Terminal UI in `pkg/cli/ui/chat/` with keyboard shortcuts and session management
  - **Reasoning Effort**: `--reasoning-effort` flag (low, medium, high) for supported models; `^E` keybind in TUI to change effort level
  - **Auto Model Selection**: Transparent model resolution with 10% discount; shows resolved model in picker and status bar (`auto → gpt-4o`)
  - **Quota Tracking**: Displays premium request usage (`300/300 reqs · 0% · resets Jan 2`); for unlimited entitlements shows only `∞ reqs`
  - **Infinite Sessions**: Background compaction enabled by default (BackgroundCompactionThreshold: 0.80, BufferExhaustionThreshold: 0.95) to manage long conversations
  - **Authentication**: Supports `KSAIL_COPILOT_TOKEN` and `COPILOT_TOKEN` environment variables; filters `GITHUB_TOKEN`/`GH_TOKEN` to avoid scope issues
  - **Enhanced Keybindings**: `^O` for model picker (lazy-loaded), `^E` for reasoning effort, `^H` for session history
- **MCP Server**: Implemented Model Context Protocol server to expose KSail as a tool for Claude and other AI assistants (`pkg/svc/mcp/`)
- **Cloud Provider KIND LoadBalancer Support**: Completed LoadBalancer support for Vanilla (Kind) × Docker using the Cloud Provider KIND controller (`pkg/svc/installer/cloudproviderkind/`); runs as an external Docker container named `ksail-cloud-provider-kind` and allocates IPs from the `kind` Docker network subnet; per-service containers use a `cpk-` prefix
- **MetalLB LoadBalancer Support**: Completed LoadBalancer support for Talos × Docker with MetalLB installer (`pkg/svc/installer/metallb/`), configured with default IP pool (172.18.255.200-172.18.255.250) and Layer 2 mode
- **String Building Optimization**: Replaced string concatenation with strings.Builder in tool generation (`pkg/toolgen/`) and chat UI (`pkg/cli/ui/chat/`) for better memory efficiency and reduced allocations; added Grow() pre-allocation for optimal performance (PR #2307)
- **Daily Workflow Optimizer**: Added `daily-workflow-optimizer` agentic workflow (`.github/workflows/daily-workflow-optimizer.md`) that systematically identifies and implements optimizations across all GitHub Actions workflows (both `.yaml`/`.yml` and agentic `.md`); operates in three phases: CI/CD research and planning, build-steps inference and guide creation, then targeted implementation
- **Hetzner Secret Race Fix**: Extracted shared Hetzner secret logic to `pkg/svc/installer/internal/hetzner/`; `EnsureSecret` now handles the TOCTOU race between concurrent `hcloud-ccm` and `hetzner-csi` installers via `createOrUpdateOnConflict` and `retry.RetryOnConflict` (PR #2488)
- **VCluster Transient Startup Retry**: Added `createWithRetry` in the VCluster provisioner (`pkg/svc/provisioner/cluster/vcluster/`) to automatically retry transient `CreateDocker` failures (e.g. exit status 22 / EINVAL on CI runners); performs up to 3 total attempts (initial attempt plus 2 retries) with a 5-second delay between attempts, cleaning up partial state between attempts. D-Bus recovery is handled by a separate `tryDBusRecovery` helper. Both helpers accept injectable function types (`createDockerFn`, `retryCleanupFn`, `dbusRecoverFn`) for unit-test isolation; tests use the `export_test.go` pattern with static error sentinels (PR #2530)
- **Detector Component Tests**: Added comprehensive unit tests for `pkg/svc/detector/` covering CNI, CSI, MetricsServer, LoadBalancer, CertManager, PolicyEngine, and GitOpsEngine detection (0% → 86.9% coverage); follows the `export_test.go` pattern (exports `ExportDetectCNI`, `ExportDetectCSI`, etc. and `NewReleaseMappingForTest`) for black-box testing of unexported methods; uses `fake.NewClientset()`, static error sentinels, `t.Parallel()`, and `require.NoError/Error` (PR #2562)
- **Agentic Workflows Threat Detection Instructions**: Added `.github/instructions/agentic-workflows.instructions.md` with globally applicable guidelines for reliable threat detection in agentic workflow jobs (intentionally no `applyTo` YAML frontmatter so Copilot can use these instructions across all contexts): diagnostic output before CLI invocation, robust error handling (`set -euo pipefail`, separate stdout/stderr capture, timeout protection), incremental tool restriction validation, and fallback mechanisms for security-critical detection failures (PR #2575)
- **VCluster Troubleshooting Docs**: Added a VCluster-specific section to `docs/src/content/docs/troubleshooting.md` covering transient startup failures (exit status 22 / EINVAL on CI runners) with cross-reference to auto-retry behavior (3 attempts, 5s delay), and kubectl connection errors immediately after VCluster creation; cross-references the VCluster Getting Started guide for detailed steps (PR #2599)
- **export_test.go Pattern Documented**: Codified the `export_test.go` convention in `.github/instructions/go.instructions.md` under Test Organization; the pattern exposes unexported symbols to `_test` packages without leaking them into production binaries — file uses the regular package name so Go only compiles it for test builds (PR #2603)
- **VCluster False-Positive Diff Fix**: Fixed `ksail cluster update` incorrectly reporting LoadBalancer and GitOps registry diffs for VCluster when nothing had changed. Two root causes resolved: (1) `ApplyGitOpsLocalRegistryDefault` now runs after component detection in the fallback path so the detected GitOps engine value correctly triggers the `localhost:5050` default; (2) the diff engine (`pkg/svc/diff/`) skips LoadBalancer comparison for VCluster (returns `LoadBalancerDefault` for both sides) since VCluster delegates LoadBalancer to the host cluster and KSail never installs or uninstalls anything — `spec.cluster.loadBalancer` has no effect on VCluster (PR #2549)
- **API Server Stability Check**: Added `waitForAPIServerStability` between Phase 1 (infrastructure components) and Phase 2 (GitOps engines) in `installComponentsInPhases` (`pkg/cli/setup/post_cni.go`); requires 3 consecutive successful API server health checks within a 2-minute timeout before GitOps engines begin installation; prevents Flux/ArgoCD from entering CrashLoopBackOff with `dial tcp 10.96.0.1:443: i/o timeout` errors caused by infrastructure components (MetalLB, Kyverno, cert-manager, etc.) temporarily destabilizing API server connectivity while registering webhooks and CRDs (PR #2601)
