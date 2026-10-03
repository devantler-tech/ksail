# 0010: Resolve analysis dependencies from authenticated release source

- Status: Accepted
- Date: 2026-10-02
- Related: #7131

## Context

Managed Go analysis resolves dependencies directly from upstream Git repositories. CEL's selected tag cannot be resolved through that path; Glamour, go-macholibre and Glamour's standalone ANSI dependency require unavailable LFS downloads. Concurrent shallow Git fetches also fail to validate both selected JMESPath pseudo-versions. Redis instrumentation and its command companion have selected nested tags that direct Git resolution cannot find. DynamicListener's selected tag and original commit are also unavailable through direct Git access, preventing resolution of Wharfie's dependency test graph. These failures prevent complete extraction even though Go's published module archives supply the selected versions.

The CLI and desktop need reproducible dependency source without changing APIs or reducing analysis coverage. Cached download metadata alone does not authenticate an extracted source directory.

## Decision

Use complete copies of the published modules as version-qualified local replacements. Preserve their licenses, published LFS pointer bytes and original source. Apply the checkout metadata adjustments and bounded parser repairs below, then authenticate the actual selected directory with Go's module directory hash, independently of cached download metadata.

| Module                                       | Version                              | Published source checksum                         |
|----------------------------------------------|--------------------------------------|---------------------------------------------------|
| github.com/google/cel-go                     | v0.31.0                              | `h1:H0bhpFTqOvmHrBGrWKp7ZlhBm5Hh8PYUEXnwxT1LL7A=` |
| github.com/charmbracelet/glamour             | v1.0.0                               | `h1:AWMLOVFHTsysl4WV8T8QgkQ0s/ZNZo7CiE4WKhk8l08=` |
| github.com/anchore/go-macholibre             | v0.1.0                               | `h1:qHbdusBZNcZM/uuKf1Psa9xxAFSoyRTps8GW9gpJgsg=` |
| github.com/kyverno/go-jmespath               | v0.4.1-0.20231124160150-95e59c162877 | `h1:XOLJNGX/q6MVpI8p8MKvk6jGBMvO4CrdwrizMMSsaRU=` |
| github.com/jmespath/go-jmespath              | v0.4.1-0.20220621161143-b0104c826a24 | `h1:liMMTbpW34dhU4az1GN0pTPADwNmvoRSeoZ6PItiqnY=` |
| github.com/charmbracelet/x/ansi              | v0.10.2                              | `h1:ith2ArZS0CJG30cIUfID1LXN7ZFXRCww6RUvAPA+Pzw=` |
| github.com/charmbracelet/x/ansi              | v0.11.7                              | `h1:kzv1kJvjg2S3r9KHo8hDdHFQLEqn4RBCb39dAYC84jI=` |
| github.com/redis/go-redis/extra/redisotel/v9 | v9.5.3                               | `h1:kuvuJL/+MZIEdvtb/kTBRiRgYaOmx1l+lYJyVdrRUOs=` |
| github.com/redis/go-redis/extra/rediscmd/v9  | v9.5.3                               | `h1:1/BDligzCa40GTllkDnY3Y5DTHuKCONbB2JcRyIfl20=` |
| github.com/rancher/dynamiclistener           | v1.27.5                              | `h1:FA/s9vbQzGz1Au3BuFvdbBfBBUmHGXGR3xoliwR4qfY=` |

Glamour and go-macholibre use `* -filter -text` in their attribute files so a normal checkout preserves module archive bytes without contacting KSail's LFS endpoint. CEL's archive contains a vendor manifest without its vendor packages; preserve that manifest at `upstream-vendor/modules.txt` so Go resolves dependencies through the module graph.

Preserve any upstream repository automation at `upstream-github/`, with Glamour's Dependabot configuration named `dependabot.yml.source`. These files describe the upstream repositories, and KSail does not execute them. Their archived paths prevent workflow scanners from mistaking them for KSail automation. The integrity tests restore all original names privately before requiring the published checksum; KSail's own automation remains scanned under the existing security policy.

Glamour's standalone module selects ANSI v0.10.2 independently of KSail's root graph, which retains v0.11.7 through the separate `ansi-runtime` copy. Glamour's version-qualified sibling replacement applies only to standalone analysis; the original module file is preserved as `upstream-go.mod` and restored privately for authentication. The source trees use root attributes that preserve every checkout byte without LFS filters.

Both ANSI versions accept color components only when they contain one through four hexadecimal digits and parse within 16 bits, and accept Kitty quiet modes only within their documented range of zero through two. Valid color normalization and other protocol options retain their behavior. CEL's unknown-value merge uses the larger input length for its optional map capacity hint, preserving union and deduplication without an unnecessary sum. This is a defensive simplification; no realizable allocation overflow is established. These repairs are limited to ANSI's `util.go` and `kitty/options.go`, and CEL's `common/types/unknown.go`. Each original file is retained at its relative path under `upstream-source/`, with an inert `.source` suffix. Owned regressions exercise both ANSI versions and CEL merge semantics.

Redis instrumentation's standalone module selects its command companion v9.5.3 through a version-qualified sibling replacement. Both Redis modules select the original Redis client v9.5.3 from its published module, with the upstream repository-relative replacements removed and that client's authenticated checksums recorded. Each original module and checksum file is preserved as `upstream-go.mod` and `upstream-go.sum` and restored privately for authentication. KSail's root graph retains Redis client v9.20.1.

DynamicListener retains its complete published source and module metadata without patches. Its upstream automation is archived under `upstream-github/` and restored privately to authenticate the complete published archive. The certificate and factory implementations remain part of the mandatory analysis evidence.

Required source-integrity tests bind each selected version to its complete local directory and fixed checkout checksum. They restore the original attributes, vendor manifest, module metadata and explicitly named source files in a private snapshot, then require the published checksum above. Both the repaired checkout and reconstructed upstream are authenticated: every other byte must match the upstream module. CEL's incomplete vendor directory must remain absent. CodeQL autobuild discovers every module and its evidence guard requires 21 function bodies from these dependencies, the owned logging adapter, the CLI and Linux desktop across all 13 module projects. A failed module extraction remains a failure.

Every change to these ten source trees, root attributes or their lint classification selects the required integrity harness, including non-Go data files. Mutating formatters and duplication checks classify only these exact trees as foreign source. KSail's maintained code, compatibility modules and integrity guards remain checked; CodeQL still extracts and analyzes all modules.

## Consequences

- KSail owns the complete source copies, declared metadata adjustments and bounded source repairs until authenticated successors satisfy analysis and parser behavior. No vulnerability exception is added for these repairs.
- The replacements apply only to the named versions. Dependency updates select upstream successors normally and must preserve complete source and analysis proof.
- Remove each replacement, copied directory and its integrity assertion together when a successor passes managed analysis. Preserve analysis coverage for the source selected after removal.
- Hosted managed analysis must still prove that extraction and analysis complete; local authentication and the owned CodeQL workflow do not establish that result.
