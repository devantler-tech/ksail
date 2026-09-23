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

The kubeconfig keeps the role it is good at: enumerating what the user can reach and connecting to
it. It stops being the source of truth for what a cluster *is*. Whenever KSail reads a valid marker
through a context, it records that context-to-cluster mapping locally. A cluster that is stopped or
serving no API is joined to its context through that last-known mapping. Where no mapping has been
recorded, KSail lists the provider-discovered cluster and the unreachable context separately, each
marked as not yet identified, rather than guessing a join from the context name.

The marker asserts provenance, not privilege. Anything with write access to `kube-system` can forge
it, including its name and provider, so it never selects the target of a destructive action.
Lifecycle operations resolve what to act on only from trusted records: provider discovery with the
user's credentials, or KSail's persisted ownership state. A context is used for such an operation
only when an identifier held on the infrastructure side matches the live cluster — for example a
node's `providerID` matching a server the provider reports for that cluster. A forged marker can
therefore make a hostile cluster look like a KSail cluster in read surfaces, but it cannot steer a
delete or update onto real infrastructure. The marker's purpose is to stop KSail mislabelling
clusters and to let read surfaces resolve identity without credentials.

Three fallbacks remain, in order. A cluster with a valid marker is managed and identified by it. A
cluster without one is identified from its nodes — OS image, kubelet version, well-known labels and
annotations, and the `providerID` scheme (#7179) — and its management is decided by the rules below.
A cluster that serves no API at all is left to provider enumeration and the last-known mapping,
which is also what continues to reveal clusters that exist but are stopped.

A marker the API reports as absent (`404 Not Found`) means the cluster carries no marker, not that
KSail did not create it. KSail reports it as unmanaged only when no other positive evidence of
ownership exists. Provider discovery or persisted state that identifies the cluster as one KSail
created keeps it managed, and KSail writes the missing marker on its next create or update. Any
other failed read, including `403 Forbidden` from a user without `get` in `kube-system`, says
nothing about who created the cluster: KSail reports management as unknown — still identified from
its nodes where they are readable, and with the missing permission named — and never as unmanaged.
It does not fall back to context-name patterns to fill the gap, since those are what this decision
retires as a source of identity.

Clusters created before this marker existed do not have one. They keep the classification their
provider or persisted evidence gives them, gain the marker on the next KSail create or update, and
an explicit adopt path lets a user stamp one without a provisioning run. A cluster with neither a
marker nor other ownership evidence reads as unmanaged-but-identified, as it does today.

Rejected alternatives: keeping context-name patterns (unverifiable, and measured wrong on a real
cluster); relying only on provider enumeration (invisible without credentials, which is exactly how
the production cluster disappeared); node heuristics alone (they describe what a cluster runs, never
who created it); and a CRD or the operator's own `Cluster` resource (both require KSail components
to be installed in every cluster, while the question must be answerable for a cluster running
nothing of ours).

References:

- [kubeadm's `kube-system/kubeadm-config`](https://kubernetes.io/docs/reference/setup-tools/kubeadm/implementation-details/)
- [Namespace UID as a cluster identifier](https://github.com/kubernetes/kubernetes/issues/44954)
