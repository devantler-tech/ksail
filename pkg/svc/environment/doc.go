// Package environment is the foundation for `ksail project add-environment
// <name> --from <env>`, which clones an existing cluster environment overlay so
// adding an environment to a multi-cluster ksail repository stops being a manual
// `cp -R k8s/clusters/<env> k8s/clusters/<new>` + hand-edit recipe. It is part of
// the multi-cluster / multi-provider GitOps scaffolding epic; see ksail#5441.
//
// # Design decision: structured rewrites, not string replace
//
// Cloning an overlay is mostly a verbatim copy. A cluster environment differs
// from its sibling only by a handful of structured values — the cluster-meta
// ConfigMap's cluster_name / provider, and the clusters/<env>/ path segment — and
// the replacements: block plus base wiring are byte-identical across environments.
// A naive strings.ReplaceAll(srcName, dstName) over the copied bytes would corrupt
// unrelated tokens: an environment named "local" also appears in "localhost", in
// the config.kubernetes.io/local-config annotation, and in arbitrary prose. So the
// clone must rewrite specific, structured locations and leave everything else
// byte-identical.
//
// This package provides the rewrite primitives and the directory-clone
// orchestrator, fully unit-testable with no CLI surface:
//
//   - [Rewrite] describes one structured substitution.
//   - [DeriveRewrites] computes what changes between two environments.
//   - [RewriteOverlayFile] applies those rewrites to one cloned file's relative
//     path and contents, preserving every untargeted byte.
//   - [CloneOverlay] walks a source overlay directory and writes the rewritten
//     clone, copying SOPS *.enc.yaml files byte-for-byte (path still repointed)
//     and honouring fsutil's force/skip semantics.
//
// # Phased delivery
//
// The command is large, so it ships in independently-valuable slices:
//
//   - Foundation: the structured-rewrite primitives — what changes between two
//     environments and how it is applied to one file. Pure logic, fully
//     unit-tested, needed by every later slice regardless of the eventual walk and
//     CLI (ksail#5562).
//   - Directory clone ([CloneOverlay]): the walk + write that clones
//     k8s/clusters/<from>/** to k8s/clusters/<name>/** honouring fsutil's
//     force/skip semantics, with SOPS *.enc.yaml copied as-is (ksail#5567).
//   - Next: the ksail.<env>.yaml root-config repoint (name, context, config
//     paths), the cobra `project add-environment` command, and its
//     generated-artifact refresh (cli-flags docs, help/toolgen snapshots).
package environment
