# Published vulnerability identities

Local Go module replacements have a directory path and no published version in
govulncheck's source graph. The required complementary guard gathers every local
replacement's original module path and version, including nested standalone
graphs, and queries the published vulnerability database with govulncheck v1.8.0.
Version-qualified replacements identify the copied release; unqualified
replacements use Go's selected module version. Every local tree must declare the
same module path and remain inside the repository without symbolic links.

The guard requires a successful scanner process, its pinned query configuration,
complete JSON, and exactly one observation for every requested identity. Any
reported advisory blocks the check. It does not use the symbol-reachability
allowlist: a module query has no reachability evidence. The normal source scan,
authenticated source checks and CodeQL analysis remain required independently.

The required integration test uses the actual scanner and its authenticated
upstream database fixture. It checks an affected published version through a
local replacement, a fixed version, malformed output, and a failed process that
emits plausible clean output. Run it with `GOVULNCHECK_BIN` pointing to the pinned
executable and `GOVULNCHECK_TEST_DB` pointing to its database fixture, then run
`go test -tags integration ./internal/vulnidentity/... -count=1`.
