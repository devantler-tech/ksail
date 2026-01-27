# GitHub App Token Configuration

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

## Next Steps

To apply these changes, the workflows need to be compiled:

```bash
# Install gh-aw extension if not already installed
gh extension install githubnext/gh-aw

# Compile all workflows
cd /path/to/ksail
gh aw compile

# Or compile specific workflow
gh aw compile issue-triage
```

This will regenerate the `.lock.yml` files with the GitHub App token configuration.

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
