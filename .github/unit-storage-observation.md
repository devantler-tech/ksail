# Fixed unit-test storage observation

This temporary observation serves [#8153](https://github.com/devantler-tech/ksail/issues/8153)
and [the shared capacity incident](https://github.com/devantler-tech/.github/issues/838).
It is a single manual job, disabled unless deliberately dispatched on reviewed
`main`. It grants GitHub contents-read access, uses no infrastructure credentials,
creates no service or cluster, and saves no caches. Normal required CI is unchanged.

## The control

The failed required job checked out synthetic merge
`bab0963e3c16b91e8a236e9679a29ffcde276b8f`, combining PR #8148 head
`42d3bba2f3d66c9e4d852d9bbea810b62433391d` with base
`620cdc63eae95e0145eaebe38d4ea6cd75c3cf55`. The probe checks out that actual
tested merge, installs Go 1.27.3, and runs the same complete `go test ./...`.
Its twenty-minute command budget and thirty-minute disposable-job ceiling retain
timeouts as failures.

The [frozen cleanup](../internal/ciharness/testdata/unit-storage-control/cleanup.run) is generated
from the exact shared Test step's decoded `run` scalar, retaining its final
newline. Its [provenance](../internal/ciharness/testdata/unit-storage-control/provenance.json)
records the immutable source commit, workflow blob and body SHA-256. This is a
historical experimental control, rather than a second maintained cleanup policy.
The workflow checks its fingerprint before running it. Its budget remains zero
and its own optional measurement remains disabled.

The module-cache key is fixed to the failed run. Both the exact-hit and matched-key
outputs must agree before the unit command can run; a partial or missing cache
does not establish this envelope. No compilation or schema archive is restored.
The observer requires an empty compilation cache, allowing only Go's cache
metadata files. The original logs record cache misses, rather than proving the
original image had no preseeded entries. The controlled empty cache is recorded
explicitly. Cache-key and compressed-size agreement do not supply an archive
content digest absent from the original receipt.

## Run and inspect

After the change is protected-merged and its ordinary checks pass:

```sh
gh workflow run measure-unit-storage.yaml \
  --repo devantler-tech/ksail --ref main -f enabled=true
```

GitHub requires a manual workflow to exist on the default branch; see its
[manual-run documentation](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow).
The [cache reference](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching)
explains exact hits, partial matches and scope. Do not enable a fallback or save a
new cache to turn an unavailable control into apparent reproduction.

Download the small `unit-storage-...` artifact. Independently bind its native
artifact digest, run and attempt to the workflow execution, then inspect
`report.json`. It must name the fixed consumer, actual harness commit, runner
image/version, actual Go version, verified module-cache key and a completed
command. Both `countersComplete` and `componentsComplete` must be true for
complete observation. A nonzero command exit remains a failed job; it can still
be a useful, complete storage observation. It never establishes CI recovery.

The report contains controlled storage labels and opaque filesystem groups,
numeric byte/inode counters and component sizes. Before Go setup, unresolved
cache and toolchain locations are marked explicitly. The observer examines each
pathname separately: common device identity alone does not exclude different
directory-quota behavior. Component sizes can overlap and must not be summed as
independent consumption.

The sampler normally waits one second between observations. Component walks at
boundaries and roughly every sixty samples can delay the next observation; the
actual timestamps remain in the report. Each probe and the eight-MiB receipt are
bounded. The collector is Bash so it never compiles a tool into the measured
cache or changes its cold compilation state.

Private paths, device numbers and raw test output remain outside the single
uploaded JSON file. Read failures are unknown, never zero. User/project quota
limits are not established by these filesystem counters and remain an explicit
limitation. Sampled extrema are not exact instantaneous peaks or isolated Go
resource accounting.

## Decide the capacity repair

The failed job had 38.609 GiB free on root after cleanup. A later green job had
110.826 GiB, with the same exact module-cache key and compressed size and misses
for compilation and schema archives. Both reported Ubuntu 26.04.1; their image
versions differed. This is an observed capacity difference, rather than proof of
its cause or evidence for changing OS major versions.

Record the new observation on the incident before choosing reclamation,
relocation, footprint reduction or another capacity change. Keep the original
adverse receipt. Retire or deliberately retarget this fixed experiment when the
capacity correction is verified; historical evidence remains in Git history and
the issue receipts.
