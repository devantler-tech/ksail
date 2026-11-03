# Go dependency license admission

The required **Audit Shipped Dependency Graphs** job checks the Go source
dependencies selected by every declared CLI and desktop release target. The
scanner is a pinned, test-only dependency and is absent from the shipped CLI's
dependency graph. Run the same check from the repository root:

```sh
go test ./pkg/client/kubescape -run '^TestReleasedGoDependencyLicenses$' -count=1
```

The test derives its targets from the release configurations. It loads source
metadata without building or running foreign executables. It independently joins
every scanner package to Go's selected dependency graph, rejects missing or
repeated membership, and treats loading and classifier errors as failures.
Cached test verdicts cannot replace the required check.

## The policy

[`go-license-policy.json`](go-license-policy.json) is the explicit admission
policy. It includes common permissive licenses, the reviewed MPL-2.0 dependencies
and OFL-1.1 font terms. MPL is file-level copyleft; OFL applies to the font, not to
all software that uses it. Admission does not erase their obligations.

Unknown or otherwise unapproved terms fail. First-party ownership is separate:
it requires the exact main-module identity, repository directory and PolyForm
Shield license fingerprint. A similarly named module or an unrecognised nested
license cannot inherit that decision.

Every third-party exception names exact packages, the selected module version,
its `h1` checksum, the source declaration's relative path and SHA-256, the raw
classifier result and the effective terms. The check recomputes the complete
extracted-module checksum; it does not trust a cached `.ziphash`. Replacements,
changed source, changed declarations, prefix collisions and unexpected raw
results invalidate the exception.

Some declarations need an explicit interpretation:

- SPDX tools-golang offers an Apache-2.0 **or** GPL choice. The fingerprinted
  disposition selects Apache-2.0; GPL is not generally allowed.
- Chroma declares MIT software and separately licensed Liberation Mono font
  content. The SVG package retains both MIT and OFL-1.1 obligations.
- The selected xi2/xz declaration places its files in the public domain. This
  decision does not approve other projects with “XZ” in their name.
- The identified admin-apis component has a maintainer-verified permission
  record for compilation into KSail binaries. The upstream license stays
  unchanged. The original correspondence remains private under
  `LOFT-ADMIN-APIS-KSAIL-001`; no general source grant, later-version permission,
  duration, irrevocability or downstream rights are inferred.

## Changing a dependency or extending admission

1. Inspect the **selected** module and any replacement, its complete linked
   package membership and the actual license declarations. An unknown scanner
   result is a review request, not proof of unlawful use.
2. Prefer a supported source revision that includes its license. The
   external-types and reports-api updates select license-bearing commits whose
   Go files and module requirements are unchanged; they do not infer a grant
   for older revisions.
3. For a classifier gap or a specific permission, record immutable upstream
   evidence and bind the exact packages, version, authenticated source and
   declaration bytes in the policy. Preserve independently licensed nested
   packages and additional obligations. Keep private correspondence out of Git.
4. Run the command above across every derived target, then the full repository
   validation. Keep the negative controls for unknown/denied terms, altered
   source, license changes, replacements, ownership and incomplete inventories.
   A new exception needs substantive review just like application code.

Do not add an Unknown wildcard, a module-prefix exception, or a blanket GPL
allowance to obtain a passing check. Updating an approved dependency can require
a fresh source assessment even when the source is still compatible.

## Evidence and redistribution

CI retains per-target JSON reports for 14 days. Each report preserves every
package's raw finding and its effective disposition. Declared module identity
and a published replacement's actual module, version and checksum are retained
separately. Local replacements are marked without exposing machine paths or
private correspondence. A missing or incomplete inventory fails the job.

This is **Go source dependency admission**. It does not prove the contents or
license delivery of release archives, native C/C++ libraries, system frameworks,
embedded executables, JavaScript/assets or containers. Keep their inventories
and artifact-level acceptance separate; scanner warnings about non-Go content
are not suppressed.

Before claiming complete redistribution acceptance, inspect the actual released
artifacts for the applicable texts, copyright notices and source-access
information. For example, Mozilla explains the source-information requirement
for compiled MPL libraries in its [MPL FAQ, question 8](https://www.mozilla.org/en-US/MPL/2.0/FAQ/).
The [OFL FAQ](https://openfontlicense.org/ofl-faq/) distinguishes font bundling,
embedding and distribution. An admission verdict alone establishes neither
notice delivery nor complete legal compliance for an artifact. Recipient-facing
Go dependency packaging and published-artifact verification are tracked in
[#8151](https://github.com/devantler-tech/ksail/issues/8151).
