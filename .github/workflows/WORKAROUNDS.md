# Agentic Workflows Workarounds

This document describes known issues and workarounds for the agentic workflows in this repository.

## Permission-Discussions Issue

### Problem

The `gh-aw` compiler (as of v0.37.26) generates workflow lock files that use `permission-discussions: write` when creating GitHub App tokens for safe-outputs jobs. However, the `actions/create-github-app-token` action (v2.2.1) does not support `permission-discussions` as a valid input parameter.

**Error Message:**
```
Warning: Unexpected input(s) 'permission-discussions', valid inputs are ['app-id', 'private-key', 'owner', 'repositories', 'skip-token-revoke', 'github-api-url', 'permission-actions', 'permission-administration', 'permission-checks', ...  'permission-team-discussions', ...]
```

### Root Cause

When workflows use `safe-outputs.create-discussion`, the compiler automatically adds discussions permissions to the GitHub App token. The compiler uses `permission-discussions`, but the action only recognizes `permission-team-discussions`.

**Upstream Issue:** https://github.com/actions/create-github-app-token/issues/307

### Workaround

After compiling workflows with `gh aw compile`, run the following command to fix the permission names:

```bash
cd .github/workflows
sed -i 's/permission-discussions:/permission-team-discussions:/g' *.lock.yml
```

Or manually replace all instances of `permission-discussions:` with `permission-team-discussions:` in the `.lock.yml` files.

### Affected Workflows

- `audit-workflows.lock.yml`
- `ci-doctor.lock.yml`
- `daily-perf-improver.lock.yml`
- `daily-progress.lock.yml`
- `daily-qa.lock.yml`
- `daily-test-improver.lock.yml`
- `issue-triage.lock.yml`
- `pr-fix.lock.yml`
- `update-docs.lock.yml`
- `weekly-research.lock.yml`

### Status

**Applied:** 2026-01-27
**Expected Resolution:** Waiting for upstream fix in `gh-aw` compiler or `actions/create-github-app-token` to support `permission-discussions`

### Alternative Solutions Considered

1. **Remove discussions permission from workflows:** Not viable because safe-outputs automatically requires it
2. **Use different safe-output method:** Would require redesigning all workflows
3. **Fork and patch gh-aw:** Too maintenance-heavy

### Notes

- `permission-team-discussions` is for organization team discussions, not repository discussions
- Repository discussions may not have a corresponding permission in the GitHub App token action yet
- This workaround allows the workflows to run without errors, though they may use team discussions permission instead of repository discussions permission
