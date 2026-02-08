# KSail - Kubernetes SDK for Local GitOps Development

KSail is a Go-based CLI application that provides a unified SDK for spinning up local Kubernetes clusters and managing workloads declaratively. It embeds common Kubernetes tools as Go libraries, requiring only Docker as an external dependency.

**ALWAYS reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.**

## Working Effectively

### Prerequisites and Dependencies

**CRITICAL**: Install Docker before using KSail:

- Docker is the only required external dependency for local clusters (the Docker provider)
- KSail embeds kubectl, helm, kind, k3d, flux, and argocd as Go libraries
- No separate installation of these tools is needed
- The Hetzner provider is supported for Talos clusters and requires cloud access/credentials (e.g., `HCLOUD_TOKEN`)

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

# For production-optimized builds (matches release artifacts):
go build -ldflags="-s -w" -o ksail
# Strips debug symbols, reduces binary size by ~28% (302MB → 217MB)
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
./ksail cluster create            # Create cluster (requires Docker)
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
├── pkg/                    # Core packages
│   ├── ai/                 # AI-related utilities
│   │   └── toolgen/        # Tool generation for AI assistants
│   ├── apis/               # API types and schemas
│   ├── cli/                # CLI wiring, UI, and Cobra commands
│   │   ├── annotations/    # Command annotation constants
│   │   ├── cmd/            # CLI command implementations
│   │   ├── helpers/        # CLI helper utilities
│   │   ├── lifecycle/      # Cluster lifecycle orchestration
│   │   ├── setup/          # Component setup (CNI, mirror registries, etc.)
│   │   └── ui/             # Terminal UI (ASCII art, chat TUI, confirmations)
│   ├── client/             # Embedded tool clients (kubectl, helm, flux, etc.)
│   ├── di/                 # Dependency injection
│   ├── io/                 # I/O utilities
│   ├── k8s/                # Kubernetes helpers/templates
│   ├── utils/              # General utilities
│   └── svc/                # Services (installers, managers, etc.)
│       ├── chat/           # AI chat integration (GitHub Copilot SDK)
│       ├── installer/      # Component installers (CNI, CSI, metrics-server, etc.)
│       ├── mcp/            # Model Context Protocol server
│       ├── provider/       # Infrastructure providers (docker, hetzner)
│       ├── provisioner/    # Distribution provisioners (Vanilla, K3s, Talos)
│       ├── image/          # Container image export/import services
│       └── reconciler/     # Common base for GitOps reconciliation clients
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

- **go.mod**: Go module dependencies (includes embedded kubectl, helm, kind, k3d, flux, argocd)
- **package.json**: Node.js dependencies for Astro documentation
- **.github/workflows/\*.yaml**: CI/CD pipelines

### CLI Commands Reference

All CLI commands only require Docker to be installed:

```bash
ksail cluster init [options]           # Initialize new KSail project
ksail cluster create                   # Create and start cluster
ksail cluster delete                   # Destroy cluster and resources
ksail cluster start                    # Start existing cluster
ksail cluster stop                     # Stop running cluster
ksail cluster info                     # Show cluster status
ksail cluster list [--all]             # List clusters
ksail cluster connect                  # Connect to cluster with K9s
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
```

### Troubleshooting Build Issues

**"Go version mismatch"**:

```bash
go version  # Should match the version in go.mod (go 1.25.4)
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

- The project uses **Go 1.25.4+** (see `go.mod`)
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
- **Provisioners** (`pkg/svc/provisioner/`) configure and manage Kubernetes distributions
  - `KindClusterProvisioner` (`pkg/svc/provisioner/cluster/kind/`): Uses Kind SDK for standard upstream Kubernetes
  - `K3dClusterProvisioner` (`pkg/svc/provisioner/cluster/k3d/`): Uses K3d via Cobra/SDK for lightweight K3s clusters
  - `TalosProvisioner` (`pkg/svc/provisioner/cluster/talos/`): Uses Talos SDK for immutable Talos Linux clusters

**Distribution Names (user-facing):**

| Distribution | Tool  | Provider        | Description                  |
|--------------|-------|-----------------|------------------------------|
| `Vanilla`    | Kind  | Docker          | Standard upstream Kubernetes |
| `K3s`        | K3d   | Docker          | Lightweight K3s in Docker    |
| `Talos`      | Talos | Docker, Hetzner | Immutable Talos Linux        |

**Key Packages:**

- `pkg/ai/toolgen/`: AI tool generation utilities for integrating with AI assistants
- `pkg/apis/`: API types, schemas, and enums (`pkg/apis/cluster/v1alpha1/enums.go` defines Distribution values)
- `pkg/client/`: Embedded tool clients (kubectl, helm, kind, k3d, flux, argocd)
- `pkg/svc/`: Services including installers, providers, and provisioners
  - `pkg/svc/chat/`: AI chat integration using GitHub Copilot SDK with embedded CLI documentation
  - `pkg/svc/installer/`: Component installers (CNI, CSI, metrics-server, etc.)
  - `pkg/svc/mcp/`: Model Context Protocol server for Claude and other AI assistants
  - `pkg/svc/provider/`: Infrastructure providers (docker, hetzner)
  - `pkg/svc/provisioner/`: Distribution provisioners (Vanilla, K3s, Talos)
  - `pkg/svc/image/`: Container image export/import services for Vanilla and K3s distributions
  - `pkg/svc/reconciler/`: Common base for GitOps reconciliation clients (Flux and ArgoCD)
- `pkg/di/`: Dependency injection for wiring components
- `pkg/utils/`: General utility functions

## Active Technologies

- Go 1.25.4+ (see `go.mod`)
- Embedded Kubernetes tools (kubectl, helm, kind, k3d, flux, argocd) as Go libraries
- Docker as the only external dependency
- Astro with Starlight for documentation (Node.js-based)

## Recent Changes

- Flattened package structure: moved from nested to flat organization in `pkg/`
- **Provider/Provisioner Architecture**: Separated infrastructure providers (Docker, Hetzner) from distribution provisioners (Vanilla, K3s, Talos)
- **Hetzner Provider**: Added support for running Talos clusters on Hetzner Cloud
- **Registry Authentication**: Added support for external registries with username/password authentication
- **Default Registry Mirrors**: Enabled docker.io and ghcr.io mirrors by default to avoid rate limits and improve CI/CD performance (`pkg/cli/setup/mirrorregistry/defaults.go`)
- **Distribution Naming**: Changed user-facing names from `Kind`/`K3d` to `Vanilla`/`K3s` to focus on the Kubernetes distribution rather than the underlying tool
- **VSCode Extension**: Added VSCode extension for managing KSail clusters from the editor with interactive wizards and MCP server support
- **AI Chat Integration**: Added `ksail chat` command powered by GitHub Copilot SDK for interactive cluster configuration and troubleshooting (`pkg/svc/chat/`)
- **MCP Server**: Implemented Model Context Protocol server to expose KSail as a tool for Claude and other AI assistants (`pkg/svc/mcp/`)
