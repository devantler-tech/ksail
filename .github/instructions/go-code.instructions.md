---
description: "Use when writing or editing Go source files in the KSail codebase. Covers error handling, path safety, dependency guard, and package organization conventions."
applyTo: "**/*.go"
---
# Go Code Conventions

## Path Safety (Security)

- **All user-supplied file paths** must be canonicalized with `fsutil.EvalCanonicalPath` before use (resolves symlinks, prevents symlink-escape attacks).
- For constrained reads, use `fsutil.ReadFileSafe` instead of reimplementing containment checks.
- For output paths that may not yet exist, call `os.MkdirAll(filepath.Dir(outputPath), <mode>)` first, then `EvalCanonicalPath`.

## Error Handling

- API-level validation errors are centralized in `pkg/apis/cluster/v1alpha1/errors.go`.
- Use sentinel errors (`var ErrFoo = errors.New(...)`) for domain errors.
- Wrap errors with `fmt.Errorf("context: %w", err)` to preserve the chain.

## Dependency Guard

`.golangci.yml` enforces a strict import allowlist via `depguard`. Adding a new dependency requires:

1. Adding it to the `allow` list in `.golangci.yml`
2. Running `golangci-lint run` to verify

## Package Organization

- `pkg/` — Public API surface. Importable by external consumers.
- `internal/` — Private packages (Go compiler enforces import restriction).
- `pkg/svc/` — Service layer (providers, provisioners, installers, chat, MCP).
- `pkg/client/` — Embedded tool clients. Distribution tools (kind, k3d, vcluster) are used via SDK in provisioners, not wrapped here.

## AI Tool Generation (`pkg/toolgen/`)

CLI commands under consolidated parents are auto-exposed as MCP/Copilot tools. Do NOT manually register tool handlers. See [CONTRIBUTING.md](../../CONTRIBUTING.md) for details.

## Enum Types

Each enum lives in its own file under `pkg/apis/cluster/v1alpha1/` (e.g., `distribution.go`, `cni.go`). Implement the `EnumValuer` interface from `enum.go`.
