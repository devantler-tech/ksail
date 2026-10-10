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

For a local observation, use a clean isolated checkout and an installed CodeQL
bundle:

```bash
CODEQL_CLI="$(command -v codeql)" GOMEMLIMIT=8GiB \
  bash .github/scripts/profile-codeql-go.sh /tmp/ksail-codeql-profile-report
```
