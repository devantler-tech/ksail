# GitHub App Token Configuration

## Status

✅ **All workflows have been updated and compiled successfully!**

The `.lock.yml` files have been regenerated with GitHub App token configuration and are ready to use.

## What Changed

All agentic workflows (`.github/workflows/*.md`) have been updated to use GitHub App tokens for safe outputs instead of the default `GITHUB_TOKEN`. This provides enhanced security with:

- **On-demand minting**: Tokens created at job start, minimizing exposure window
- **Short-lived**: Tokens automatically revoked at job end (even on failure)
- **Automatic permissions**: Compiler calculates required permissions based on safe outputs
- **Audit trail**: All actions logged under the GitHub App identity
- **No PAT rotation**: Eliminates need for manual token rotation

## Configuration Added

Each workflow's `safe-outputs` section now includes:

```yaml
safe-outputs:
  app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
  # ... other safe outputs
```

## Required Secrets and Variables

The following are required and should already be configured:

- `vars.APP_ID` - GitHub App ID (repository variable)
- `secrets.APP_PRIVATE_KEY` - GitHub App private key (organization secret)

These are already in use by the CI workflow (`.github/workflows/ci.yaml`).

## Compilation Results

All workflows have been compiled successfully using `gh aw compile v0.37.26`:

```
✓ audit-workflows.md (70.3 KB)
✓ ci-doctor.md (72.0 KB)
✓ daily-perf-improver.md (75.9 KB)
✓ daily-progress.md (69.6 KB)
✓ daily-qa.md (65.5 KB)
✓ daily-test-improver.md (79.0 KB)
✓ issue-triage.md (62.0 KB)
✓ pr-fix.md (70.1 KB)
✓ update-docs.md (64.5 KB)
✓ weekly-research.md (56.3 KB)

Compiled 10 workflow(s): 0 error(s), 9 warning(s)
```

The warnings are pre-existing issues unrelated to the GitHub App token changes.

## Verification

Each compiled `.lock.yml` file now includes:
- `actions/create-github-app-token@v2.2.1` step for token creation
- Automatic permission calculation (e.g., `permission-issues: write`, `permission-pull-requests: write`)
- Proper references to `vars.APP_ID` and `secrets.APP_PRIVATE_KEY`

## Future Updates

If you modify any workflow `.md` files in the future, remember to recompile them:

```bash
# Install gh-aw extension if not already installed
gh extension install githubnext/gh-aw

# Compile all workflows
cd /path/to/ksail
gh aw compile

# Or compile specific workflow
gh aw compile issue-triage
```

## Workflows Updated

1. audit-workflows.md
2. ci-doctor.md
3. daily-perf-improver.md
4. daily-progress.md
5. daily-qa.md
6. daily-test-improver.md
7. issue-triage.md
8. pr-fix.md
9. update-docs.md
10. weekly-research.md

## Reference

For more information about GitHub App tokens in agentic workflows, see:
- https://githubnext.github.io/gh-aw/reference/tokens/#github-app-tokens
