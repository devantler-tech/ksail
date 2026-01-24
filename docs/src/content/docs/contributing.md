---
title: Contributing
description: Guidelines for contributing to KSail - development environment setup, testing, and submitting pull requests
---

This project accepts contributions in the form of [**bug reports**](https://github.com/devantler-tech/ksail/issues/new/choose), [**feature requests**](https://github.com/devantler-tech/ksail/issues/new/choose), and **pull requests**. If you are looking to contribute code, please follow the guidelines outlined below.

For the complete contributing guide with detailed instructions, see [CONTRIBUTING.md](https://github.com/devantler-tech/ksail/blob/main/CONTRIBUTING.md) in the repository.

## Quick Start

### Prerequisites

**Runtime Requirements:**

- [Docker](https://www.docker.com/get-started/) — The only required external dependency for running KSail

**Development Requirements:**

- [Go (v1.25.4+)](https://go.dev/doc/install)
- [mockery (v3.5+)](https://vektra.github.io/mockery/v3.5/installation/)
- [golangci-lint](https://golangci-lint.run/docs/welcome/install/)
- [mega-linter](https://megalinter.io/latest/mega-linter-runner/#installation)
- [Node.js (v22+)](https://nodejs.org/en/download/) — Required for building documentation

### Development Workflow

``````bash
# Clone the repository
git clone https://github.com/devantler-tech/ksail.git
cd ksail

# Build the application
go build -o ksail

# Run tests
go test ./...

# Lint code
mega-linter-runner -f go

# Build documentation
cd docs
npm ci
npm run build
``````

### Project Structure

The repository is organized around the top-level CLI entry point (`main.go`) and the public packages in `pkg/`:

- **main.go** - CLI entry point
- **pkg/cli/cmd/** - CLI command implementations
- **pkg/apis/** - API types, schemas, and enums
- **pkg/client/** - Embedded tool clients (kubectl, helm, kind, k3d, flux, argocd)
- **pkg/svc/** - Services (providers, provisioners, installers, reconcilers)
- **pkg/di/** - Dependency injection

For detailed architecture information, see [.github/copilot-instructions.md](https://github.com/devantler-tech/ksail/blob/main/.github/copilot-instructions.md).

## Submitting Changes

1. **Fork** the repository
2. **Create** a feature branch (`git checkout -b feature/amazing-feature`)
3. **Make** your changes following the coding standards
4. **Test** your changes (`go test ./...`)
5. **Lint** your code (`mega-linter-runner -f go`)
6. **Commit** your changes using [Conventional Commits](https://www.conventionalcommits.org/)
7. **Push** to your branch (`git push origin feature/amazing-feature`)
8. **Open** a Pull Request

### Commit Message Format

We use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages:

- `feat:` New feature
- `fix:` Bug fix
- `docs:` Documentation changes
- `refactor:` Code refactoring
- `test:` Test updates
- `chore:` Build/tooling changes

Example: `feat: add support for custom CNI configurations`

## Testing

### Unit Tests

``````bash
# Run all unit tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Generate mocks (before running tests)
mockery
``````

### System Tests

System tests run in GitHub's merge queue before merging to `main`. They test complete cluster lifecycle scenarios across different distributions and providers.

## Code Review Process

All submissions require review:

1. Automated checks must pass (linting, unit tests)
2. At least one maintainer approval required
3. System tests run in merge queue
4. Squash merge to `main` preserves clean history

## Release Process

Releases are automated:

1. Merge to `main` triggers version calculation based on commit messages
2. Semantic version tag created automatically (`vX.Y.Z`)
3. GoReleaser builds and publishes binaries
4. Changelog generated from commit history

## Getting Help

- **Code Documentation:** [pkg.go.dev/github.com/devantler-tech/ksail/v5](https://pkg.go.dev/github.com/devantler-tech/ksail/v5)
- **Discussions:** [GitHub Discussions](https://github.com/devantler-tech/ksail/discussions)
- **Issues:** [GitHub Issues](https://github.com/devantler-tech/ksail/issues)
- **Full Guide:** [CONTRIBUTING.md](https://github.com/devantler-tech/ksail/blob/main/CONTRIBUTING.md)

## Code of Conduct

Be respectful, inclusive, and constructive. We're all here to build better tools together.
