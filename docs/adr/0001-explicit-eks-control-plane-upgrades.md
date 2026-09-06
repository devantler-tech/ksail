# Explicit EKS control-plane upgrades

Status: Proposed in #6925, under EKS roadmap #4328.

An EKS upgrade changes the managed control plane independently of worker nodes and add-ons. KSail exposes this capability only when `spec.cluster.eks.experimentalControlPlaneUpgrade` is true. An explicit `spec.cluster.kubernetesVersion` selects one forward minor step; no registry discovery or recreation path is used.

The upgrade uses AWS SDK UpdateClusterVersion with readiness enforcement intact. The eksctl cluster-upgrade command also repairs cluster-stack resources and can change remote networking, so it exceeds this operation's scope. The factory carries the immutable credential/transport snapshot and ownership verifier into the upgrader.

KSail validates the requested step before dry-run output. At execution it rereads the ACTIVE cluster, rejects stale starting versions, verifies the bound cluster identity, submits one version request and polls that exact update ID. Completion requires a Successful version update plus the same cluster ACTIVE at the target version. Failure, cancellation, incomplete evidence and the bounded wait all return errors. Worker readiness and add-on compatibility are not inferred from control-plane completion.

The API uses major.minor; the shared orchestration uses equivalent vMajor.minor.0 values. Nonzero patches, prereleases, skipped minors and rollback requests are refused. AWS rollback support is outside this forward-upgrade capability.

The flag remains default-off pending disposable AWS-cluster validation and separate graduation.

The SDK can resolve service endpoints lazily from the process environment. KSail snapshots endpoint presence and values with the credential/configuration freeze, preserves the SDK source interfaces and applies the resolved EKS/STS endpoints after SDK environment resolution and before explicit service options. This prevents SDK environment-precedence checks from retargeting a frozen operation while retaining caller overrides (#6929).

The AWS version API has no conditional expected-version parameter. A final identity/status/version reread narrows external-operation races; it cannot make the read and submission atomic. Live graduation must exercise concurrency and document this boundary.

References:

- [AWS UpdateClusterVersion](https://docs.aws.amazon.com/eks/latest/APIReference/API_UpdateClusterVersion.html)
- [AWS update guidance](https://docs.aws.amazon.com/eks/latest/userguide/update-cluster.html)
- [eksctl owned-cluster upgrade](https://github.com/eksctl-io/eksctl/blob/6154cdc220369e6e061c5cda4ef4eb04be8e1973/pkg/actions/cluster/owned.go#L65)
