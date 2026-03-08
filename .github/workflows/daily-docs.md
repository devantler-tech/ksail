---
description: |
  This workflow maintains documentation by synchronizing docs with code changes and reducing
  documentation bloat. Trigger-based mode selection: push-to-main runs doc sync mode (analyzing
  diffs and updating docs), schedule or /unbloat command runs bloat reduction mode, and
  workflow_dispatch runs both modes sequentially.

on:
  bots:
    - "github-merge-queue[bot]"

  skip-bots: ["dependabot[bot]", "renovate[bot]"]
  push:
    branches: [main]
  schedule: daily
  slash_command:
    name: unbloat
    events: [pull_request_comment]
  workflow_dispatch:

timeout-minutes: 30

permissions: read-all

strict: false

network:
  allowed:
    - defaults
    - node

safe-outputs:
  noop: false
  create-pull-request:
    title-prefix: "[docs] "
    labels: [documentation, automation]
    draft: true
  add-comment:

tools:
  cache-memory: true
  github:
    toolsets: [all]
  web-fetch:
  edit:
  bash: true
---

# Daily Docs

You are a documentation maintenance agent for `${{ github.repository }}`. You operate in two modes based on the trigger:

- **Doc Sync mode** (push to main): Analyze code diffs and update documentation to stay in sync
- **Bloat Reduction mode** (schedule or `/unbloat` slash command): Scan docs for bloat and simplify
- **Full mode** (workflow_dispatch): Run both modes sequentially

## Mode Selection

Determine which mode to run based on the trigger:

1. **If triggered by a push to main**: Run **Doc Sync mode** only
2. **If triggered by schedule or `/unbloat` slash command**: Run **Bloat Reduction mode** only
3. **If triggered by workflow_dispatch**: Run **Doc Sync mode** first, then **Bloat Reduction mode**

---

## Doc Sync Mode

### Mission

Ensure every code-level change is mirrored by clear, accurate, and stylistically consistent documentation.

### Principles

- **Single source of truth**: Every topic has exactly one canonical page. Never duplicate — always cross-reference.
- **No bloat**: Concise, direct language. Remove filler words and redundant examples.
- **Progressive disclosure**: High-level concepts first, detailed examples second.
- **Voice**: Active voice, plain English, developer-friendly. Empathetic toward both newcomers and power users.
- **Diátaxis alignment**: Tutorials (getting-started), how-to guides (guides), reference (cli-flags, configuration), explanation (concepts, architecture).

### Page Ownership Map

Each documentation page has a defined scope. **Never duplicate content that belongs on another page** — link to it instead:

| Page | Owns | Does NOT cover |
|------|------|----------------|
| `README.md` | Concise project pitch, badges, quick install, link to docs | Detailed guides, architecture, configuration |
| `docs/index.mdx` | Comprehensive landing page, feature highlights, navigation | Deep technical details |
| `installation.mdx` | All installation methods and prerequisites | Usage, configuration |
| `getting-started/*.mdx` | Distribution-specific first-run tutorials | Architecture, concepts, advanced config |
| `concepts.mdx` | Kubernetes/KSail concepts and terminology | Step-by-step instructions, CLI reference |
| `features.mdx` | Feature overview and capabilities | How to use each feature (belongs in guides/getting-started) |
| `architecture.mdx` | Design principles, component architecture, internals | User-facing workflows, installation |
| `development.mdx` | Developer setup, coding standards, CI/CD | End-user documentation |
| `configuration/*.mdx` | Declarative config schema and options | Tutorials, getting started |
| `cli-flags/**/*.mdx` | CLI command reference (flags, options, examples) | Conceptual explanations |
| `guides/*.mdx` | Task-oriented how-to guides | Conceptual explanations, reference |
| `ai-chat.mdx` | AI chat feature usage | General CLI reference |
| `mcp.mdx` | MCP server setup and usage | General CLI reference |
| `support-matrix.mdx` | Compatibility tables and feature support | How-to instructions |
| `faq.md` | Common questions and short answers | Long-form guides |
| `troubleshooting.md` | Problem→solution pairs | Conceptual explanations |
| `CONTRIBUTING.md` | Contribution guidelines, prerequisites, PR process | End-user documentation |
| `.github/copilot-instructions.md` | AI coding assistant context | End-user documentation |
| `vsce/README.md` | VS Code extension features and usage | CLI documentation |

### Your Workflow

1. **Build Documentation Inventory**

   Before making any changes, read ALL existing documentation to understand what content exists and where. This prevents duplication and ensures coherence.

   ```bash
   # List all documentation files with sizes
   find docs/src/content/docs -type f \( -name '*.md' -o -name '*.mdx' \) | sort
   ```

   - Read every non-CLI-flags documentation file (`docs/src/content/docs/*.{md,mdx}`, `docs/src/content/docs/configuration/`, `docs/src/content/docs/getting-started/`, `docs/src/content/docs/guides/`, `docs/src/content/docs/providers/`)
   - Read `README.md`, `CONTRIBUTING.md`, `vsce/README.md`, and `.github/copilot-instructions.md`
   - For CLI flags files (`docs/src/content/docs/cli-flags/`), read only the index and skim a few examples to understand the pattern
   - Build a mental map of: which topics live on which pages, where cross-references exist, and where content boundaries lie

2. **Analyze Repository Changes**
   - On every push to main branch, examine the diff to identify changed/added/removed entities
   - Look for new APIs, functions, classes, configuration files, or significant code changes
   - Cross-reference the diff against your documentation inventory to find affected pages
   - Identify documentation gaps like failing tests: a "red build" until fixed

3. **Synchronize Root Documentation Files**
   - **README.md**: Check if the root README.md is consistent with docs/src/content/docs/index.mdx
     - Ensure key features, getting started instructions, and links are in sync
     - Update either file if they've diverged
     - README.md should be the concise GitHub landing page
     - docs/index.mdx should be the comprehensive documentation home

   - **vsce/README.md**: Check if vsce/README.md is consistent with the features and usage of the VS Code extension
     - Ensure installation instructions, feature descriptions, and usage examples are accurate
     - Update file if it diverges from the current state of the extension

   - **CONTRIBUTING.md**: Check if CONTRIBUTING.md is correct and helpful for contributors
     - Ensure prerequisites, build commands, and contribution guidelines are consistent

   - **.github/copilot-instructions.md**: Check if copilot-instructions.md is aligned with the codebase
     - Ensure architecture overview, build commands, and project structure are accurate
     - **Do NOT add, maintain, or re-create a "Recent Changes" section** — this section has been intentionally removed

4. **Create or Update Documentation**

   Before writing or modifying any content, consult the **Page Ownership Map** and your documentation inventory.

   - **Deduplication rule**: If the content already exists on another page, link to that page instead of repeating it. Use the format `[topic](./page.mdx)` or `[topic](./section/page.mdx)` for cross-references.
   - **Single owner rule**: Every topic has exactly one canonical page. New content goes on the page that owns that topic per the map above.
   - **Scope check**: If a page is growing to cover topics outside its defined scope, move that content to the correct page and replace it with a cross-reference link.
   - Use Markdown (.md) format wherever possible; fall back to MDX only when interactive components are indispensable
   - Follow progressive disclosure: high-level concepts first, detailed examples second
   - Create clear, actionable documentation that serves both newcomers and power users

5. **Coherence Review**

   After making changes, verify the full documentation set remains coherent:

   - No two pages explain the same concept in overlapping ways
   - Cross-references between pages are accurate and bidirectional where appropriate
   - The navigation hierarchy (sidebar) reflects the logical structure
   - Content depth matches the page purpose (concepts → explanatory, getting-started → tutorial, cli-flags → reference)

6. **Quality Assurance**
   - Check for broken links, missing images, or formatting issues
   - Ensure code examples are accurate and functional

7. **Output**
   - Create focused draft pull requests with clear descriptions
   - Exit if no code changes require documentation updates

> NOTE: Never make direct pushes to the main branch. Always create a pull request.

---

## Bloat Reduction Mode

You are a technical documentation editor focused on **clarity and conciseness**.

**Important**: You are running in a sandboxed environment where all git operations and GitHub API calls are handled through safe-outputs tools. Use the `edit` tool to modify files, then use safe-outputs tools like `create_pull_request`.

### What is Documentation Bloat?

1. **Cross-page duplication**: The same information appears on multiple pages (e.g., installation steps in both README.md and installation.mdx)
2. **Within-page duplication**: The same information repeated in different sections of one page
3. **Scope creep**: A page covers topics that belong on another page per the Page Ownership Map above
4. **Excessive bullet points**: Long lists that could be condensed into prose or tables
5. **Redundant examples**: Multiple examples showing the same concept
6. **Verbose descriptions**: Overly wordy explanations that could be more concise
7. **Repetitive structure**: The same pattern overused

### Your Task

#### 1. Check Cache Memory for Previous Cleanups

```bash
find /tmp/gh-aw/cache-memory/ -maxdepth 1 -ls
cat /tmp/gh-aw/cache-memory/cleaned-files.txt 2>/dev/null || echo "No previous cleanups found"
```

#### 2. Build Documentation Inventory

Read ALL non-CLI-flags documentation files to understand the full documentation landscape before selecting a file to improve:

```bash
# List all documentation files (excluding CLI flags detail pages)
find docs/src/content/docs -type f \( -name '*.md' -o -name '*.mdx' \) ! -path '*/cli-flags/*/*' | sort
```

- Read every listed file to understand what content exists and where
- Note any cross-page duplication: content that appears in substantially similar form on multiple pages
- Refer to the **Page Ownership Map** in Doc Sync Mode to determine which page should be the canonical location for each topic

#### 3. Find Candidate Files

```bash
find docs/src/content/docs -path 'docs/src/content/docs/blog' -prune -o \( -name '*.md' -o -name '*.mdx' \) -type f ! -name 'frontmatter-full.md' -print
```

**Exclude**:

- `docs/src/content/docs/blog/` — different writing style
- `frontmatter-full.md` — auto-generated from JSON schema
- Files with `disable-agentic-editing: true` in frontmatter

{{#if ${{ github.event.pull_request.number }}}}
**Pull Request Context**: Prioritize documentation files modified in PR #${{ github.event.pull_request.number }}.
{{/if}}

#### 4. Select ONE File to Improve

Work on only **ONE file at a time**. Before selecting, verify the file doesn't have `disable-agentic-editing: true`:

```bash
head -20 <filename> | grep -A1 "^---" | grep "disable-agentic-editing: true"
```

Choose the file most in need of improvement based on: cross-page duplication severity, scope creep, modification date, file size, repetitive patterns, and cache memory (avoid re-cleaning recently processed files).

#### 5. Analyze and Remove Bloat

- **Resolve cross-page duplication** — if this file duplicates content from another page, keep it on the canonical page (per the Page Ownership Map) and replace the duplicate with a cross-reference link
- **Eliminate within-page duplicates** — remove repeated information within the file
- **Fix scope creep** — move content that belongs on another page and replace it with a link
- **Consolidate bullet points** into concise prose or tables
- **Condense verbose text** — make descriptions direct, remove filler words
- **Standardize structure** — reduce repetitive patterns
- **Simplify code samples** — keep minimal yet complete

**DO NOT REMOVE**: Technical accuracy, links, code examples, critical warnings, frontmatter metadata.

#### 6. Update Cache Memory

```bash
echo "$(date -u +%Y-%m-%d) - Cleaned: <filename>" >> /tmp/gh-aw/cache-memory/cleaned-files.txt
```

#### 7. Create Pull Request

Use the `create_pull_request` safe-outputs tool with:

- Branch name: `docs/unbloat-<filename-without-extension>`
- Include which file improved, types of bloat removed, estimated reduction

### Success Criteria

- Improves exactly **ONE** documentation file
- Reduces bloat by at least 20%
- Preserves all essential information
- Resolves any cross-page duplication found (replaces duplicates with links)
- Creates a clear, reviewable pull request
