---
title: "ksail workload create source git"
parent: "ksail workload create source"
grand_parent: "ksail workload create"
---

# ksail workload create source git

```text
Create or update a GitRepository source using Flux APIs

Usage:
  ksail workload create source git [name] [flags]

Examples:
  # Create a source from a public Git repository master branch
  ksail workload create source git podinfo \
    --url=https://github.com/stefanprodan/podinfo \
    --branch=master

  # Create a source for a Git repository pinned to specific git tag
  ksail workload create source git podinfo \
    --url=https://github.com/stefanprodan/podinfo \
    --tag="3.2.3"

  # Create a source from a Git repository using SSH authentication
  ksail workload create source git podinfo \
    --url=ssh://git@github.com/stefanprodan/podinfo \
    --branch=master \
    --secret-ref=git-credentials

Flags:
      --branch string       git branch
      --commit string       git commit
      --export              export in YAML format to stdout
  -h, --help                help for git
      --interval duration   source sync interval (default 1m0s)
      --secret-ref string   the name of an existing secret containing SSH or basic credentials
      --tag string          git tag
      --tag-semver string   git tag semver range
      --url string          git address, e.g. ssh://git@host/org/repository

Global Flags:
      --timing   Show per-activity timing output
```
