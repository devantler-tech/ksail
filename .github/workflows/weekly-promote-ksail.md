---
description: |
  This workflow produces ONE finished, ready-to-share piece of promotional content
  for KSail each week. The AI picks the best medium (Reddit, LinkedIn, or blog post)
  based on what's genuinely interesting that week, writes a complete post, and delivers
  it as a GitHub Discussion for human review before sharing.

on:
  bots:
    - "github-merge-queue[bot]"

  skip-bots: ["dependabot[bot]", "renovate[bot]"]
  schedule:
    - cron: "weekly on wednesday"
  workflow_dispatch:

permissions: read-all

network: defaults

safe-outputs:
  noop: false
  create-discussion:
    title-prefix: "${{ github.workflow }} - "
    category: "agentic-workflows"
    close-older-discussions: true
    max: 1

tools:
  github:
    toolsets: [all]
  web-fetch:

timeout-minutes: 15
---

# Weekly Promote KSail

You produce **one finished piece of content** per week for KSail, a Kubernetes SDK for local GitOps development. The content must be ready to copy-paste and share without modifications.

## Step 1 — Research context

Gather what's genuinely interesting about KSail right now:

1. Read the repository README.md for current feature highlights.
2. Check recent releases and merged PRs for new or improved features.
3. Read the most recent Weekly Roadmap discussion (category: `agentic-workflows`, title prefix: `Weekly Roadmap`) for roadmap context and competitive insights.
4. Read the previous Weekly Promote KSail discussions to see which mediums were used recently.
5. Identify the single most compelling thing to write about this week — a new feature, a solved problem, a use case, a comparison, or a lesson learned.

## Step 2 — Pick the medium

Choose **one** platform. Rotate naturally — avoid picking the same platform two weeks in a row (check previous discussions from Step 1).

| Medium                                            | Best for                                                             | Tone                                                                                 |
|---------------------------------------------------|----------------------------------------------------------------------|--------------------------------------------------------------------------------------|
| **Reddit** (r/kubernetes, r/devops, r/selfhosted) | Sharing a practical tip, asking for feedback, announcing a milestone | Community-first, never promotional. Lead with the problem solved, not the tool name. |
| **LinkedIn**                                      | Professional milestones, architecture insights, lessons learned      | Conversational but polished. Tell a story or share an insight. No listicle format.   |
| **Blog post** (devantler.tech)                    | Feature deep-dives, tutorials, comparisons                           | 800–1500 words. Include actual code snippets and real examples.                      |

## Step 3 — Write the content

Write the complete, final post. Follow these rules strictly:

### Voice rules

- Sound like a real developer sharing their work, not a marketing department.
- Use first person ("I", "we") naturally.
- Include specific technical details, not generic claims.
- No buzzword-stuffing or hype language.
- No emoji spam (1–2 max, only where natural).
- Never use phrases like "game-changer", "revolutionary", "excited to announce", "I'm thrilled", "incredibly powerful", "seamless", or "unlock".
- The post must stand completely on its own — **no placeholders** like "[insert X here]" or "[add link]".

### Platform-specific rules

- **Reddit**: Match subreddit culture. Be genuinely helpful. Lead with the problem solved, not the tool. Write as a community member sharing something useful, not as a project maintainer promoting their work.
- **LinkedIn**: Tell a story or share a concrete insight. Avoid generic motivational or listicle format. No "5 reasons why..." posts.
- **Blog post**: Use the Starlight blog frontmatter format below. Replace every placeholder with real values — use today's date, a real title, actual tags, and a genuine excerpt. No placeholder text may remain in the final output.

  ```yaml
  ---
  title: "Your Title Here"
  date: YYYY-MM-DD
  authors:
    - devantler
  tags:
    - relevant-tag
  excerpt: "One-sentence summary."
  ---
  ```

  Write the full Markdown body after the frontmatter. Include code blocks, CLI examples, or architecture diagrams where they add value.

## Step 4 — Self-review

Re-read everything you wrote and honestly ask:

1. Would a real developer post this? Does it sound genuine?
2. Is it providing value to the reader, or just advertising?
3. Would I scroll past this in my feed? If yes, rewrite.
4. Does it match the platform's culture and norms?
5. Are there any placeholder phrases or generic filler?

If any answer is wrong, rewrite before outputting.

## Output format

Create a GitHub discussion with this structure. Replace all guidance below with concrete values — no bracketed placeholders may appear in the final discussion body.

### Platform

**Medium name** — One sentence explaining why this medium was chosen this week.

### The Post

````text
The complete, copy-paste-ready content goes here inside a fenced code block so it can be copied without cleanup. For blog posts, include the full frontmatter + Markdown body.
````

### Posting notes

- Where exactly to post (subreddit name, LinkedIn as article vs post, blog file path)
- Best time to post if relevant
- Any relevant hashtags for LinkedIn
- Which subreddit flair to use if applicable
