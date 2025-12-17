# KSail-Go Copilot Instructions

## Big picture

- CLI entrypoint is [main.go](main.go); Cobra root + subcommands are in [cmd/](cmd/).
- Most real logic lives in [pkg/](pkg/) (no `internal/`): clients ([pkg/client/](pkg/client/)), config IO ([pkg/io/](pkg/io/)), services ([pkg/svc/](pkg/svc/)), UI ([pkg/ui/](pkg/ui/)).

## Conventions to follow

- New CLI commands should be wired under the relevant parent command (root: [cmd/root.go](cmd/root.go); cluster/workload groups under [cmd/](cmd/)) and use `runtime.RunEWithRuntime(...)` for dependency injection.
- Runtime wiring uses a lightweight DI container: [pkg/di/providers.go](pkg/di/providers.go) creates the shared runtime; commands inject only what they need.
- Config is `ksail.yaml`-first and auto-discovered by walking up parent dirs (git-style): [pkg/io/config-manager/ksail/viper.go](pkg/io/config-manager/ksail/viper.go).
- CLI flags are derived from config fields via field selectors + a pointer-to-flag mapping: [pkg/io/config-manager/ksail/binding.go](pkg/io/config-manager/ksail/binding.go). When adding a config field: FieldSelector → `getFieldMappings()` → update [pkg/io/config-manager/ksail/binding_test.go](pkg/io/config-manager/ksail/binding_test.go).
- User-facing output goes through `notify.WriteMessage(...)` and staged timers; multi-stage timing must be explicit (`Message.MultiStage = true`) to show `[stage: X|total: Y]`: [pkg/ui/notify/notify.go](pkg/ui/notify/notify.go).
- Cobra error handling is centralized via the executor/normalizer so CLI errors are readable but still unwrap correctly: [pkg/ui/error-handler/executor.go](pkg/ui/error-handler/executor.go). `main.go` prints only the root cause after unwrapping.

## Cluster lifecycle behavior (easy to break)

- `ksail cluster create` orchestrates provisioning + optional installs (CNI, metrics-server, Flux) and uses staged output. See [cmd/cluster/create.go](cmd/cluster/create.go).
- Flux install is a Helm-based installer using the OCI chart `ghcr.io/controlplaneio-fluxcd/charts/flux-operator` and intentionally silences Helm stderr to hide harmless CRD warnings: [pkg/svc/installer/flux/installer.go](pkg/svc/installer/flux/installer.go), [pkg/client/helm/client.go](pkg/client/helm/client.go).

## Local dev workflow (repo root)

- To run real cluster flows locally, you need external tools on PATH (Docker + `kind`/`k3d` + `kubectl` + `helm`); unit tests should still run without them.
- Build: `go build ./...`
- Unit tests: `go test ./...`
- Lint: `golangci-lint run --timeout 5m --fix` (format with `golangci-lint fmt`)
- Mocks: run `mockery` after changing interfaces (see `//go:generate mockery` usage, e.g. [pkg/client/helm/client.go](pkg/client/helm/client.go)).

## Tests & fixtures

- **Black-box tests only (NON-NEGOTIABLE)**: tests MUST validate exported/public APIs only.
  - Prefer external test packages: `package <pkg>_test`
  - It is FORBIDDEN to call unexported functions/methods, access unexported fields, or test internal helpers directly
  - CLI behavior tests and snapshot tests are allowed when asserting user-visible output
- Snapshot tests use `github.com/gkampitakis/go-snaps` and write to `__snapshots__/` under the relevant package (e.g. [cmd/**snapshots**/](cmd/__snapshots__/)). Tests typically call `testutils.RunTestMainWithSnapshotCleanup(...)` to keep snapshots deterministic: [pkg/testutils/helpers.go](pkg/testutils/helpers.go).

## CI expectations (do not regress)

- CI runs an end-to-end matrix that executes the built binary through: `cluster init` → `cluster create` → `cluster list` → workload ops → stop/start/delete, across Kind/K3d and options (CNI/Flux/mirror registries/metrics). See [.github/workflows/ci.yaml](.github/workflows/ci.yaml).
- A separate workflow job regenerates JSON schema and auto-commits changes via [.github/scripts/generate-schema.sh](.github/scripts/generate-schema.sh).

## Active Technologies

- 005-argocd-gitopsengine-oci: Go 1.25.4 + Cobra CLI (`github.com/spf13/cobra`), DI runtime (`github.com/samber/do/v2`), output styling (`github.com/fatih/color`), OCI image tooling (`github.com/google/go-containerregistry`), snapshot testing (`github.com/gkampitakis/go-snaps`); config is `ksail.yaml` + in-cluster resources; Kubernetes API is primary state store

## Recent Changes

- 002-timing-output-control: Added Go 1.25.4 + Cobra CLI (`github.com/spf13/cobra`), DI runtime (`github.com/samber/do/v2`), output styling (`github.com/fatih/color`)
