# Debug Kubernetes Failure Action

A GitHub composite action that outputs diagnostic information for debugging Kubernetes cluster failures. Shows disk usage, node status, pod status, recent events, and GitOps resource statuses (FluxInstance, OCIRepository, ArgoCD Application).

## Why?

When Kubernetes workloads fail in CI, it's often due to resource constraints (disk pressure, memory) or pod scheduling issues. This action provides a standardized way to collect debugging information when failures occur.

## Usage

```yaml
steps:
  - name: Deploy application
    id: deploy
    run: kubectl apply -f manifests/

  - name: Debug on failure
    if: failure() && steps.deploy.outcome == 'failure'
    uses: ./.github/actions/debug-kubernetes-failure
```

### With Custom kubectl Command

```yaml
- name: Debug on failure
  if: failure()
  uses: ./.github/actions/debug-kubernetes-failure
  with:
    kubectl-command: "ksail workload"
```

### With Namespace Filter

```yaml
- name: Debug on failure
  if: failure()
  uses: ./.github/actions/debug-kubernetes-failure
  with:
    namespace: "my-app"
```

### Selective Output

```yaml
- name: Debug on failure
  if: failure()
  uses: ./.github/actions/debug-kubernetes-failure
  with:
    show-disk-usage: "false"
    show-nodes: "true"
    show-pods: "true"
    show-events: "true"
    events-limit: "100"
```

## Inputs

| Input             | Description                           | Default   |
|-------------------|---------------------------------------|-----------|
| `kubectl-command` | The kubectl command or wrapper to use | `kubectl` |
| `show-disk-usage` | Show host and Docker disk usage       | `true`    |
| `show-nodes`      | Show node status and conditions       | `true`    |
| `show-pods`       | Show pod status                       | `true`    |
| `show-events`     | Show recent cluster events            | `true`    |
| `events-limit`    | Number of recent events to show       | `50`      |
| `namespace`       | Namespace to query (empty for all)    | `""`      |

## Example Output

```text
=== Disk Usage ===
Filesystem      Size  Used Avail Use% Mounted on
/dev/root        84G   63G   21G  75% /

=== Docker Disk Usage ===
TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE
Images          15        5         4.2GB     2.1GB (50%)
Containers      8         3         150MB     100MB (66%)
Local Volumes   5         2         500MB     300MB (60%)

=== Node Status ===
NAME                  STATUS   ROLES           AGE   VERSION   INTERNAL-IP   ...
local-control-plane   Ready    control-plane   10m   v1.33.1   172.18.0.2    ...

=== Node Describe (conditions) ===
Conditions:
  Type             Status  ...  Reason                       Message
  ----             ------       ------                       -------
  MemoryPressure   False        KubeletHasSufficientMemory   ...
  DiskPressure     False        KubeletHasNoDiskPressure     ...
  PIDPressure      False        KubeletHasSufficientPID      ...
  Ready            True         KubeletReady                 ...

=== Pod Status ===
NAMESPACE     NAME                          READY   STATUS    RESTARTS   AGE
default       my-app-abc123                 1/1     Running   0          5m
kube-system   coredns-abc123                1/1     Running   0          10m

=== Pod Events ===
NAMESPACE   LAST SEEN   TYPE      REASON      OBJECT              MESSAGE
default     2m          Normal    Scheduled   pod/my-app-abc123   Successfully assigned...
default     2m          Normal    Pulled      pod/my-app-abc123   Container image pulled...

=== FluxInstance Status ===
NAMESPACE     NAME   AGE   READY   STATUS
flux-system   flux   10m   True    Flux installation is ready

=== FluxInstance Details ===
Status:
  Conditions:
    Last Transition Time:  2024-01-22T07:20:00Z
    Message:               Reconciliation finished, next run in 1h0m0s
    Reason:                ReconciliationSucceeded
    Status:                True
    Type:                  Ready

=== OCIRepository Status ===
NAMESPACE     NAME          URL                                          AGE   READY   STATUS
flux-system   flux-system   oci://ghcr.io/org/repo/manifests             10m   True    stored artifact for revision 'latest@sha256:abc...'

=== OCIRepository Details ===
Status:
  Artifact:
    Digest:         sha256:abc123...
    Last Update Time: 2024-01-22T07:20:00Z
    Path:           /data/flux-system/flux-system/artifact.tar.gz
    Revision:       latest@sha256:abc123...
    URL:            http://source-controller/ocirepository/flux-system/flux-system/artifact.tar.gz
  Conditions:
    Last Transition Time:  2024-01-22T07:20:00Z
    Message:               stored artifact for revision 'latest@sha256:abc123...'
    Reason:                Succeeded
    Status:                True
    Type:                  Ready

=== ArgoCD Application Status ===
NAMESPACE   NAME     SYNC STATUS   HEALTH STATUS   AGE
argocd      my-app   Synced        Healthy         5m

=== ArgoCD Application Details ===
Status:
  Health:
    Status:  Healthy
  Sync:
    Status:   Synced
  Conditions:
    Last Transition Time:  2024-01-22T07:25:00Z
    Message:               Application synced successfully
    Type:                  SyncSucceeded
  Operation State:
    Phase:     Succeeded
    Message:   successfully synced (all tasks run)
```

## Common Failure Patterns

| Symptom                                  | Likely Cause                                                             |
|------------------------------------------|--------------------------------------------------------------------------|
| `DiskPressure: True`                     | Runner out of disk space - use `free-disk-space` action                  |
| `MemoryPressure: True`                   | Not enough RAM for workloads                                             |
| Pods in `Pending`                        | Insufficient resources or node selector issues                           |
| Pods in `ImagePullBackOff`               | Image not found or registry authentication failed                        |
| Pods in `CrashLoopBackOff`               | Application crashing - check container logs                              |
| FluxInstance not `Ready`                 | Check conditions for reconciliation errors or OCIRepository not synced   |
| OCIRepository not `Ready`                | Check conditions for artifact fetch errors or authentication failures    |
| OCIRepository condition `ArtifactFailed` | Artifact not pushed, wrong tag, or registry unreachable                  |
| ArgoCD Application not `Synced`          | Check sync status and conditions for manifest errors or Git issues       |
| ArgoCD Application not `Healthy`         | Check health status - deployed resources may be unhealthy or progressing |
| ArgoCD Application operation `Failed`    | Check operation state message for sync or apply errors                   |
