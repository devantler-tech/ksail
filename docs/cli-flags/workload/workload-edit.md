---
title: "ksail workload edit"
parent: "ksail workload"
grand_parent: "CLI Flags Reference"
---

# ksail workload edit

```text
Edit a Kubernetes resource from the default editor.

The editor is determined by (in order of precedence):
  1. --editor flag
  2. spec.editor from ksail.yaml config
  3. KUBE_EDITOR, EDITOR, or VISUAL environment variables
  4. Fallback to vim, nano, or vi

Example:
  ksail workload edit deployment/my-deployment
  ksail workload edit --editor "code --wait" deployment/my-deployment

Usage:
  ksail workload edit [flags]

Flags:
      --editor string   editor command to use (e.g., 'code --wait', 'vim', 'nano')
  -h, --help            help for edit

Global Flags:
      --timing   Show per-activity timing output
```
