---
title: "ksail workload describe"
parent: "ksail workload"
grand_parent: "CLI Flags Reference"
---

# ksail workload describe

```text
Show details of a specific resource or group of resources.

Usage:
  ksail workload describe

Examples:
  # Describe a node
  ksail workload describe nodes kubernetes-node-emt8.c.myproject.internal
  
  # Describe a pod
  ksail workload describe pods/nginx
  
  # Describe a pod identified by type and name in "pod.json"
  ksail workload describe -f pod.json
  
  # Describe all pods
  ksail workload describe pods
  
  # Describe pods by label name=myLabel
  ksail workload describe pods -l name=myLabel
  
  # Describe all pods managed by the 'frontend' replication controller
  # (rc-created pods get the name of the rc as a prefix in the pod name)
  ksail workload describe pods frontend

Flags:
  -A, --all-namespaces     If present, list the requested object(s) across all namespaces. Namespace in current context is ignored even if specified with --namespace.
      --chunk-size int     Return large lists in chunks rather than all at once. Pass 0 to disable. (default 500)
  -f, --filename strings   Filename, directory, or URL to files containing the resource to describe
  -h, --help               help for describe
  -k, --kustomize string   Process the kustomization directory. This flag can't be used together with -f or -R.
  -R, --recursive          Process the directory used in -f, --filename recursively. Useful when you want to manage related manifests organized within the same directory.
  -l, --selector string    Selector (label query) to filter on, supports '=', '==', '!=', 'in', 'notin'.(e.g. -l key1=value1,key2=value2,key3 in (value3)). Matching objects must satisfy all of the specified label constraints.
      --show-events        If true, display events related to the described object. (default true)

Global Flags:
      --timing   Show per-activity timing output
```
