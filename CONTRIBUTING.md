# Contributing

This project accepts contributions in the form of [**bug reports**](https://github.com/devantler-tech/ksail/issues/new/choose), [**feature requests**](https://github.com/devantler-tech/ksail/issues/new/choose), and **pull requests** (see below). If you are looking to contribute code, please follow the guidelines outlined in this document.

## Getting Started

To get started with contributing to ksail, you'll need to set up your development environment, and understand the codebase, the CI setup and its requirements.

To understand the codebase it is recommended to read the `.github/copilot-instructions.md` file, which provides an overview of the project structure and key components. You can also use GitHub Copilot to assist you in navigating the codebase and understanding its functionality.

### Code Documentation

For detailed package and API documentation, refer to the Go documentation at [pkg.go.dev/github.com/devantler-tech/ksail/v5](https://pkg.go.dev/github.com/devantler-tech/ksail/v5). This provides comprehensive documentation for all exported packages, types, functions, and methods.

### Prerequisites

Before you begin, ensure you have the following installed:

- [Go (v1.25.4+)](https://go.dev/doc/install)
- [mockery](https://vektra.github.io/mockery/v3.5/installation/)
- [golangci-lint](https://golangci-lint.run/docs/welcome/install/)
- [mega-linter](https://megalinter.io/latest/mega-linter-runner/#installation)
- [Docker](https://www.docker.com/get-started/)

For building documentation:

- [Ruby (v3.3+)](https://www.ruby-lang.org/en/documentation/installation/) (matches CI)
- [Bundler](https://bundler.io/)

### Lint

KSail uses mega-linter with the go flavor, and uses a strict configuration to ensure code quality and consistency. You can run the linter with the following command:

```sh
# working-directory: ./
mega-linter-runner -f go
```

The same configuration is used in CI, so you can expect the same linting behavior in your local environment as in the CI pipeline.

### Build

```sh
# working-directory: ./
go build ./...
```

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

### Documentation

The project documentation is built using [Jekyll](https://jekyllrb.com/) with the [Just the Docs](https://just-the-docs.com/) theme and is located in the `docs/` directory.

#### Building the documentation

```sh
# working-directory: ./docs

# Install bundler (first time only)
gem install --user-install bundler
export PATH="$(ruby -e 'print Gem.user_dir')/bin:$PATH"

# Install dependencies (first time only or when Gemfile changes)
bundle config set --local path 'vendor/bundle'
bundle install

# Build the site
bundle exec jekyll build

# Serve the site locally with live reload (optional)
bundle exec jekyll serve
# Visit http://localhost:4000 to view the site
```

The built site will be available in `docs/_site/`. Note that `docs/_site/`, `docs/vendor/`, and `docs/.bundle/` are excluded from git via `.gitignore`.

## Project Structure

The repository is organized around the top-level CLI entry point (`main.go`) and the public packages in `pkg/`.

- **main.go** - CLI entry point
- **pkg/cli/cmd/** - CLI command implementations
- **pkg/** - Public packages (importable by external projects)

### Key Packages in pkg/

- **apis/** - API types, schemas, and enums (distribution/provider values)
- **client/** - Embedded tool clients (kubectl, helm, kind, k3d, flux, argocd)
- **svc/provider/** - Infrastructure providers (e.g., `docker.Provider` for running nodes as containers)
- **svc/provisioner/** - Distribution provisioners (Vanilla, K3s, Talos)
- **svc/installer/** - Component installers (CNI, CSI, metrics-server, etc.)
- **di/** - Dependency injection for wiring components

### Architecture: Providers vs Provisioners

KSail separates infrastructure management from distribution configuration:

- **Providers** manage the infrastructure lifecycle (start/stop containers)
- **Provisioners** configure and manage Kubernetes distributions

| Distribution | Provisioner        | Tool  | Description                  |
|--------------|--------------------|-------|------------------------------|
| `Vanilla`    | VanillaProvisioner | Kind  | Standard upstream Kubernetes |
| `K3s`        | K3sProvisioner     | K3d   | Lightweight K3s in Docker    |
| `Talos`      | TalosProvisioner   | Talos | Immutable Talos Linux        |

This project strives to be fully open-source friendly, and as such, all core functionality is implemented in the `pkg/` directory, and the `internal/` directory is not used. This allows external projects to import and use any part of the codebase.

For detailed package and API documentation, refer to [pkg.go.dev/github.com/devantler-tech/ksail/v5](https://pkg.go.dev/github.com/devantler-tech/ksail/v5).

## CI

### GitHub Workflows

#### Unit Tests

```sh
# working-directory: ./
go test ./...
```

#### System Tests

System tests are configured in a GitHub Actions workflow file located at `.github/workflows/ci.yaml`. These test e2e scenarios for various providers and configurations. You are unable to run these tests locally, but they are required in CI, so breaking changes will result in failed checks.

## CD

### Release Process

The release process for KSail is fully-automated and relies on semantic versioning. When PRs are merged into the main branch, a new version is automatically released based on the name of the PR. The following conventions are used:

- **fix:** A patch release (e.g. 1.0.1) is triggered.
- **feat:** A minor release (e.g. 1.1.0) is triggered.
- **BREAKING CHANGE:** A major release (e.g. 2.0.0) is triggered.

The changelog is auto-generated by go-releaser, so contributors just have to ensure their PRs are well-named and descriptive, such that the intent of the changes is clear.
