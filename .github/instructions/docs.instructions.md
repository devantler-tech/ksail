---
description: "Use when editing Astro documentation pages, MDX content, or Starlight configuration in the docs/ directory. Covers documentation structure, Diátaxis framework, and build commands."
applyTo: "docs/**"
---
# Documentation Conventions (Astro + Starlight)

## Build & Preview

```bash
cd docs && npm ci && npm run build   # Validate build (dist/)
cd docs && npm run dev               # Local preview
```

CI uses Node.js 24. Documentation builds in ~2-3 seconds.

## Content Structure

Documentation follows the [Diátaxis framework](https://diataxis.fr/):
- **Tutorials** — Learning-oriented walkthroughs
- **How-to guides** — Task-oriented procedures
- **Reference** — CLI flags, API types, configuration schemas
- **Explanation** — Architecture, design decisions

## CLI Docs Generation

CLI flag reference pages are auto-generated. See `docs/doc.go` and `docs/gen_docs_prose.go` for the generator. Do not manually edit generated pages.

## Conventions

- Prefer Markdown (`.md`) over MDX (`.mdx`) unless interactive components are needed
- Use the write-docs agent (`/write-docs`) for documentation updates that sync across README.md, index.mdx, and CONTRIBUTING.md
