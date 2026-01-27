---
description: |
  This workflow performs research to  provides industry insights and competitive analysis.
  Reviews recent code, issues, PRs, industry news, and trends to create comprehensive
  research reports. Covers related products, research papers, market opportunities,
  business analysis, and new ideas. Creates GitHub discussions with findings to inform
  strategic decision-making.

on:
  schedule:
    # Every week, Monday (fuzzy scheduling to distribute load)
    - cron: "weekly on monday"
  workflow_dispatch:

  stop-after: +1mo # workflow will no longer trigger after 1 month. Remove this and recompile to run indefinitely

permissions: read-all

network: defaults

safe-outputs:
  app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
  create-discussion:
    title-prefix: "${{ github.workflow }}"
    category: "agentic-workflows"

tools:
  github:
    toolsets: [all]
  web-fetch:
  web-search:

timeout-minutes: 15

source: githubnext/agentics/workflows/weekly-research.md@1ef9dbe65e8265b57fe2ffa76098457cf3ae2b32

steps:
  - name: Initialize safe outputs directory
    if: always()
    run: |
      # Create safe outputs directories to prevent file not found errors
      mkdir -p /opt/gh-aw/safeoutputs
      mkdir -p /tmp/gh-aw/safeoutputs
      # Create empty safe outputs file if it doesn't exist
      # This ensures the "Ingest agent output" step can process it
      touch /opt/gh-aw/safeoutputs/outputs.jsonl
      # Pre-create the agent output file that will be uploaded
      # This ensures the artifact upload always has a file to upload
      echo '{}' > /tmp/gh-aw/safeoutputs/agent_output.json

post-steps:
  - name: Ensure agent output artifact exists
    if: always()
    run: |
      # Ensure the agent output file exists for artifact upload
      # This step runs after the main workflow and ensures the file is present
      if [ ! -f "/tmp/gh-aw/safeoutputs/agent_output.json" ]; then
        mkdir -p /tmp/gh-aw/safeoutputs
        echo '{}' > /tmp/gh-aw/safeoutputs/agent_output.json
      fi
  - name: Upload agent output fallback
    if: always()
    continue-on-error: true
    uses: actions/upload-artifact@b7c566a772e6b6bfb58ed0dc250532a479d7789f # v6.0.0
    with:
      name: agent-output
      path: /tmp/gh-aw/safeoutputs/agent_output.json
      overwrite: true
---

# Weekly Research

## Job Description

Do a deep research investigation in ${{ github.repository }} repository, and the related industry in general.

- Read selections of the latest code, issues and PRs for this repo.
- Read latest trends and news from the software industry news source on the Web.

Create a new GitHub discussion with title starting with "${{ github.workflow }}" containing a markdown report with

- Interesting news about the area related to this software project.
- Related products and competitive analysis
- Related research papers
- New ideas
- Market opportunities
- Business analysis
- Enjoyable anecdotes

Only a new discussion should be created, no existing discussions should be adjusted.

At the end of the report list write a collapsed section with the following:

- All search queries (web, issues, pulls, content) you used
- All bash commands you executed
- All MCP tools you used
