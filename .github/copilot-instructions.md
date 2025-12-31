# KSail - Kubernetes SDK for Local GitOps Development

KSail is a Go-based CLI application that provides a unified SDK for spinning up local Kubernetes clusters and managing workloads declaratively. It embeds common Kubernetes tools as Go libraries, requiring only Docker as an external dependency.

**ALWAYS reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.**

## Working Effectively

### Prerequisites and Dependencies

**CRITICAL**: Install Docker before using KSail:

- Docker is the only required external dependency
- KSail embeds kubectl, helm, kind, k3d, flux, and argocd as Go libraries
- No separate installation of these tools is needed

**Required for Documentation**:

```bash
# Ruby and Jekyll for documentation builds
gem install --user-install bundler
export PATH="$HOME/.local/share/gem/ruby/3.2.0/bin:$PATH"
cd /path/to/repo
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
export PATH="$HOME/.local/share/gem/ruby/3.2.0/bin:$PATH"
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
   export PATH="$HOME/.local/share/gem/ruby/3.2.0/bin:$PATH"
   bundle exec jekyll build  # Must succeed
   ls _site/  # Should contain generated HTML files
   ```

## Common Tasks

### Project Structure

```text
/
├── cmd/                    # CLI command implementations
├── pkg/                    # Core packages
│   ├── apis/               # API types and schemas
│   ├── client/             # Embedded tool clients (kubectl, helm, flux, etc.)
│   ├── di/                 # Dependency injection
│   ├── io/                 # I/O utilities
│   └── svc/                # Services (installers, managers, etc.)
├── docs/                   # Jekyll documentation source
├── _site/                  # Generated documentation (after jekyll build)
├── go.mod                  # Go module file
├── main.go                 # Main entry point
├── Gemfile                 # Ruby dependencies for documentation
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

```bash
ksail cluster init \
  --distribution <Kind|K3d> \
  --cni <Default|Cilium|None> \
  --csi <Default|LocalPathStorage|None> \
  --metrics-server <Enabled|Disabled> \
  --cert-manager <Enabled|Disabled> \
  --local-registry <Enabled|Disabled> \
  --local-registry-port <port> \
  --gitops-engine <None|Flux|ArgoCD> \
  --mirror-registry <host>=<upstream>
```

### Troubleshooting Build Issues

**"Go version mismatch"**:

```bash
go version  # Should show Go 1.21 or later
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
export PATH="$HOME/.local/share/gem/ruby/3.2.0/bin:$PATH"
bundle exec jekyll build               # Verify docs build
bundle exec jekyll serve --host 0.0.0.0  # Test locally (if needed)
```

### Important Notes

- The project uses **Go 1.23.9+** (currently requires Go 1.21 or later)
- All Kubernetes tools are embedded as Go libraries - only Docker is required externally
- Unit tests run quickly and should generally pass
- System tests in CI cover extensive scenarios with multiple tool combinations
- Documentation is built with Jekyll and uses the "just-the-docs" theme
- Build times: ~2-3 minutes for initial build (downloads dependencies), faster on subsequent builds
- **NEVER CANCEL** long-running builds - they need time to download packages and compile

## Active Technologies

- Go 1.23.9+ (currently using Go 1.25.4)
- Embedded Kubernetes tools (kubectl, helm, kind, k3d, flux, argocd) as Go libraries
- Docker as the only external dependency
- Jekyll for documentation (Ruby-based)

## Recent Changes

- Flattened package structure: moved from nested to flat organization in `pkg/`
