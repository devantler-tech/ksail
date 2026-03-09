---
description: |
  This workflow upgrades npx skills daily by checking for available updates and creating
  a pull request when new versions are found. Skills provide specialized knowledge and
  capabilities for AI agents in the repository.

on:
  bots:
    - "github-merge-queue[bot]"

  skip-bots: ["dependabot[bot]", "renovate[bot]"]
  schedule: daily
  workflow_dispatch:

timeout-minutes: 15

permissions: read-all

network:
  allowed:
    - defaults
    - node
    - github

safe-outputs:
  noop:
  create-pull-request:
    labels: [dependencies, automation]
  create-issue:

steps:
  - name: Checkout repository
    uses: actions/checkout@v6.0.2
    with:
      persist-credentials: false

  - name: Setup Node.js
    uses: actions/setup-node@v6.2.0
    with:
      node-version: "22"

tools:
  github:
    toolsets: [repos, issues, pull_requests]
  bash:
    - "*"
---

# Daily Skills

Your name is "${{ github.workflow }}". Your job is to upgrade the npx skills in the GitHub repository `${{ github.repository }}` to their latest versions.

## Instructions

1. **Check for skill updates**:
   - Run `npx skills check` to see if any installed skills have available updates
   - Review the output to identify which skills have newer versions

2. **Apply updates if available**:
   - If updates are available, run `npx skills update` to upgrade all skills to their latest versions
   - Review the output to confirm which skills were updated

3. **Verify the changes**:
   - Check which files were modified:

     ```bash
     git status
     ```

   - Review changes to `skills-lock.json`:

     ```bash
     git diff skills-lock.json
     ```

   - Review changes to skill files:

     ```bash
     git diff .agents/skills/
     ```

4. **Create appropriate outputs**:
   - **If skills were updated**: Create a pull request with the title `Update skills - [date]` containing:
     - Updated `skills-lock.json` and `.agents/skills/` files
     - A summary of which skills were updated (old version/hash → new version/hash)
     - Links to the source repositories for the updated skills

   - **If no updates are available**: Call the `noop` safe output

   - **If the update command fails**: Create an issue with the title `Failed to update skills` containing:
     - The error output from `npx skills check` or `npx skills update`
     - Any relevant context about the failure

## Important Notes

- Node.js has already been set up and is available for use
- The `npx skills` CLI manages skill packages defined in `skills-lock.json`
- Updated skills are stored in the `.agents/skills/` directory
- Always include both `skills-lock.json` and `.agents/skills/` changes in the PR
- **You MUST always produce a safe output** — either `noop`, `create_pull_request`, or `create_issue`
