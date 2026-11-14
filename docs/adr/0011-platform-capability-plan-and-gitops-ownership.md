# 0011: Resolve platform capabilities once and preserve GitOps ownership

- Status: Accepted design; implementation pending
- Date: 2026-10-04
- Related: #6875–#6882, #7511–#7513

## Context

KSail exposes cluster component selections through `spec.cluster`. The CLI and operator use existing
installer and connector mechanisms, while GitOps controllers can own installed releases. These
mechanisms cover individual cluster components; they do not provide a complete production platform,
composable presets or a universal adopter override contract.

The product direction in #6875 requires a configurable platform from built-in configuration, with
every managed component still extensible and overridable through GitOps. Existing cluster-only
configuration and the CLI, desktop, web and operator interfaces remain supported. The reviewed
reference inventory in #6877 supplies component verdicts, scoped alternatives and dependencies;
deployment-specific accounts and applications remain adopter-owned.

The main design risks are independent defaulting paths, implicit dependency activation, mutable
presets, competing writers and destructive cleanup inferred from labels. A render can also look
complete while omitting unavailable inputs or differing from its owning engine's values semantics.

## Decision

### One presence-aware plan

Add an optional sibling `spec.platform` section for production capabilities, catalogue selection,
presets and component settings. Preserve the existing scalar shapes and meanings of `spec.cluster`.
The deprecated `nodeAutoscaling` alias remains an alias of `autoscaler.node.enabled`.

Use one service-layer resolved plan for all interfaces. A legacy adapter maps existing fields to
stable capability IDs such as `cluster.cni`; new capabilities use IDs such as
`platform.secretsManagement`. Presets may assign supported paths in both sections. The operator
consumes the same plan and renderer as the CLI.

The proposed platform API includes an experimental gate, exact catalogue revision, exact preset
versions, typed slot objects, stable-ID component overrides and optional adopter GitOps sources.
These are design fields, not configuration accepted by the current implementation.

New platform behavior starts default-off. An absent platform section preserves cluster-only
behavior. Nonempty selections with the gate off fail before mutation. New commands use the existing
`experimental.Guard` convention. Disabling a gate does not uninstall a capability or make existing
cleanup inaccessible.

Presence is captured before defaults: omitted, explicit `None`, false, zero and empty collections
are distinct. An omitted new slot requests nothing unless a selected preset supplies it. A present
slot object with no implementation inherits its preset assignment, or requests the selected
catalogue's recommendation if none exists. Explicit `None` defeats a preset. Existing enums retain
their current `Default`, Enabled/Disabled and distribution-supplied meanings.

### Capabilities contain a dependency graph

A slot represents an adopter-facing capability, potentially composed of several controllers and
wiring resources. Its typed implementation enum includes `None` and supported choices. The selected
immutable catalogue defines exact bundle/chart/image pins, implementation settings, compatibility,
required interfaces, component ordering, readiness evidence and ownership identities.

The secrets slot is a composite: store, synchronization and conditional trust distribution. Its
reference-derived choice is OpenBao plus External Secrets and conditional trust-manager; an existing
authenticated store is a scoped alternative. Store installation, independent bootstrap/configuration,
usable store, bindings and workload consumption are separate phases. The first bootstrap credentials
cannot depend on the uninitialized store supplying them.

Validate the complete effective graph before writes. Refuse unsupported versions or implementations,
irrelevant settings, missing or explicitly disabled dependencies, incompatible provider interfaces,
cycles and object-identity collisions. Dependencies belong to reviewed catalogue data; an adopter
cannot supply arbitrary edges to bypass them. A preset may supply a dependency transparently.

A failed dependency leaves its dependents unattempted/converging with a reason. Independent branches
may continue, but aggregate failure remains visible. Installed manifests or healthy controllers do
not prove the capability's promised behavior. External interfaces also require authenticated
behavioral evidence; declaring a store, address or metrics interface is insufficient.

### Presets are immutable data

Ship granular reviewed presets beside the catalogue. They contain assignments expressible through
the same public paths and exact pins, with no hidden resources or executable hooks. Initial
composition is a flat list without nested presets.

Disjoint assignments combine and identical assignments coalesce. Conflicts fail with both origins
and the exact path, unless an explicit higher-precedence user value resolves the complete conflict.
Lists replace as a whole. Preset order does not decide priority.

An active platform selects an exact catalogue revision and exact preset versions. Scaffolding may
write the bundled recommended revision. Record selected revisions, effective values, component pins
and plan digest as the applied baseline. A binary upgrade does not advance them. Changed content
gets a new version and a visible effective-plan diff; unavailable old content produces a refusal.

### Configuration and resource precedence are separate

Desired configuration resolves from lowest to highest precedence:

1. Defaults of the selected immutable catalogue and existing distribution behavior.
2. Composed preset assignments, with explicit conflict handling.
3. Explicit `ksail.yaml` / `Cluster.spec` fields, including per-component overrides.
4. Explicit CLI environment overrides.
5. Changed CLI flags.

The operator uses stages 1–3; its process environment cannot reinterpret an adopter's resource.
Every effective path records its origin. Switching implementation discards incompatible settings
from the previous implementation.

Managed resources resolve from the versioned generated base to the adopter's declared GitOps
overlay. The overlay is authoritative for manifest fields, including fields exposed by convenient
KSail settings. A reconcile or changed CLI flag updates the base, then reapplies the overlay; it
never rewrites the adopter's overlay.

Patches target exact group/kind/namespace/name identities. Missing or ambiguous targets fail.
Extensions may add new identities but cannot silently replace managed ones. Arrays use explicit
replacement or JSON patch operations. Removing a patch is explicit, not guessed from an empty value.

Proposed CLI output is an immutable base and provenance inventory under
`<sourceDirectory>/platform/generated/<planDigest>/`, composed with user-owned `platform/overlays/`.
Scaffolding supplies the initial empty overlay and connects the entry point without replacing
unrelated workloads. Existing generated content is verified before reuse; traversal, unsafe symlinks
and digest mismatches fail before writes. The operator uses equivalent in-memory rendering and
immutable artifacts rather than depending on a CLI-host path.

Bootstrap and steady-state consume the same resolved values. Temporary bootstrap seeds transfer
ownership once to the selected GitOps controller. KSail does not continue direct Helm mutation of
GitOps-owned identities. Active platform slots require a supported GitOps engine; an explicit
`gitOpsEngine: None` is a dependency error. Ordinary cluster-only configurations with None remain valid.

### Ownership, removal and upgrade

Component overrides can select `Managed` or `External` ownership. `External` is a non-destructive
handoff: preserve live objects, transfer responsibility, and stop KSail desired-state, version,
values and deletion writes for those identities. The external owner and component controllers may
continue normal reconciliation. Remaining components stay managed.

`None` requests removal only of positively proven KSail-owned resources, in reverse dependency order.
Stateful removal requires an explicit retention/cleanup decision. Ambiguous ownership, unmet dependent
consumers and incomplete cleanup cannot be called successful removal. A catalogue or preset no longer
mentioning an external component does not authorize its deletion. Labels alone grant no destructive
authority, consistent with ADR 0002.

Upgrade compares the prior generated base, proposed base and declared overlay. A changed overridden
field, disappearing/type-changing target or incompatible dependency surfaces a conflict and preserves
the prior applied component revision. Resolution requires an updated overlay or acknowledgement bound
to exact old/new/overlay digests. Unchanged overrides are reapplied on every reconcile. Partial failure
cannot mark a new baseline applied.

Immutable persistent-resource changes require an explicit migration/retention plan. Refuse forced
delete/recreate or conflicting overlays before writes. The current reference's prune protections and
two-stage retirement remain. Engine-specific generated, template and rendered controls must be
examined completely; the reference source guard is not a universal certificate for arbitrary chart
output, Argo behavior or live data retention. Storage Retain is not backup/restore proof.

### Authenticated built-in content

Embed the reviewed catalogue, presets, wiring templates and complete component render inputs needed
for built-in configuration. Exact chart versions need authenticated chart content/digests; an image
digest does not authenticate a separately downloaded chart. Retain source, license and provenance
with exact generated/published artifact identities.

Built-in composition does not implicitly fetch the reference repository, an adopter repository or
mutable latest configuration at provision time. The built-in path needs no external configuration
repository. Images and explicitly selected provider/secret services may still require a network;
this decision does not promise an air-gapped platform.

Optional explicitly selected adopter Git/OCI sources may be fetched and bind a revision and path.
Component-version overrides
appear in the plan and do not inherit the bundled version's compatibility certificate. Credentials
remain private inputs/references; previews and diagnostics redact resolved secret values.

Preserve engine-native values semantics. Ordinary Flux `valuesFrom` merges in list order, followed
by inline values; `targetPath` retains its documented special precedence. Argo and Flux need separate
qualification of equivalent effective resources. Degraded rendering or a valid CR cannot certify
complete resource coverage, consistent with ADR 0004.

## Worked design examples

These examples describe proposed API and illustrative catalogue/bundle versions. They are not
runnable current KSail configuration or published capability versions.

For an existing Cilium selection, the generated base may set operator replicas to 2 while an adopter
overlay sets the exact HelmRelease path `/spec/values/operator/replicas` to 3. Reconcile preserves 3.
If an explicit catalogue upgrade changes the base to 4, the old 2 / new 4 / adopter 3 conflict is
surfaced and the old applied revision remains until resolved. Taking external ownership preserves
Cilium and stops KSail's writes while the other components remain managed.

For a secrets preset, the selected exact catalogue expands the composite into store, synchronization
and conditional trust components with explicit pins and bootstrap/readiness dependencies. Explicit
`None` defeats the preset and produces a reviewed removal plan; `External` produces preservation
and handoff. Retaining a managed consumer while removing its required store is refused unless a
compatible replacement is declared. Acceptance observes store secret → Kubernetes Secret → workload
consumption, rather than treating successful Helm calls as the capability.

## Consequences and delivery

- Existing cluster config remains compatible, while production capabilities have a distinct section.
- One resolver/renderer prevents interface-specific defaults and engines from defining separate
  catalogues. Capturing raw presence and retaining origin/digest information increases implementation
  complexity but preserves explicit adopter intent.
- Complete embedded chart/render inputs and supported old revisions increase release size and
  maintenance. They make exact pins and preflight previews meaningful without a runtime config service.
- GitOps overlays and external ownership provide full escape paths. Three-way conflicts and explicit
  stateful retention favor recoverable refusals over silent overwrite or destructive convenience.
- #7512 delivers the shared default-off schema/plan, including explicit HPA/KEDA admission semantics.
  #7513 delivers authenticated inputs and deterministic complete rendering. #7511 repairs existing
  offline Flux values-reference precedence before parity is claimed.
- #6878 proves override, extension, handoff, reconcile and upgrade/removal on real resources. #6879
  proves both secrets paths and retention. #6880 delivers at least two working immutable presets.
  #6881 detects complete-source drift without auto-sync. #6882 adds multi-target operator convergence,
  reusing delivered #4899 mechanisms; shared hosted tenancy/auth/quota remain separate work.
- All new behavior stays default-off and is tested in both states. Current-head CI/review and actual
  user/provider-path evidence remain required for delivery. This record archives a design decision;
  it implements no new API, preset, platform slot or runtime behavior.

Rejected alternatives are moving existing fields, one slot per chart, implicit dependency activation,
mutable/latest or ordered/nested presets, independent operator/engine catalogues, live patches outside
desired GitOps state, competing controllers, `None` as handoff, and an external platform repository
as the built-in runtime configuration service.

## References

- [Schema and ownership decision](https://github.com/devantler-tech/ksail/issues/6876#issuecomment-5983706455).
- [Capability catalogue and exact source inventory](https://github.com/devantler-tech/ksail/issues/6877#issuecomment-5984361226).
- [Flux values references](https://fluxcd.io/flux/components/helm/helmreleases/#values-references).
- [Flux immutable-field replacement](https://fluxcd.io/flux/components/kustomize/kustomizations/#force).
