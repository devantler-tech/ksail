# Design: operator-driven component-install lifecycle

Status: **proposal (for review)** · Owner: KSail operator · Scope: `internal/controller`, `pkg/operator`, `pkg/svc/installer`, `pkg/svc/provisioner/cluster`

## 1. Problem & goal

The CLI's `ksail cluster create` does two things: (a) **provision** the bare cluster, then
(b) **install components** — CNI, CSI, CDI, metrics-server, cert-manager, load-balancer,
policy-engine, and the GitOps engine — according to the `Cluster` spec.

The **operator today only does (a)**: `ClusterReconciler.reconcileNormal` calls
`provisioner.Create/Exists/Update` and never installs components. So a `Cluster` resource that
sets `spec.cluster.cni: Cilium` (or `gitOpsEngine: Flux`, etc.) is provisioned but those
components are never installed.

**Goal:** make the operator reconcile the *full* desired state, installing/maintaining the
components declared in the spec, idempotently and safely within the reconcile loop.

## 2. Building blocks that already exist

- **`pkg/svc/installer.Factory`** (`NewFactory(helmClient, dockerClient, kubeconfig, kubecontext, timeout, distribution)`)
  with **`CreateInstallersForConfig(cfg *v1alpha1.Cluster) (map[string]Installer, error)`**. It
  already picks the right installer per spec enum (CNI=Cilium/Calico/None, GitOps=Flux/ArgoCD, …).
- **`installer.Installer`** interface: `Install(ctx) error`, `Uninstall(ctx) error`, `Images(ctx) ([]string, error)`.
- **`helm.NewClient(kubeconfigPath, kubeContext) (*helm.Client, error)`** — a helm client bound to a
  kubeconfig **file path** + context. Helm install/upgrade is internally idempotent.
- **`pkg/svc/detector`** — already used by the CLI `update` path to detect installed Helm releases.

These are service-layer packages (no Cobra/CLI coupling), so the operator can import them directly.

The CLI's *orchestration* (`pkg/cli/setup.InstallCNI` / `InstallPostCNIComponents`) is `*cobra.Command`-
coupled and **will not be reused**; we re-implement a thin, reconcile-friendly runner.

## 3. The crux: obtaining a client to the *child* cluster

Installers run against the **provisioned (child)** cluster, not the hub. The operator must obtain a
kubeconfig/helm client for the child. Today the `Provisioner` interface exposes **no** way to get
this. Access differs per distribution × provider:

| Distribution × Provider | How the child is reached | Feasible from operator now? |
|---|---|---|
| **VCluster + Kubernetes** | `vc-<name>` Secret in `vcluster-<name>` ns holds a kubeconfig; rewrite server → in-cluster Service `https://<name>.vcluster-<name>.svc:443`, `tls-server-name: kubernetes` (same trick the status observer uses) | **Yes** |
| Vanilla/Talos/KWOK + Kubernetes (DinD) | API server runs in a DinD pod, exposed via Gateway API `TCPRoute`/NodePort; child kubeconfig must be extracted from the DinD pod / a published Secret | Needs plumbing |
| K3s + Kubernetes (k3k) | k3k publishes a kubeconfig Secret for the virtual cluster | Needs plumbing |
| any + Docker | kind/k3d write a kubeconfig to a path on the host's Docker network; not reachable from a hub pod without `DOCKER_HOST` + network routing | Needs plumbing |
| EKS + AWS / Talos + Hetzner/Omni | kubeconfig obtained from the cloud API using operator-mounted credentials | Needs plumbing |

**Proposed long-term contract** — extend the provisioner with an optional capability interface so
the operator gets child access uniformly instead of special-casing:

```go
// pkg/svc/provisioner/cluster
type Connector interface {
    // Kubeconfig returns a kubeconfig for the provisioned cluster, reachable from where the
    // operator runs (in-cluster Service address, cloud endpoint, etc.).
    Kubeconfig(ctx context.Context, name string) ([]byte, error)
}
```

Provisioners implement `Connector` per distribution. The operator type-asserts
`provisioner.(Connector)`; if absent, component install is skipped for that distribution (logged).

**Status: Phase 2 implemented.** The `Connector` interface lives in
`pkg/svc/provisioner/cluster` and the vcluster `KubernetesProvisioner.Kubeconfig` implements it
(reading the `vc-<name>` Secret via its host clientset and rewriting the server to the in-cluster
Service address). The operator's `InstallComponents` receives the provisioner and type-asserts
`Connector` — the vcluster Secret special-case that Phase 1 carried in `pkg/operator` is gone. A
shared `clustererr.ErrKubeconfigNotReady` sentinel signals "child not published yet → retry".

## 4. Reconcile model (idempotency)

The reconcile loop runs every ~60s (steady) / ~10s (transitional). We must **not** re-run helm on
every tick. Gate component install on desired-state changes + readiness, mirroring the existing
drift-detection pattern (`ksail.io/last-applied-spec`).

- Add a status condition **`ComponentsReady`** and a baseline annotation
  **`ksail.io/last-applied-components`** (hash of the component-relevant spec subset:
  cni, csi, cdi, metricsServer, loadBalancer, certManager, policyEngine, gitOpsEngine, node counts).
- Run the installer sequence when **either**: `ComponentsReady != True`, **or** the component hash
  differs from the annotation (spec changed). On success → set `ComponentsReady=True`, write the
  hash. On failure → `ComponentsReady=False` + `Degraded=True`, requeue at the transitional interval
  (retries until it succeeds; covers the window where the child API is not yet reachable).
- Helm's install-or-upgrade is idempotent, so re-runs after a partial failure are safe.

Reconcile flow becomes:

```
reconcileNormal:
  provisioner.Exists / Create / drift-Update      (unchanged)
  observeStatus                                    (unchanged; endpoint/nodes)
  if componentsNeedApply(cluster):
      installComponents(ctx, cluster)              # best-effort; sets ComponentsReady / Degraded
  markReady (only if ComponentsReady)              # gate Ready on components when applicable
```

## 5. Ordering / phases

Mirror the CLI's phases (the installer map is unordered, so we impose order):

1. **CNI** first (pods can't schedule until networking exists).
2. **Infrastructure** (parallelizable, but sequential is simpler/safer in the operator):
   CSI, metrics-server, cert-manager, load-balancer, policy-engine.
3. **GitOps** (Flux/ArgoCD) last, after the rest is stable.

Each step is best-effort: collect per-component errors, continue, and report an aggregate. A failed
component sets `Degraded` with the component name + message and triggers requeue.

## 6. Failure handling & status

- Component-install failure **does not** fail provisioning — the cluster is still "provisioned"; it
  is reported `Degraded` with `ComponentsReady=False` and requeued.
- Surface a per-component summary in `status` (e.g. `status.components: {cni: Ready, gitops: Failed}`)
  so the UI can show component health. (UI already has a detail drawer to render this.)

## 7. Security / RBAC

- The operator already runs with broad hub RBAC (chart `rbac.create`).
- Child installs authenticate with the child cluster's admin credentials from its kubeconfig — no
  new hub permissions required. Temp kubeconfig files are written under `os.MkdirTemp` and removed
  after each run.

## 8. Testing strategy

- **Unit:** component-hash gating (apply/skip/re-apply on change), ordering, no-op for distributions
  without a `Connector`, error aggregation. Fakes for the installer set.
- **Integration (local):** VCluster + Kubernetes with a **lightweight** component (e.g.
  `metricsServer: Enabled` or `gitOpsEngine: Flux`) to validate the child-kubeconfig → helm →
  install path end-to-end. Heavy CNIs (Cilium) are validated in CI, not the kind-in-kind dev hub.

## 9. Phased rollout

- **Phase 1 — VCluster + Kubernetes** (smallest verifiable slice) — **done**: special-cased the
  `vc-<name>` Secret for child access; implemented the condition-gated runner + ordering + status;
  reused `installer.Factory`. Unit tests + a lightweight in-cluster verify.
- **Phase 2 — `Connector` interface** — **done (vcluster)**: added `Kubeconfig(ctx, name)` to the
  provisioner contract and implemented it on the vcluster `KubernetesProvisioner`; generalized the
  operator to use the interface instead of the vcluster special case. The other Kubernetes-provider
  distros (Kind DinD, k3k, KWOK, Talos DinD) implement `Connector` next — each needs its own child
  kubeconfig source (DinD pod / published Secret) and is unverifiable on the kind-in-kind dev hub
  without the Gateway API CRDs, so they land as their environments become testable.
- **Phase 3 — Docker provider**: requires the operator pod to reach the host Docker network
  (`DOCKER_HOST` + routable API); document the deployment requirement.
- **Phase 4 — Cloud (EKS/Hetzner/Omni)**: obtain kubeconfig via cloud APIs using mounted credentials.

## 10. Open questions

1. Should `Ready` be gated on `ComponentsReady`, or stay independent (cluster Ready = provisioned,
   components a separate condition)? Proposed: cluster reports `Ready` for provisioning and a
   distinct `ComponentsReady`; `Degraded` reflects component failures.
2. Reconcile-time vs one-shot: do we ever **uninstall** a component when the spec flips it off
   (e.g. `policyEngine: Kyverno → None`)? Proposed: yes, drive uninstall from the diff in a later
   phase; Phase 1 only installs/upgrades.
3. Where does per-component `status` live — inline in `ClusterStatus` or a separate sub-resource?
4. Timeout/backoff per component (helm timeouts can be minutes); ensure they fit the requeue cadence.
