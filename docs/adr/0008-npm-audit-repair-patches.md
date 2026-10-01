# 0008: Deliver npm audit repairs through the signed correction job

- Status: Accepted
- Date: 2026-10-01
- Related: #7125, #7399, #7402

## Context

Dependabot must retain sole ownership of its branches to accept later rebases. An npm audit repair committed by another identity removes that ownership. KSail also requires verified commit signatures and delivers protected-branch corrections through pull requests.

## Decision

The npm audit action is a read-only auditor for dependency-automation pull requests, forks and merge-group runs. It never receives a write token or commits changes. Ordinary same-repository pull requests and pushes to the protected default branch may prepare package-manifest repair patches.

The existing correction job consumes those patches alongside generated-file patches. It uses the GitHub commit API and verifies signatures for ordinary pull request branches. Protected-branch corrections use signed pull requests. The writer remains inaccessible to dependency-automation branches, forks and merge groups.

The source audit result is distinct from a proposed repair's re-audit result. Preparing a patch does not mean the branch was corrected. Findings remain visible, pull request audits retain their existing advisory policy, and the protected default branch fails its audit while its source still contains findings.

## Consequences

- Audit jobs need read access only; commit credentials stay in the correction job.
- Independent npm audits produce separate patch artifacts for one writer, avoiding competing branch commits.
- A failed or incomplete fix cannot make the source audit pass. Only package manifests enter the repair patch.
- Executable workflow coverage guards event policy, patch contents and audit reporting. A real bot-owned npm update and accepted rebase remain separate outcome evidence on #7399.
