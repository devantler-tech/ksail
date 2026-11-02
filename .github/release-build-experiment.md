# Cold release build comparison

Use **Actions → Cold release build comparison → Run workflow**, selecting `main`.
The workflow is manual and accepts only its first attempt. It cannot publish a
release, image, signature or Homebrew change. Do not supply publication or cloud
credentials. Production `.goreleaser.yaml` and `cd.yaml` remain unchanged.

The cohort contains three baseline/candidate pairs on six fresh Ubuntu runners,
with at most two jobs active. Allow up to three hours for the cohort: each full
build has a 55-minute deadline inside a 60-minute job. `fail-fast: false` preserves
failed observations. Do not rerun failed members or combine reports from different
runs, attempts or harness revisions. An incomplete cohort is inconclusive.

Both arms build the complete `v7.206.65` source at
`a3a3957d45f8071c3be2fd2fc96e699459d01d73`, using Go 1.27.3 and GoReleaser 2.19.0.
The frozen configuration fixture is checked by SHA-256. Both normalize the
injected build date to the source commit date so executable hashes can be
compared. The candidate alone sets `GOMAXPROCS=1` in the build environment;
GoReleaser's outer parallelism stays at four. The baseline's child default is
measured rather than inferred from a runner label.

`goreleaser build` retains module preparation, the UI build, every target's
Copilot bundler hook, all five targets, CGO settings and trimpath. Build/module
caches start empty and are separate from those used to compile the measurement
helper. No shared cache is restored or saved. The build command omits snapshot,
skip, target and ID filters. It does not run packaging or publication stages.

Download the **release-build-comparison** artifact for the decision and the six
**release-build** artifacts for individual observations. These artifacts contain
compact JSON only; executables and build logs stay on the disposable runners.

- `inconclusive` means a build failed, evidence is missing, or the cohort cannot
  be compared. Inspect the existing native job logs. Preserve the failed run.
- `retain-baseline` is a complete negative result. A pair missed the 20% binary
  phase improvement, increased total build time, increased peak memory or caused
  more swap output. Do not select only the faster pairs.
- `candidate-qualified` requires all three pairs to meet those conditions, with
  identical hardware/runtime constraints within each pair and one program/input
  inventory across all six. It qualifies a proposal for review; it makes no
  production change and does not prove signed publication or a 15-minute PR loop.

The binary phase requires its complete GoReleaser timing receipt and surrounding
phase boundaries, a successful command and all five inspected executables.
Target, Go version, CGO, trimpath and VCS facts come from native build information.
Go omits linker flags from trimmed build information: injected metadata is checked
through the audited configuration and the real Linux `--version` output, then
bound to every other observation by matching executable hashes. The Linux help
and schema-location option must also work without a cluster.

Resource scope is explicit: the cgroup memory peak includes job preparation;
CPU usage is a build-window cgroup delta; swap output is a host counter delta.
Disk headroom is sampled every five seconds on source, module, build-cache and
scratch filesystems, including a final sample. It cannot establish an absolute
between-sample minimum. Inherited limits, affinity/CPU constraints and swap
capacity must remain unchanged before and after the build. These readings do not
prove a CPU or memory root cause by themselves.

Record the native cohort and adopt/iterate/stop decision on #7869. A qualified
candidate still needs a separately reviewed production change, an actual signed
release and downstream publication verification. The delivery task #8122 remains
open until a hosted complete build has exercised this measurement path.

Validate harness changes with:

```sh
shellcheck .github/scripts/collect-release-build-experiment.sh .github/scripts/release-build-experiment.test.sh
bash .github/scripts/release-build-experiment.test.sh
go test -count=1 ./internal/releasebuildprofile/... ./internal/ciharness/...
golangci-lint run ./internal/releasebuildprofile/...
actionlint .github/workflows/release-build-experiment.yaml
```

The frozen YAML fixture is an exact source snapshot. Updating the frozen cohort
requires reviewing all identities, tool versions, hooks, targets and acceptance
fixtures together; changing a digest alone is insufficient.
