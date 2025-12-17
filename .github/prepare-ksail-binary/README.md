# Prepare KSail Binary

A composite action that consolidates the binary preparation logic used across multiple CI jobs in the KSail-Go workflow.

## Purpose

This action extracts the duplicated cache-based binary preparation pattern into a reusable component, addressing the maintenance burden identified in [#527](https://github.com/devantler-tech/ksail-go/pull/527).

## What It Does

1. **Computes cache key** based on Go version and source files
2. **Restores cached binary** from `.cache/ksail` if available
3. **Builds binary** if cache miss occurs
4. **Saves binary to cache** for future runs
5. **Ensures binary is executable** with `chmod +x`
6. **Optionally runs smoke test** (runs `--version` on the prepared binary)

## Path Differences

The action supports different output paths to accommodate job-specific requirements:

- **`build-artifact` job**: Uses `ksail` (root directory) for direct execution
- **`system-test` job**: Uses `bin/ksail` (bin directory) to avoid conflicts with test artifacts

This path flexibility is intentional and necessary because:

- The build-artifact job produces a standalone binary for verification
- The system-test job needs the binary in `bin/` to align with the test execution context and avoid conflicts with KSail-generated cluster configuration files (e.g., `k8s/`, `kind.yaml`, `k3d.yaml`)

## Usage

```yaml
- name: 📦 Prepare ksail binary
  uses: ./.github/actions/prepare-ksail-binary
  with:
    go-version: ${{ steps.setup-go.outputs.go-version }}
    source-hash: ${{ hashFiles('go.mod', 'go.sum', '**/*.go') }}
    output-path: ksail  # or bin/ksail
    run-smoke-test: 'true'  # optional, defaults to 'true'
```

## Inputs

| Input            | Required | Default  | Description                                                                                                                                                                                                     |
|------------------|----------|----------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `go-version`     | Yes      | -        | Go version from `setup-go` output, used for cache key computation                                                                                                                                               |
| `source-hash`    | Yes      | -        | Hash of source files (use `hashFiles('go.mod', 'go.sum', '**/*.go')`)                                                                                                                                           |
| `output-path`    | No       | `ksail`  | Target path for the binary relative to repository root (e.g., `ksail` or `bin/ksail`). Must be relative to repository root and must not contain path traversal sequences (e.g., `..`, `../`, `*/../*`, `*/..`). |
| `run-smoke-test` | No       | `'true'` | Whether to run `--version` smoke test on the prepared binary                                                                                                                                                    |

## Outputs

| Output                   | Description                                                         |
|--------------------------|---------------------------------------------------------------------|
| `cache-hit`              | Whether the cache was hit (`'true'` or `'false'`)                   |
| `binary-path`            | Absolute path to the prepared binary                                |
| `output-path-normalized` | Normalized relative path to the binary (with leading `./` stripped) |

## Examples

### Build-Artifact Job

```yaml
- name: 📦 Prepare ksail binary
  uses: ./.github/actions/prepare-ksail-binary
  with:
    go-version: ${{ steps.setup-go.outputs.go-version }}
    source-hash: ${{ hashFiles('go.mod', 'go.sum', '**/*.go') }}
    output-path: ksail
    run-smoke-test: 'true'
```

### System-Test Job

```yaml
- name: 📦 Prepare ksail binary
  uses: ./.github/actions/prepare-ksail-binary
  with:
    go-version: ${{ steps.setup-go.outputs.go-version }}
    source-hash: ${{ hashFiles('go.mod', 'go.sum', '**/*.go') }}
    output-path: bin/ksail
    run-smoke-test: 'false'  # tests handle validation
```

## Related

- Original issue: [#527 - Optimize CI system-test build time](https://github.com/devantler-tech/ksail-go/pull/527)
