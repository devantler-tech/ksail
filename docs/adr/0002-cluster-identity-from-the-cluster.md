# Cluster identity comes from the cluster, not from the kubeconfig context name

Status: Proposed in #7190.

KSail decides whether it manages a cluster by enumerating infrastructure providers and then matching
those names against kubeconfig context names through per-distribution patterns (`kind-<name>`,
`k3d-<name>`, `admin@<name>`, …). Both halves fail on a cluster KSail itself created.

Measured on the `devantler-tech/platform` production cluster (2026-09-22): it is a KSail-created
Talos cluster on Hetzner, reached through an OIDC context named `oidc@prod`. The desktop app
showed it as an unmanaged, unidentified cluster. Provider enumeration needs a Hetzner credential the
desktop process does not hold, so the managed set was empty; and even with that credential the
cluster would have been listed as `prod` while the context reads `oidc@prod`, which matches no
distribution pattern, so the same cluster would have appeared twice — once managed, once not. A
context name is a user-chosen string: an OIDC issuer, a colleague's naming scheme or a renamed entry
all break the match, and nothing about the match is verifiable.

A cluster can answer this question about itself. KSail therefore writes a cluster-identity marker
into the cluster it provisions — a `kube-system/ksail-cluster-info` ConfigMap holding the KSail
cluster name, distribution, provider, the KSail version that wrote it, and the cluster's identity —
and surfaces read that marker over the Kubernetes API. This follows the pattern Kubernetes tooling
already uses: `kubeadm` records `kube-system/kubeadm-config`, and the `kube-system` namespace UID is
the de-facto cluster identifier. Reading a ConfigMap is an ordinary API call, needs no provider
credential, and works through whatever context, proxy or auth plugin the user has configured, so
KSail stays native to the Kubernetes workflow.

The marker carries the `kube-system` namespace UID it was written against. A marker whose recorded
UID does not match the live one was copied or restored into a different cluster and is ignored. This
makes the marker verifiable against the cluster serving it, which a context name never was. The
comparison needs a second read: the ConfigMap names its namespace but not that namespace's UID, so
KSail also needs `get` on the cluster-scoped `kube-system` Namespace. A user who may read the
ConfigMap but not the Namespace holds a marker KSail cannot verify, and KSail treats it as
unverified: management is reported as unknown, never as managed.

The UID check catches a marker copied into another cluster, not a full etcd snapshot restore: a
snapshot restores the `kube-system` Namespace with its UID, so the recorded and live values still
match. The marker therefore identifies a logical cluster, and a control plane restored from a full
snapshot is the same logical cluster as its source. Running a restored clone alongside its source
is a deliberate identity split, and the clone must be re-stamped with a new cluster identity
through KSail's explicit adopt path before it is used. Differing API servers alone do not tell a
clone from one cluster exposed through several endpoints (internal and public load balancers, an HA
endpoint, a local proxy). When two reachable contexts serve the same cluster identity through
different API servers, read surfaces treat them as endpoints of one cluster only when provider
discovery or persisted ownership state binds both endpoints to that cluster, and report a
duplicate-identity conflict for both when that evidence places them on different infrastructure.
When no evidence decides it, both entries are listed separately and marked as sharing an
unverified identity, neither merged nor reported as a conflict.

The kubeconfig keeps the role it is good at: enumerating what the user can reach and connecting to
it. It stops being the source of truth for what a cluster *is*. Whenever KSail reads a valid marker
through a context, it records that context-to-cluster mapping locally, bound to the kubeconfig
cluster entry the context points at: its API server URL and the fingerprint of its certificate
authority, plus the stable identity provider discovery or persisted ownership state reports for
that cluster (for example the provider's cluster or server IDs). A cluster that is stopped or
serving no API is joined to its context through that last-known mapping only while the context
still points at the same server and CA and the recorded stable identity still holds. Live provider
evidence decides that whenever the provider can be queried: when it no longer reports the recorded
identity, KSail discards the mapping, whatever the local record says. Only when the provider cannot
be queried does the persisted record keep the join, and the context is then marked as last-known
rather than verified. A last-known mapping only joins a context to a cluster for display; it is not
ownership state, and on its own it never authorizes an update or delete. The endpoint and CA alone never suffice, because a cluster recreated
behind the same address can present both again; without a matching stable identity, the context
is treated as not yet identified. When the context
is repointed, its cluster entry changes, or the entry disappears, KSail discards the mapping and
treats the context as not yet identified, since context names are mutable and reusable. Where no
mapping has been
recorded, KSail lists the provider-discovered cluster and the unreachable context separately, each
marked as not yet identified, rather than guessing a join from the context name.

The marker asserts provenance, not privilege. Anything with write access to `kube-system` can forge
it, including its name and provider, so it never selects the target of a destructive action.
Lifecycle operations resolve what to act on only from trusted records: provider discovery with the
user's credentials, or KSail's persisted ownership state. A context is used for such an operation
only when that trusted record binds the intended cluster to the exact API-server endpoint and
certificate authority the context points at. KSail rejects an update or delete when the binding is
absent or either value differs. The CA fingerprint check and a TLS handshake against that CA stay
necessary, but on their own they prove only that the endpoint holds a key the CA trusts; a CA can be
shared or reused, so they do not identify the intended cluster. For the same reason the endpoint
and CA alone are not enough either: a cluster recreated behind the same address can present them
again. The trusted record therefore also carries a stable identity for the cluster from provider
discovery or persisted ownership state (for example the provider's cluster or server IDs), and
KSail rejects a context-based update or delete when the provider no longer reports that identity
behind the endpoint, even if the endpoint and CA are unchanged.

The binding gates only operations that reach the cluster through a context. A delete that provider
discovery resolves and that acts only through the provider's API — removing servers, load balancers
and volumes — never talks to the cluster, so it needs no binding and still works when the cluster is
stopped or its control plane is broken. A cluster whose trusted record predates this decision has
no recorded endpoint or CA. KSail fills that gap only from a source it already trusts: the
provider's own report of the cluster's endpoint and CA (for example the EKS API), or the kubeconfig
KSail itself wrote when it created the cluster and recorded in its persisted state. It writes the
binding before the first update, and never copies it from whatever context the user happens to have
selected. When no trusted source can supply it, KSail refuses the update and names the missing
binding rather than guessing. Data the cluster reports about
itself — the marker, node labels, or a node's `providerID` — never counts as that binding, because
an administrator of a hostile cluster can set any of it to a victim's known values. A forged marker can
therefore make a hostile cluster look like a KSail cluster in read surfaces, but it cannot steer a
delete or update onto real infrastructure. The marker's purpose is to stop KSail mislabelling
clusters and to let read surfaces resolve identity without credentials.

Three fallbacks remain, in order. A cluster with a valid marker is managed and identified by it. A
cluster without one is classified from its nodes — OS image, kubelet version, well-known labels and
annotations, and the `providerID` scheme (#7179) — which tells KSail its distribution and provider,
not which cluster it is: several clusters of one distribution look the same, and `providerID` names
a machine, not a cluster. Its identity comes only from provider or persisted evidence; without
either, KSail reports the identity as unknown, and its management is decided by the rules below.
A cluster that serves no API at all is left to provider enumeration and the last-known mapping,
which is also what continues to reveal clusters that exist but are stopped.

A marker the API reports as absent (`404 Not Found`) means the cluster carries no marker, not that
KSail did not create it. KSail reports it as unmanaged only when no other positive evidence of
ownership exists. Provider discovery or persisted state that identifies the cluster as one KSail
created keeps it managed, and KSail writes the missing marker on its next create or update. Any
other failed read, including `403 Forbidden` from a user without `get` in `kube-system`, says
nothing about who created the cluster: KSail reports management as unknown — still classified from
its nodes where they are readable, and with the missing permission named — and never as unmanaged.
It does not fall back to context-name patterns to fill the gap, since those are what this decision
retires as a source of identity.

Clusters created before this marker existed do not have one. They keep the classification their
provider or persisted evidence gives them, gain the marker on the next KSail create or update, and
an explicit adopt path lets a user stamp one without a provisioning run. A cluster with neither a
marker nor other ownership evidence reads as unmanaged: its distribution and provider are
classified from its nodes, and its cluster identity is reported as unknown.

Rejected alternatives: keeping context-name patterns (unverifiable, and measured wrong on a real
cluster); relying only on provider enumeration (invisible without credentials, which is exactly how
the production cluster disappeared); node heuristics alone (they describe what a cluster runs, never
who created it); and a CRD or the operator's own `Cluster` resource (both require KSail components
to be installed in every cluster, while the question must be answerable for a cluster running
nothing of ours).

References:

- [kubeadm's `kube-system/kubeadm-config`](https://kubernetes.io/docs/reference/setup-tools/kubeadm/implementation-details/)
- [Namespace UID as a cluster identifier](https://github.com/kubernetes/kubernetes/issues/44954)
