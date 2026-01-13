# KSail - Kubernetes SDK for Local GitOps Development

KSail is a Go-based CLI application that provides a unified SDK for spinning up local Kubernetes clusters and managing workloads declaratively. It embeds common Kubernetes tools as Go libraries, requiring only Docker as an external dependency.

**ALWAYS reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.**

## Working Effectively

### Prerequisites and Dependencies

**CRITICAL**: Install Docker before using KSail:

- Docker is the only required external dependency for currently supported local clusters (the Docker provider)
- KSail embeds kubectl, helm, kind, k3d, flux, and argocd as Go libraries
- No separate installation of these tools is needed
- Cloud providers (e.g., Hetzner for Talos) are planned and will require cloud access/credentials

**Required for Documentation**:

```bash
# Ruby and Jekyll for documentation builds
# CI uses Ruby 3.3 (see .github/workflows/test-pages.yaml)
gem install --user-install bundler
export PATH="$(ruby -e 'print Gem.user_dir')/bin:$PATH"
cd /path/to/repo/docs
bundle config set --local path 'vendor/bundle'
bundle install
```

### Build Commands

**Main Application Build**:

```bash
cd /path/to/repo
go build -o ksail
# Takes a few seconds on first run for Go module downloads
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
export PATH="$(ruby -e 'print Gem.user_dir')/bin:$PATH"
bundle exec jekyll build
# Takes ~2 seconds. Documentation builds to _site/ directory
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
   export PATH="$(ruby -e 'print Gem.user_dir')/bin:$PATH"
   bundle exec jekyll build  # Must succeed
   ls _site/  # Should contain generated HTML files
   ```

## Common Tasks

### Project Structure

```text
/
├── main.go                 # Main entry point
├── pkg/                    # Core packages
│   ├── apis/               # API types and schemas
│   ├── cli/                # CLI wiring, UI, and Cobra commands
│   │   └── cmd/            # CLI command implementations
│   ├── client/             # Embedded tool clients (kubectl, helm, flux, etc.)
│   ├── di/                 # Dependency injection
│   ├── io/                 # I/O utilities
│   ├── k8s/                # Kubernetes helpers/templates
│   └── svc/                # Services (installers, managers, etc.)
├── docs/                   # Jekyll documentation source
│   ├── _site/              # Generated site (after jekyll build)
│   └── Gemfile             # Ruby dependencies for documentation
├── go.mod                  # Go module file
└── README.md               # Main repository documentation
```

### Key Configuration Files

- **go.mod**: Go module dependencies (includes embedded kubectl, helm, kind, k3d, flux, argocd)
- **Gemfile**: Jekyll documentation dependencies
- **\_config.yml**: Jekyll site configuration in docs/
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
```

### Init Command Options

Use the CLI help output as the source of truth:

```bash
ksail cluster init --help
# See also: docs/configuration/cli-flags/cluster/cluster-init.md
```

### Troubleshooting Build Issues

**"Go version mismatch"**:

```bash
go version  # Should match the version in go.mod (go 1.25.4)
# If not, install/update Go from https://go.dev/dl/
```

**Jekyll Permission Errors**:

```bash
bundle config set --local path 'vendor/bundle'
bundle install
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
export PATH="$(ruby -e 'print Gem.user_dir')/bin:$PATH"
bundle exec jekyll build               # Verify docs build
bundle exec jekyll serve --host 0.0.0.0  # Test locally (if needed)
```

### Important Notes

- The project uses **Go 1.25.4+** (see `go.mod`)
- All Kubernetes tools are embedded as Go libraries - only Docker is required externally
- Unit tests run quickly and should generally pass
- System tests in CI cover extensive scenarios with multiple tool combinations
- Documentation is built with Jekyll and uses the "just-the-docs" theme
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
| ------------ | ----- | --------------- | ---------------------------- |
| `Vanilla`    | Kind  | Docker          | Standard upstream Kubernetes |
| `K3s`        | K3d   | Docker          | Lightweight K3s in Docker    |
| `Talos`      | Talos | Docker, Hetzner | Immutable Talos Linux        |

**Key Packages:**

- `pkg/apis/`: API types, schemas, and enums (`pkg/apis/cluster/v1alpha1/enums.go` defines Distribution values)
- `pkg/client/`: Embedded tool clients (kubectl, helm, kind, k3d, flux, argocd)
- `pkg/svc/`: Services including installers, providers, and provisioners
- `pkg/di/`: Dependency injection for wiring components

## Active Technologies

- Go 1.25.4+ (see `go.mod`)
- Embedded Kubernetes tools (kubectl, helm, kind, k3d, flux, argocd) as Go libraries
- Docker as the only external dependency
- Jekyll for documentation (Ruby-based)

## Recent Changes

- Flattened package structure: moved from nested to flat organization in `pkg/`
- **Provider/Provisioner Architecture**: Separated infrastructure providers (Docker, Hetzner) from distribution provisioners (Vanilla, K3s, Talos)
- **Hetzner Provider**: Added support for running Talos clusters on Hetzner Cloud
- **Registry Authentication**: Added support for external registries with username/password authentication
- **Distribution Naming**: Changed user-facing names from `Kind`/`K3d` to `Vanilla`/`K3s` to focus on the Kubernetes distribution rather than the underlying tool
