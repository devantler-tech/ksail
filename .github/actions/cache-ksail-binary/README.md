# Cache KSail Binary

A composite action that restores the KSail binary from the Actions cache, or builds it when the cache has no match. The CI and system-test workflows use it so every job runs the same binary without rebuilding it.

## Purpose

This action extracts the duplicated cache-based binary preparation pattern into a reusable component, addressing the maintenance burden identified in [#527](https://github.com/devantler-tech/ksail/pull/527).

## What It Does

1. **Computes the cache key** from the runner OS, the Go version, the source hash and the bundled Copilot CLI version
2. **Restores the cached binary** from `.cache/ksail` if that key exists
3. **Builds the binary on a cache miss**: generates the embedded chat documentation (`go generate ./pkg/svc/chat/...`), bundles the Copilot CLI (`go tool bundler`), then runs a stripped `go build`
4. **Copies the binary** to `output-path` and makes it executable (with `sudo` for an absolute path)
5. **Saves the binary to the cache** after a build, unless `save` is `'false'`

The action does not build the web UI.

## Usage

```yaml
- name: ⚙️ Setup Go
  id: setup-go
  uses: actions/setup-go@v7
  with:
    go-version-file: go.mod

- name: 📦 Cache KSail Binary
  uses: ./.github/actions/cache-ksail-binary
  with:
    go-version: ${{ steps.setup-go.outputs.go-version }}
    source-hash: ${{ hashFiles('go.mod', 'go.sum', '**/*.go') }}
    output-path: /usr/local/bin/ksail
```

Go must be set up first: the action runs `go` itself on a cache miss.

## Inputs

| Input                 | Required | Default           | Description                                                                                                                                                                                                                                  |
|-----------------------|----------|-------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `go-version`          | Yes      | -                 | Go version from the `setup-go` output, used in the cache key                                                                                                                                                                                 |
| `source-hash`         | Yes      | -                 | Hash of the source files (use `hashFiles('go.mod', 'go.sum', '**/*.go')`)                                                                                                                                                                    |
| `output-path`         | No       | `ksail`           | Where to put the binary. Relative to the repository root (for example `ksail`) or absolute (for example `/usr/local/bin/ksail`). Must not contain a parent-directory step (`..`); the action fails if it does.                               |
| `save`                | No       | `'true'`          | Whether to save the cache after a build. Set to `'false'` in jobs that should only restore, so one job owns the cache entry.                                                                                                                 |
| `copilot-cli-version` | No       | see `action.yaml` | Copilot CLI version bundled into the binary. It is part of the cache key, so a version change rebuilds instead of restoring a binary bundled with the previous version. Leave it at the default unless you are changing the bundled version. |

## Outputs

| Output                   | Description                                                                                            |
|--------------------------|--------------------------------------------------------------------------------------------------------|
| `cache-hit`              | Whether the cache was hit (`'true'` or `'false'`)                                                      |
| `binary-path`            | Absolute path to the prepared binary                                                                   |
| `output-path-normalized` | The `output-path` as used: a relative path with a leading `./` stripped, or the absolute path as given |

## Examples

### Build and save (one job per workflow)

```yaml
- name: 📦 Cache KSail Binary
  uses: ./.github/actions/cache-ksail-binary
  with:
    go-version: ${{ steps.setup-go.outputs.go-version }}
    source-hash: ${{ hashFiles('go.mod', 'go.sum', '**/*.go') }}
    output-path: /usr/local/bin/ksail
```

### Restore only

```yaml
- name: 📦 Cache KSail Binary
  uses: ./.github/actions/cache-ksail-binary
  with:
    go-version: ${{ steps.setup-go.outputs.go-version }}
    source-hash: ${{ hashFiles('go.mod', 'go.sum', '**/*.go') }}
    output-path: /usr/local/bin/ksail
    save: "false"
```

A restore-only job still builds the binary when the cache has no match; it just does not write the result back.

## Related

- Original change: [#527 - Optimize CI system-test build time](https://github.com/devantler-tech/ksail/pull/527)
