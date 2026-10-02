# 0010: Resolve analysis dependencies from authenticated release source

- Status: Accepted
- Date: 2026-10-02
- Related: #7131

## Context

Managed Go analysis resolves dependencies directly from upstream Git repositories. CEL's selected tag cannot be resolved through that path, and Glamour and go-macholibre Git archives require unavailable LFS downloads. These failures prevent complete extraction even though Go's published module archives supply the selected versions.

The CLI and desktop need reproducible dependency source without changing APIs or reducing analysis coverage. Cached download metadata alone does not authenticate an extracted source directory.

## Decision

Use complete copies of the published modules as version-qualified local replacements. Preserve all source code, licenses and published LFS pointer bytes. Apply only the checkout metadata adjustments below, then authenticate the actual selected directory with Go's module directory hash, independently of cached download metadata.

| Module | Version | Published source checksum |
| --- | --- | --- |
| github.com/google/cel-go | v0.31.0 | `h1:H0bhpFTqOvmHrBGrWKp7ZlhBm5Hh8PYUEXnwxT1LL7A=` |
| github.com/charmbracelet/glamour | v1.0.0 | `h1:AWMLOVFHTsysl4WV8T8QgkQ0s/ZNZo7CiE4WKhk8l08=` |
| github.com/anchore/go-macholibre | v0.1.0 | `h1:qHbdusBZNcZM/uuKf1Psa9xxAFSoyRTps8GW9gpJgsg=` |

Glamour and go-macholibre use `* -filter -text` in their attribute files so a normal checkout preserves module archive bytes without contacting KSail's LFS endpoint. CEL's archive contains a vendor manifest without its vendor packages; preserve that manifest at `upstream-vendor/modules.txt` so Go resolves dependencies through the module graph.

Required source-integrity tests bind each selected version to its complete local directory and fixed checkout checksum. They restore the original attribute bytes and vendor manifest location in a private snapshot, then require the published checksum above. This permits only the declared metadata patch: every other byte must match the upstream module. CEL's incomplete vendor directory must remain absent. CodeQL autobuild discovers every module and its evidence guard requires function bodies from these dependencies as well as the CLI and Linux desktop. A failed module extraction remains a failure.

Every change to these three source trees, root attributes or their lint classification selects the required integrity harness, including non-Go data files. Mutating formatters and duplication checks classify only these exact trees as foreign source. KSail's maintained code, compatibility modules and integrity guards remain checked; CodeQL still extracts and analyzes all modules.

## Consequences

- KSail owns complete source copies and their checkout metadata patches until authenticated successors resolve through managed analysis. The copies have no source code changes or vulnerability exceptions.
- The replacements apply only to the named versions. Dependency updates select upstream successors normally and must preserve complete source and analysis proof.
- Remove each replacement, copied directory and its integrity assertion together when a successor passes managed analysis. Preserve analysis coverage for the source selected after removal.
- Hosted managed analysis must still prove that extraction and analysis complete; local authentication and the owned CodeQL workflow do not establish that result.
