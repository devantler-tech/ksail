# Cache KSail Binary

Reuse a CI executable only when its compiled inputs and build environment match.
The action computes its own key, so callers do not maintain separate source globs.

## Use

Set up Go and restore dependencies first, then call the action from the repository root:

```yaml
- name: Setup Go
  id: setup-go
  uses: actions/setup-go@v7
  with:
    go-version-file: go.mod

- name: Prepare KSail
  uses: ./.github/actions/cache-ksail-binary
  with:
    go-version: ${{ steps.setup-go.outputs.go-version }}
    output-path: /usr/local/bin/ksail
```

Set `save: "false"` for a consumer that must not save another cache entry. It still
builds on a miss. Docker system-test consumers instead use the producer's verified,
run-specific artifact through `restore-ksail-binary`.

## What the key covers

Before lookup, the action generates the embedded chat documentation. It asks
`go list -deps -json .` which files the target executable uses and hashes the
selected Go/native sources and embedded files, including local replacement modules.
Module manifests, checksums and vendor metadata bind dependency selection. The active toolchain,
target and architecture settings, build flags, C compiler settings, runner image,
bundled Copilot CLI version and the action's complete build/key recipe also enter
the digest. Checkout paths do not.

Dockerfiles, chart metadata, provisioning assets, generated documentation and
staged web assets therefore invalidate the cache when they are compiled into the
executable. Ordinary test files do not, unless an embed pattern includes them.
CI builds use `-buildvcs=false` so test-only commits can reuse equivalent compiled
content. Release-build identity and run-specific artifact checks remain separate.

The action does not build the web UI: stage real assets before calling it when
that executable needs them. Otherwise the committed placeholder is the input.

Workspace builds, local replacements outside the checkout and custom `GOFLAGS`
other than `-tags=…`, `-mod=readonly` or `-mod=vendor` need a separately reviewed
recipe. Incomplete discovery, unreadable inputs or a mismatched toolchain fail
before cache lookup. No fallback key can select a different executable.

## Build and restore

The versioned key selects `.cache/ksail`. Only an exact cache hit is used. On a
miss, the action bundles the selected Copilot CLI and builds the stripped binary,
then saves it when permitted. It copies the prepared binary to `output-path` and
makes it executable, using `sudo` for system paths.

The real-action regressions are in
[`internal/ciharness/binary_cache_key_test.go`](../../../internal/ciharness/binary_cache_key_test.go):

```bash
go test ./internal/ciharness -run '^TestBinaryCacheKey' -count=1
```

## Inputs

| Input                 | Required | Default           | Purpose                                                        |
|-----------------------|----------|-------------------|----------------------------------------------------------------|
| `go-version`          | Yes      | —                 | Exact active version from `setup-go`; a mismatch fails.        |
| `output-path`         | No       | `ksail`           | Relative or absolute destination; parent traversal is refused. |
| `save`                | No       | `"true"`          | Save a newly built cache entry.                                |
| `copilot-cli-version` | No       | See `action.yaml` | Copilot CLI version bundled on a miss and included in the key. |

## Outputs

| Output                   | Meaning                                  |
|--------------------------|------------------------------------------|
| `cache-hit`              | Whether the key matched exactly.         |
| `binary-path`            | Absolute prepared-executable path.       |
| `output-path-normalized` | Destination with a leading `./` removed. |

The key follows Go's documented [build-input fields](https://pkg.go.dev/cmd/go#hdr-List_packages_or_modules)
and [embed matching](https://pkg.go.dev/embed). Cache reuse preserves GitHub's
[branch access restrictions](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching#restrictions-for-accessing-a-cache).
