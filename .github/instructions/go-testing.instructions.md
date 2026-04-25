---
description: "Use when writing or editing Go test files, creating unit tests, benchmarks, or test helpers. Covers KSail's testing conventions, table-driven patterns, and the export_test.go seam."
applyTo: "**/*_test.go"
---
# Go Testing Conventions

## Test Style

- **Black-box tests**: Use `package foo_test` (not `package foo`). Test only exported API.
- **Table-driven tests**: Preferred for multiple scenarios. Each subtest gets `t.Parallel()`.
- **One test file per source file**: `foo.go` → `foo_test.go`.
- **`t.Parallel()`**: Always call on every test and subtest, except tests that mutate shared state (env vars, global config).

## Test Seams (`export_test.go`)

When an internal function must be tested directly, expose it via `export_test.go` in the same package:

```go
// export_test.go (package foo, NOT package foo_test)
package foo

var ExportedForTest = internalFunction
```

This is the **only** accepted pattern for testing internals. Avoid it when possible.

## Libraries

- **`github.com/stretchr/testify`**: `assert` for non-fatal, `require` for fatal checks
- **`github.com/gkampitakis/go-snaps`**: Snapshot testing for complex outputs
- **`github.com/vektra/mockery`**: Mock generation for interfaces

## No External Dependencies

Tests must not require Docker, filesystem writes outside `t.TempDir()`, or network access.

## Linting

Linter config (`.golangci.yml`) runs `all` linters. Key exemptions are documented there. After writing tests: `golangci-lint run`.
