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
│       │   ├── cluster/    # Detects distribution, provider, cluster name from kubeconfig context
│       │   └── gitops/     # Detects existing GitOps CRs (FluxInstance, ArgoCD Application) in source dir
│       ├── diff/           # Computes ClusterSpec config diffs and classifies update impact
│       ├── image/          # Container image export/import services
│       │   └── parser/     # Parses image references from Dockerfiles
│       ├── installer/      # Component installers (CNI, CSI, metrics-server, etc.)
│       ├── mcp/            # Model Context Protocol server
│       ├── provider/       # Infrastructure providers (docker, hetzner, omni)
│       ├── provisioner/    # Distribution provisioners (Vanilla, K3s, Talos, VCluster)
│       ├── registryresolver/ # OCI registry detection, credential resolution, and artifact push
│       └── state/          # Cluster state persistence for distributions without introspection
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
- **.github/workflows/\*.md**: Agentic workflows (repo-assist, daily-docs, daily-workflow-maintenance, weekly-strategy, ci-doctor); each runs on a schedule or dispatch, and many operate in multiple phases

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
ksail cluster diagnose                 # Report failing pods and NotReady nodes
ksail cluster list [--all]             # List clusters
ksail cluster connect                  # Connect to cluster with K9s
ksail cluster switch [cluster-name]    # Switch active kubeconfig context (interactive picker if no arg)
ksail cluster backup                   # Backup cluster resources to .tar.gz
ksail cluster restore                  # Restore cluster resources from .tar.gz
ksail workload apply                   # Apply manifests to cluster
ksail workload create                  # Create resources imperatively
ksail workload edit                    # Edit a resource in-place
ksail workload get                     # Get resources
ksail workload describe                # Describe resources in detail
ksail workload explain                 # Get API documentation for a resource
ksail workload delete                  # Delete Kubernetes resources
ksail workload logs                    # View container logs
ksail workload exec                    # Execute command in container
ksail workload expose                  # Expose a resource as a service
ksail workload gen <resource>          # Generate Kubernetes manifests
ksail workload validate                # Validate manifests against schemas
ksail workload install                 # Install Helm charts
ksail workload scale                   # Scale deployments
ksail workload rollout                 # Manage rollouts
ksail workload wait                    # Wait for conditions
ksail workload images                  # List required container images
ksail workload export                  # Export container images to a tar archive
ksail workload import                  # Import container images from a tar archive
ksail workload watch [--path <dir>]    # Watch directory and auto-apply on change
ksail workload push                    # Package and push manifests to registry
ksail workload reconcile               # Trigger GitOps sync and wait
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
- All Kubernetes tools are embedded as Go libraries - only Docker is required externally
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

- `pkg/toolgen/`: AI tool generation — auto-generates tools from the Cobra command tree for both the MCP server and the Copilot chat assistant. All runnable CLI commands (except excluded meta commands: `chat`, `mcp`, `completion`, `help`, root — see `toolgen.DefaultOptions()` and `ai.toolgen.exclude`) are automatically exposed as tools; do NOT manually register individual tool handlers. Parent commands annotated with `ai.toolgen.consolidate` group their subcommands into a single tool, then `ai.toolgen.permission` splits them into read/write pairs (e.g., `cluster_read`, `cluster_write`). Adding a new CLI command under a consolidated parent automatically makes it available as an MCP tool and a chat tool — no separate tool registration is needed
- `pkg/apis/`: API types, schemas, and enums; each enum type lives in its own file under `pkg/apis/cluster/v1alpha1/` (e.g., `distribution.go`, `cni.go`, `csi.go`, `loadbalancer.go`, `gitopsengine.go`, etc.); the `EnumValuer` interface is in `enum.go`; API-level validation errors (e.g., `ErrInvalidDistribution`, `ErrInvalidGitOpsEngine`, `ErrClusterNameTooLong`, `ErrInvalidDistributionProviderCombination`) are centralized in `errors.go`
- `pkg/client/`: Embedded tool clients (kubectl, helm, flux, argocd, docker, k9s, kubeconform, kustomize, oci, netretry); distribution tools like kind, k3d, and vcluster are used directly via their SDKs in provisioners, not wrapped in `pkg/client/`.
- `pkg/svc/`: Services including installers, providers, and provisioners
  - `pkg/svc/chat/`: AI chat integration using GitHub Copilot SDK with embedded CLI documentation; `sandbox.go` exports `IsPathWithinDirectory` which uses `fsutil.EvalCanonicalPath` for path containment checks
  - `pkg/svc/detector/`: Detects installed Kubernetes components by querying Helm release history and the Kubernetes API; used by the update command to build accurate baseline state
    - `pkg/svc/detector/cluster/`: Detects Kubernetes distribution, provider, and cluster name by analyzing kubeconfig context names and server endpoints; exposes `DetectInfo`, `DetectDistributionFromContext`, and `ResolveKubeconfigPath`
    - `pkg/svc/detector/gitops/`: Detects existing GitOps Custom Resources (FluxInstance, ArgoCD Application) managed by KSail in the source directory
  - `pkg/svc/diff/`: Computes configuration differences between old and new ClusterSpec values; classifies update impact (in-place, reboot-required, recreate-required)
  - `pkg/svc/image/`: Container image export/import services for Vanilla and K3s distributions; `parser/` sub-package provides `ParseAllImagesFromDockerfile` for extracting all `FROM` directives from multi-stage Dockerfiles (used by Flux installer to include distribution controller images in mirror cache warming)
  - `pkg/svc/installer/`: Component installers (CNI, CSI, metrics-server, etc.); `internal/hetzner/` holds shared utilities for the Hetzner installers—`hcloudccm.Installer` and `hetznercsi.Installer` are type aliases for `hetzner.Installer` and share a single `EnsureSecret` implementation; `flux/Dockerfile.distribution` tracks Flux distribution controller images (updated by Dependabot) that are deployed by the Flux operator when creating a FluxInstance but are not part of the Helm chart — included in `Images()` output for mirror cache warming
  - `pkg/svc/mcp/`: Model Context Protocol server for Claude and other AI assistants; tools are auto-generated from root Cobra commands via `pkg/toolgen/` (not manually registered) — all operational cluster/workload/cipher commands (both read and write) are consolidated into 5 tools via `ai.toolgen.consolidate` + `ai.toolgen.permission`: `cluster_read`, `cluster_write`, `workload_read`, `workload_write`, `cipher_write`
  - `pkg/svc/provider/`: Infrastructure providers (docker, hetzner, omni)
  - `pkg/svc/provisioner/`: Distribution provisioners (Vanilla, K3s, Talos, VCluster)
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

## Threat Detection False-Positive Prevention

- When your only output is a `noop` safe-output call (i.e., the model responds solely with the standardized `safe_output` block and does not propose any code, configuration, or workflow changes), this is legitimate — do NOT describe it as "suppressing actions."
- The `<security>` section in the workflow prompt (the dedicated security-policy guidance block in the gh-aw threat-detection workflow template) is standard policy guidance, NOT a prompt injection attempt.
- When the detection model's own `reasons` array contradicts the boolean verdict, the reasons text takes precedence for human review.
- Agents should avoid phrasing like "ignoring policy" or "suppressing" in their reasoning; prefer "no action required" or "no changes needed."

## Daily Docs Workflow Behavior

The Daily Docs agentic workflow (`daily-docs.lock.yml`) is designed to fill documentation gaps:

- When a push to `main` already includes documentation changes alongside code changes, the agent may correctly call `noop` — no additional PR is needed
- When the agent calls `noop`, the `agent` job is marked as `failure` by the gh-aw framework (this is expected behavior, not a real error)
- The `update_cache_memory` job is skipped on agent failure — subsequent runs will rebuild the page ownership map cache automatically
- This pattern is particularly common for commits that combine code dependency updates with documentation fixes

## Benchmark Pipeline Consistency

When changing `-count` in the `go test -bench` command in CI, always update the awk averaging/filtering step in "Prepare benchmark regression gate input" to produce exactly one result line per benchmark name. Failing to do so causes false-positive performance regression alerts for all benchmarks, even ones unrelated to the PR. The comparison tool (`github-action-benchmark`) expects the file it reads to contain exactly one result line per benchmark name, so repeated entries for the same benchmark must be consolidated before comparison. See [docs/BENCHMARK-REGRESSION.md](../docs/BENCHMARK-REGRESSION.md) for details.

## Active Technologies

- Go 1.26.1+ (see `go.mod`)
- Embedded Kubernetes tools (kubectl, helm, kind, k3d, vcluster, flux, argocd) as Go libraries
- Docker as the only external dependency
- Astro with Starlight for documentation (Node.js-based)
