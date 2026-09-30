# NPM Audit and Fix

Audits npm dependencies without committing or pushing changes. Dependabot and Renovate pull requests, forks and merge-group runs remain read-only. Ordinary same-repository pull requests and pushes to `main` may prepare a repair patch.

Provide `working-directory` and a unique `patch-artifact-name` ending in `-patch`. The optional `audit-level` defaults to `moderate`. A successful automatic fix produces a binary Git patch containing only `package.json` and `package-lock.json`. The action uploads it only after a successful re-audit.

The workflow's signed correction job consumes the artifact:

- Ordinary pull request branches receive a GitHub API commit with a verified signature.
- Protected-branch corrections open a pull request with signed commits.
- Dependency-automation branches, forks and merge groups never enter the writer.

`audit-passed` describes the checked-out source before repair. `repair-passed` describes the proposed patch's re-audit. `patch-created` confirms its upload; `changes-committed` is always `false`. A prepared patch cannot make the source audit pass. Pull request findings remain advisory; a push to `main` fails while its source audit has findings.

Run the executable event, package-patch and reporting regressions with:

```bash
go test ./internal/ciharness -run 'TestNPMAudit|TestProtectedCorrection'
```
