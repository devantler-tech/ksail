# 0007: Build the desktop app from the shared Go module

- Status: Accepted
- Date: 2026-09-30
- Related: #7125, #7140, #7388

## Context

The desktop app has its own Go module and replaces KSail with the parent directory. Dependabot updates the root module, leaving the desktop graph stale. CI repairs that graph by pushing a tidy commit onto the dependency branch. That foreign commit prevents Dependabot from rebasing the branch; later conflicts can prevent pull request workflows from starting at all.

Simply excluding Dependabot from the repair reproduces #6974: the desktop build cannot pass until its graph is repaired, so the dependency update cannot merge. A second dependency directory has not demonstrated that Dependabot will update all of the desktop module's indirect requirements. Asking another identity to impersonate Dependabot adds credentials without fixing the duplicated graph.

## Decision

Use the root Go module for both entry points. Gate every desktop source and test file behind the `desktop` build tag, retaining the Darwin constraints on its native environment helper. Native builds, tests, linting and release packaging explicitly opt in. The CLI continues to build without that tag and with CGO disabled for release.

Go's minimum version selection resolves the shared graph, and `go mod tidy` maintains dependencies from tagged files too. Dependabot therefore updates the same manifest that both entry points build from. Desktop CI checks that manifest without a same-repository exemption. Generated-file CI never writes to a Dependabot pull request branch; ordinary pull request sync and the protected-branch generated-file repair remain available.

Security analysis must include the tagged desktop source, and its extraction evidence must be checked before delivery. Native desktop tests run on Linux and macOS; macOS CI also validates the complete app bundle and cask snapshot.

## Consequences

- The dependency graph includes Wails, but the default CLI build and tests do not compile or link it. Tests guard default exclusion on Linux, macOS and Windows and retain the platform-specific desktop helpers.
- The desktop build commands now require `-tags desktop`; there is no independent desktop tidy command or manifest.
- Both entry points use one selected version of each dependency. A desktop requirement can raise a shared dependency version, so the complete CLI suite and native desktop builds are delivery gates.
- The change removes the cause of foreign dependency-branch commits. It does not prove that GitHub accepts a later Dependabot rebase or dispatches checks after a real update. #7125 and #7140 stay open until that behavior is observed on a new bot-owned branch.
