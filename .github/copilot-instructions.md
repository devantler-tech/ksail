# KSail - Kubernetes SDK for Local GitOps Development

KSail is a .NET-based CLI application that provides a unified SDK for spinning up local Kubernetes clusters and managing workloads declaratively. It streamlines Kubernetes development by providing a single interface over multiple container engines (Docker, Podman), distributions (Kind, K3d), and deployment tools (Kubectl, Flux).

**ALWAYS reference these instructions first and fallback to search or bash commands only when you encounter unexpected information that does not match the info here.**

## Working Effectively

### Prerequisites and Dependencies

**CRITICAL**: Install .NET 9.0 SDK before building anything:

```bash
curl -sSL https://dot.net/v1/dotnet-install.sh | bash /dev/stdin --version 9.0.102
export PATH="$HOME/.dotnet:$PATH"
```

**Required for Documentation**:

```bash
# Ruby and Jekyll for documentation builds
gem install --user-install bundler
export PATH="$HOME/.local/share/gem/ruby/3.2.0/bin:$PATH"
cd /path/to/repo
bundle config set --local path 'vendor/bundle'
bundle install
```

**External Kubernetes Tools** (optional but enable full functionality):

- Docker or Podman (container engine)
- kubectl, helm, k9s (Kubernetes tools)
- kind, k3d (local cluster distributions)
- flux, argocd (GitOps tools)
- cilium (CNI)
- age, sops (secret management)
- kubeconform (validation)

### Build Commands

**Main Application Build**:

```bash
cd src/KSail
export PATH="$HOME/.dotnet:$PATH"
dotnet build
# Takes ~40 seconds: 30s NuGet restore + 5s compilation. NEVER CANCEL. Set timeout to 90+ seconds.
```

**Run Unit Tests**:

```bash
cd /path/to/repo
export PATH="$HOME/.dotnet:$PATH"
dotnet test tests/KSail.Tests.Unit/
# Takes ~18 seconds. NEVER CANCEL. Set timeout to 60+ seconds.
# Note: Some tests may fail if external Kubernetes tools are missing - this is expected
```

**Build Documentation**:

```bash
cd /path/to/repo
export PATH="$HOME/.local/share/gem/ruby/3.2.0/bin:$PATH"
bundle exec jekyll build
# Takes ~2 seconds. Documentation builds to _site/ directory
```

### Running the Application

**CLI Usage**:

```bash
cd src/KSail
export PATH="$HOME/.dotnet:$PATH"
dotnet run -- --help                    # Show all commands
dotnet run -- init --help               # Show init options
dotnet run -- init                      # Initialize default project
dotnet run -- up                        # Create cluster (requires external tools)
dotnet run -- status                    # Show cluster status
dotnet run -- down                      # Destroy cluster
```

**Build and Use Executable**:

```bash
cd src/KSail
export PATH="$HOME/.dotnet:$PATH"
dotnet build
./bin/Debug/net9.0/linux-x64/ksail --help
```

## Validation

**ALWAYS validate changes by running through complete scenarios:**

1. **Build Validation**:

   ```bash
   cd src/KSail
   export PATH="$HOME/.dotnet:$PATH"
   dotnet build  # Must succeed
   ```

2. **Test Validation**:

   ```bash
   cd /path/to/repo
   export PATH="$HOME/.dotnet:$PATH"
   dotnet test tests/KSail.Tests.Unit/  # Most tests should pass
   ```

3. **CLI Functional Validation**:

   ```bash
   cd /tmp && mkdir test-ksail && cd test-ksail
   export PATH="$HOME/.dotnet:$PATH"
   /path/to/repo/src/KSail/bin/Debug/net9.0/linux-x64/ksail init
   # Should create: ksail.yaml, kind.yaml, k8s/kustomization.yaml
   ```

4. **Documentation Validation**:

   ```bash
   cd /path/to/repo
   export PATH="$HOME/.local/share/gem/ruby/3.2.0/bin:$PATH"
   bundle exec jekyll build  # Must succeed
   ls _site/  # Should contain generated HTML files
   ```

**NEVER CANCEL BUILDS**: .NET builds can take 40+ seconds on first run. Always set timeouts to 90+ seconds for builds and 60+ seconds for tests.

## Common Tasks

### Project Structure

```text
/
├── src/
│   ├── KSail/              # Main CLI application (builds to 'ksail' executable)
│   ├── KSail.Models/       # Data models and configuration
│   ├── KSail.Generator/    # Code generation utilities  
│   └── KSail.Docs/         # Documentation generation
├── tests/
│   ├── KSail.Tests.Unit/   # Unit tests for main application
│   └── *Tests.Unit/        # Unit tests for each component
├── docs/                   # Jekyll documentation source
├── _site/                  # Generated documentation (after jekyll build)
├── KSail.slnx             # .NET solution file (requires .NET 9.0)
├── Gemfile                # Ruby dependencies for documentation
└── README.md              # Main repository documentation
```

### Key Configuration Files

- **KSail.slnx**: .NET solution file (XML format, requires .NET 9.0 SDK)
- **src/KSail/KSail.csproj**: Main application project targeting .NET 9.0
- **Gemfile**: Jekyll documentation dependencies
- **_config.yml**: Jekyll site configuration
- **.github/workflows/test.yaml**: CI pipeline with comprehensive system tests

### CLI Commands Reference

All CLI commands require external Kubernetes tools for full functionality:

```bash
ksail init [options]           # Initialize new KSail project
ksail up                       # Create and start cluster
ksail down                     # Destroy cluster and resources
ksail start                    # Start existing cluster
ksail stop                     # Stop running cluster
ksail update                   # Apply configuration changes
ksail status                   # Show cluster status
ksail list [--all]             # List clusters
ksail connect                  # Connect to cluster with K9s
ksail validate                 # Validate project files
ksail gen <type> <resource>    # Generate resources
ksail secrets <command>        # Manage secrets (requires SOPS)
```

### Init Command Options

```bash
ksail init \
  --container-engine <Docker|Podman> \
  --distribution <Kind|K3d> \
  --deployment-tool <Kubectl|Flux> \
  --cni <Default|Cilium|None> \
  --csi <Default|LocalPathProvisioner|None> \
  --ingress-controller <Default|Traefik|None> \
  --gateway-controller <Default|None> \
  --metrics-server <True|False> \
  --secret-manager <None|SOPS> \
  --mirror-registries <True|False> \
  --editor <Nano|Vim>
```

### Troubleshooting Build Issues

**"NETSDK1045: The current .NET SDK does not support targeting .NET 9.0"**:

```bash
curl -sSL https://dot.net/v1/dotnet-install.sh | bash /dev/stdin --version 9.0.102
export PATH="$HOME/.dotnet:$PATH"
dotnet --version  # Should show 9.0.102 or later
```

**"MSB4068: The element <Solution> is unrecognized"**:

- KSail.slnx requires .NET 9.0 SDK
- Build individual projects with `dotnet build src/KSail/` instead

**Jekyll Permission Errors**:

```bash
bundle config set --local path 'vendor/bundle'
bundle install
```

**Missing External Tools**:

- The CLI shows warnings for missing tools (age, flux, k3d, etc.)
- Install tools as needed for desired functionality
- Most basic operations work without external tools

### Making Changes

**Always build and test after making changes**:

```bash
cd src/KSail
export PATH="$HOME/.dotnet:$PATH"
dotnet build                           # Verify builds
dotnet test ../../tests/KSail.Tests.Unit/  # Run unit tests
dotnet run -- init                    # Test CLI functionality
```

**For documentation changes**:

```bash
export PATH="$HOME/.local/share/gem/ruby/3.2.0/bin:$PATH"
bundle exec jekyll build               # Verify docs build
bundle exec jekyll serve --host 0.0.0.0  # Test locally (if needed)
```

### Important Notes

- The project targets **.NET 9.0** but many environments have .NET 8.0 by default
- Unit tests may fail if external Kubernetes tools are missing - this is expected behavior
- The CLI warns about missing external tools but basic functionality works without them
- System tests in CI cover extensive scenarios with multiple tool combinations
- Documentation is built with Jekyll and uses the "just-the-docs" theme
- Build times: ~40s for initial build, ~18s for tests, ~2s for docs
- **NEVER CANCEL** long-running builds - they need time to download packages and compile
