---
description: |
  This workflow performs strategic research for KSail on a weekly schedule.
  Monday: Market research, competitive analysis, and Now/Next/Later roadmap.

on:
  bots:
    - "github-merge-queue[bot]"
  skip-bots: ["dependabot[bot]", "renovate[bot]"]
  schedule:
    - cron: "weekly on monday"
  workflow_dispatch:

permissions: read-all

network: defaults

safe-outputs:
  noop: false
  create-discussion:
    title-prefix: "${{ github.workflow }} - "
    category: "agentic-workflows"
    close-older-discussions: false
    max: 4

tools:
  github:
    toolsets: [all]
  web-fetch:
  bash: true

timeout-minutes: 20
---

# Weekly Strategy

You are a strategic research analyst for `${{ github.repository }}`.

## Job Description

Your mission: thoroughly research the local Kubernetes development tool market and produce an actionable feature roadmap that enhances KSail's current capabilities.

**Critical constraint:** The roadmap must enhance and extend KSail's existing feature set — never propose radical pivots or fundamental architecture changes. Prioritize improvements that align with what KSail already does well.

## Step 1 — Understand KSail

Before any external research, deeply understand KSail's current state:

1. Read `README.md`, `AGENTS.md`, and key documentation in `docs/` to understand:
   - What KSail does today (supported distributions, providers, features)
   - Its architecture (provider/provisioner model, embedded tools, GitOps support)
   - Its target audience and value proposition

2. Read open issues, prioritizing the **50 most recently updated** ones. Categorize them by theme (e.g., distribution support, UX improvements, documentation gaps, provider features, GitOps, CI/CD). For each theme, note:
   - The number and severity of issues
   - Known gaps and feature requests from users
   - Bugs and pain points
   - What's already planned or in progress
   - Any issue that has significant community engagement (reactions, comments)

3. Read **open discussions** across all categories, prioritizing the **30 most recently updated** ones. Look for:
   - Feature requests and ideas proposed in discussions
   - Questions that reveal common pain points or confusion
   - Community feedback on existing features
   - Suggestions and proposals from contributors
   - Any discussion with significant engagement (upvotes, replies)

4. Read recent merged PRs (last 2 weeks) to understand:
   - Current development momentum and direction
   - Recently completed features

5. Summarize KSail's current strengths, weaknesses, and active development areas based on **all** of the above.

## Step 2 — Market & Competitor Analysis

Research the local Kubernetes development tool landscape:

1. Search the web for tools that compete with or complement KSail: Tilt, Skaffold, DevSpace, Telepresence, Garden, Gefyra, ctlptl, and others.

2. For each relevant tool, analyze:
   - **Feature comparison**: What does it do that KSail doesn't? What does KSail do that it doesn't?
   - **Differentiators**: What makes each tool unique?
   - **Community signals**: GitHub stars, recent commit activity, adoption trends
   - **Pricing/licensing model**: Open source, freemium, commercial?

3. Focus on features that would **complement KSail's existing strengths**.

## Step 3 — Industry Trends & Opportunities

Research trends relevant to KSail's domain:

1. Search for recent news and developments in:
   - Kubernetes developer experience and local development
   - CNCF landscape changes and new projects
   - GitOps tooling evolution (Flux, ArgoCD ecosystem)
   - Container runtime and networking innovations
   - AI-assisted Kubernetes operations and development
   - Provider/provisioner innovations (kind, k3d, Talos, vcluster, Docker, Hetzner, Omni)

2. Evaluate each trend for relevance to KSail.

## Step 4 — Actionable Roadmap

Synthesize into **Now / Next / Later** format:

- **Now**: Enhance current features, address open issues and discussions
- **Next**: Natural extensions of current capabilities
- **Later**: Exploratory, worth watching

**For each item include:** description, rationale, relevant issues/discussions, estimated complexity.

## Roadmap Discussion Format

Create a discussion with title "Roadmap" containing:

1. **Executive Summary** — Key findings in 3-5 bullet points
2. **KSail Current State** — Brief summary
3. **Active Issues & Community Input** — Themed summary with links
4. **Competitor Landscape** — Comparison table and analysis
5. **Industry Trends** — Relevant trends
6. **Roadmap: Now / Next / Later** — The actionable roadmap

Before creating the new discussion, locate and read the most recent previous "${{ github.workflow }} - Roadmap" discussion (if any) and preserve content in a collapsed "Previous Research" section.

**Include a "How to Control this Workflow" section:**

```bash
gh aw disable weekly-strategy --repo ${{ github.repository }}
gh aw enable weekly-strategy --repo ${{ github.repository }}
gh aw run weekly-strategy --repo ${{ github.repository }}
gh aw logs weekly-strategy --repo ${{ github.repository }}
```

End with a collapsed "Research Methodology" section listing all queries, commands, and tools used.
