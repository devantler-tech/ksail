---
title: Ephemeral Admission Checks
description: Check workload admission against an isolated Kind cluster with the declared operators installed.
---

`ksail workload validate --ephemeral` and `ksail workload scan --ephemeral` add Kubernetes API admission checks to their normal offline checks. The mode is experimental and off by default. It requires a running Docker daemon and creates a separate Kind cluster with its own temporary kubeconfig.

```bash
# Select the overlay you intend to deploy.
ksail workload validate ./k8s/overlays/test --include-crd-schemas --ephemeral
ksail workload scan ./k8s/overlays/test --ephemeral
```

The offline validation or security scan runs first. If it fails, KSail does not create a cluster. For valid input, KSail applies namespaces and CRDs, waits for CRD establishment, prepares ConfigMaps and Secrets, installs declared Helm charts and waits for their workloads and jobs, then submits the remaining resources through server-side apply with strict field validation. After chart installation, KSail also reapplies the bootstrap resources to check their updates against newly installed policies. An API rejection fails the command and identifies the affected resource.

The admission phase has one ten-minute deadline shared by its API operations and chart installations. Cluster deletion runs with a separate two-minute deadline, including after cancellation or admission failure. Cleanup errors are reported together with the original failure. The user's existing clusters and kubeconfig are not used.

## Select one workload

Pass a single Kustomize root, a YAML file, or a directory containing plain YAML manifests. When the selected directory is a Kustomize root, its build output drives both chart installation and resource application, so the chosen overlay's values and patches are used. KSail does not separately apply that root's bases.

A directory containing nested Kustomize roots without a root of its own is ambiguous: select the desired overlay explicitly. Duplicate resource identities, YAML `List` wrappers, and Flux `Kustomization` descriptors are rejected. Expand lists into separate YAML documents and select the workload directory referenced by a Flux `Kustomization`.

The selected input must contain no `${...}` expressions, including literal shell expressions in ConfigMaps or Secrets. The experimental mode conservatively rejects these expressions because it cannot distinguish them from unresolved Flux substitutions. The synthetic values used by offline schema validation are not deployment values. An unresolved chart source or values reference also fails the admission preparation instead of silently reducing coverage.

HelmRelease declarations and their HelmRepository/OCIRepository sources are consumed by Helm installation. Chart-generated resources are not separately reapplied by KSail. ConfigMaps and Secrets needed by charts must be present in the selected workload, together with Namespace declarations for their non-builtin namespaces. Preparation fails before provisioning if these namespaces are missing. Helm post-renderers are unsupported and rejected. Charts install in the order they appear in the selected manifest stream; KSail does not infer Flux `dependsOn` ordering.

## What a successful check proves

Success means the selected resources passed the offline gate and their apply requests were accepted by the throwaway API server. The admission path submits every directly declared resource, including kinds excluded from the offline scan or schema pass. Bootstrap resources are created before chart installation and checked again as updates afterward; policies scoped only to creation cannot retroactively check their initial creation.

This does not prove that arbitrary operators finished reconciling or that all resources they create are valid or secure. Inventory and validation of operator-generated children are tracked separately in the [ephemeral validation roadmap](https://github.com/devantler-tech/ksail/issues/5919). The isolated cluster also does not reproduce external services, cloud permissions, production admission policies, or production secrets unless they are explicitly declared in the selected input.
