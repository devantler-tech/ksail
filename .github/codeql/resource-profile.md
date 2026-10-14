# Go extraction resource measurements

The CodeQL Go resource profile workflow measures each tracked Go module and a
separate desktop extraction on a standard Linux runner. It runs when the
collector changes and can also be dispatched with `8GiB` or `off` as the Go heap
limit. Existing security analysis and managed code-quality checks remain
independent delivery gates.

The artifact contains the source commit, observed runner memory and processor
count, tool versions, requested project inventory, process exit statuses,
elapsed seconds, and maximum resident set size in bytes. Command peak RSS is the
native timer's measurement of the command and its children; it is neither the Go
heap limit nor a sum of simultaneous process memory.

`complete` means that every requested command produced valid measurements and
exited successfully. `sourceVerified` additionally requires unchanged Go source
and module metadata after extraction. `coverageVerified` requires the database's
25 CLI, desktop, dependency and collector function bodies, with no failed-project
diagnostics. A successful profile requires all three and successful tracing and
finalization. It does not constitute a security or code-quality verdict.

Reports are replaced atomically after each command so earlier observations
survive later failures or timeouts. Missing or interrupted commands leave the
report incomplete. Databases and raw logs remain temporary and are not uploaded.

## Controlled swap comparison

Dispatch the workflow with `swap_comparison=true` to collect one paired experiment.
The default remains false; ordinary PR and main profiles retain their 8 GiB heap
limit and 40-minute measurement deadline. The comparison uses two fresh standard
Linux runners at the same source commit, with the heap limit explicitly off.
One arm preserves the runner's existing swap. The other adds and verifies 8 GiB
before downloading the extraction dependencies. It never turns off existing swap.
Comparison measurements have a 100-minute deadline and 120-minute job limit.

The collector binds the source SHA, run and attempt, image version, architecture,
kernel, processor capabilities, installed package versions, Go and CodeQL versions,
extractor digest, original swap, physical memory, processor count and inherited
cgroup-v2 memory/swap limits. The pinned setup action uses its linked tools bundle.
An unreadable constraint, failed collector or unequal environment rejects the
comparison. Each artifact name includes its source, attempt and swap arm.

`measurementsValid` accepts a complete inventory of valid timer and native-log
observations even when extraction fails. It does not relax `complete`: that field
still requires every requested extraction command to succeed.
`observedExtractedFileCount` counts distinct repository Go paths whose native
extractor completion was logged for each command. Completion is logged before
the TRAP writer closes, so this count does not establish database coverage.

A finalized database supplies its own Go-file count, desktop-file count and
digest of the canonical sorted native file set. The positive function-body and
failed-project diagnostic checks still apply. A pair is coverage-comparable only
when both arms pass these checks and their native file sets match. A failed,
otherwise valid baseline is reported as `baseline-incomplete`, with its original
exit statuses and null native coverage if finalization never ran; it yields no
speedup or equivalent-coverage claim. A failed added-swap arm fails collection.
Neither outcome changes the independent analysis or managed quality gates.

Five-second Linux samples retain minimum available memory, maximum swap use and
largest sampled process RSS. They cover the whole profile, including finalization
and coverage queries. These are sampled extrema, not exact aggregate peaks or Go
heap measurements. Per-project native timer RSS and wall duration remain separate.
Missing samples or a stale tail invalidate a comparison.

One pair measures this workload under the recorded conditions. It does not prove
sustained reliability or establish the cause of a historical runner shutdown.
The earlier shutdown trigger remains unknown unless independent evidence identifies
it. Raw source paths, logs and database files are not exported in the artifacts.

For a local observation, use a clean isolated checkout and an installed CodeQL
bundle:

```bash
CODEQL_CLI="$(command -v codeql)" GOMEMLIMIT=8GiB \
  bash .github/scripts/profile-codeql-go.sh /tmp/ksail-codeql-profile-report
```
