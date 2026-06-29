# ADR-0001: KSail Headlamp plugin — home, scope, and auth model

- Status: Proposed
- Date: 2026-06-29
- Tracking issue: #5321 ("Contribute a KSail plugin for the Headlamp Kubernetes UI")
- Roadmap: #4988 (KSail strategy & roadmap)

## Context

KSail already ships two first-party UI surfaces — the `ksail-ui` React/Vite SPA
(`web/ui`, embedded via `pkg/webui`) and the desktop wrapper (`desktop/`) — both backed by
the operator REST API (`pkg/operator`) over the `Cluster` CRs reconciled by
`internal/controller`.

[Headlamp](https://headlamp.dev) (maintained under
[`kubernetes-sigs/headlamp`](https://github.com/kubernetes-sigs/headlamp)) is an extensible
Kubernetes web UI with a large installed base. Its
[plugin system](https://headlamp.dev/docs/latest/development/plugins/) lets third parties
extend the UI with TypeScript/React plugins that register sidebar entries, routes, app-bar
actions, and resource-detail tabs. Plugins are built with the `@kinvolk/headlamp-plugin`
tooling, packaged to a `.tar.gz`, and discovered through Artifact Hub (plugin kind 21).
Official plugins live in [`headlamp-k8s/plugins`](https://github.com/headlamp-k8s/plugins).

A KSail Headlamp plugin is an ecosystem-integration / distribution play that is
complementary to — not a replacement for — `ksail-ui`: it puts the KSail-specific layer
(`Cluster` CRs, GitOps bootstrap, component lifecycle) in front of users who already run
Headlamp and would not install a second dashboard.

Issue #5321 (AC#1 and AC#4) requires deciding, before any code, **where the plugin lives**,
**what v1 surfaces**, and **how it reaches the KSail operator API**. This ADR records those
decisions. It is the first increment of #5321; subsequent ACs (scaffold, packaging/CI, docs)
build on the choices made here.

## Decision

### 1. Home — in-repo `headlamp-plugin/` workspace (not a separate repo)

The plugin lives in this repository as a self-contained `headlamp-plugin/` workspace,
mirroring how `web/ui` and `desktop/` are co-located today.

- The plugin's entire value is the **KSail-specific contract** against the operator REST API
  (`pkg/operator`). Co-locating it keeps that contract and the plugin **co-evolving in one
  atomic change** under one CI; a separate `devantler-tech/ksail-headlamp-plugin` repo would
  drift from the operator API and need a release-coupling dance on every contract change.
- It matches the existing precedent (`web/ui`, `desktop/`) and `.mega-linter.yml` already
  scopes self-contained front-end toolchains out of the repo-wide linters (`web/ui` is
  excluded), so the workspace can own its `tsc + @kinvolk/headlamp-plugin` build the same way.
- Submission to `headlamp-k8s/plugins` and/or Artifact Hub is a **distribution step layered on
  top** (AC#5 / stretch AC), not a reason to externalize the source. The in-repo workspace
  produces the packaged `.tar.gz`; publishing it upstream does not require the source to live
  upstream.

If the workspace later proves to warrant independent release cadence, extraction to its own
repo stays an option — but starting in-repo keeps the contract honest while the API is still
moving.

### 2. v1 scope — read-only

v1 is **read-only**: a KSail sidebar section with a cluster overview (distribution, provider,
GitOps engine, component status) and a GitOps view, both sourced from the operator REST API /
`Cluster` CR status. Write actions (create/update/delete/reconcile) are **deferred** to a
later increment, exposed only where the operator already exposes them and strictly behind the
operator's existing auth. Read-only keeps v1 additive, low-risk, and free of a new
authorization surface, while delivering the headline ecosystem-reach value immediately.

The plugin must **not duplicate Headlamp built-ins** (Headlamp already renders core K8s
objects); it focuses on the KSail-specific layer only.

### 3. Auth / connection model — through Headlamp's in-cluster proxy

The plugin reaches the operator REST API via **Headlamp's own backend proxy**
(`request` / the cluster-scoped proxy helpers) targeting the in-cluster `ksail-operator`
Service, rather than calling the operator endpoint directly from the browser.

- This **avoids CORS** entirely — the browser only ever talks to Headlamp's origin, and
  Headlamp proxies to the in-cluster Service.
- It **reuses the cluster credentials Headlamp already holds**, so the plugin inherits the
  operator's existing auth rather than introducing a second credential path.
- For the desktop / out-of-cluster case, the same proxy abstraction is used; only the operator
  Service address differs (documented in the install page, AC#6).

The exact operator endpoints the plugin consumes are confirmed against `pkg/operator` when the
read path is implemented (AC#3); this ADR fixes the *transport* decision, not the endpoint
list.

### 4. Roadmap placement

Track the Headlamp plugin under **#5321 as the epic**, decomposed into per-AC children
(this ADR is the first). If the maintainer prefers a roadmap theme, open *"Theme F — UI &
ecosystem integration"* under #4988 to group it with the `ksail-ui` / desktop surfaces; that
is an organizational choice and does not change the technical decisions above.

## Consequences

- **Positive:** one repo, one CI, one operator-API contract; additive and low-risk; reuses the
  `web/ui`/`desktop/` co-location precedent; no new Go runtime dependencies; no CORS surface.
- **Negative / cost:** a third front-end surface to keep in sync with the operator API. This is
  accepted because the plugin deliberately surfaces only the thin KSail-specific layer (not a
  full dashboard), and co-location means an API change updates plugin and operator together.
- **Follow-up ACs (#5321):** scaffold the `headlamp-plugin/` workspace with
  `@kinvolk/headlamp-plugin` (AC#2); implement the read path against the confirmed operator
  endpoints (AC#3); wire `artifacthub-pkg.yml` + `npm run package` into CI to publish a
  versioned `.tar.gz` (AC#5); add an install/usage docs page under
  `docs/src/content/docs/integrations/` (AC#6); submit to Artifact Hub / `headlamp-k8s/plugins`
  as a stretch step (AC#7).

## Alternatives considered

- **Dedicated `devantler-tech/ksail-headlamp-plugin` repo.** Rejected for v1: it decouples the
  plugin from the operator-API contract it depends on, adding cross-repo release coupling for
  every contract change while the API is still evolving. Revisit if independent release cadence
  becomes necessary.
- **Direct browser → operator API calls.** Rejected: introduces CORS handling and a second
  credential path in the browser, for no benefit over Headlamp's existing proxy.
- **Write-capable v1.** Rejected: a shared-dashboard create/delete path needs an explicit
  authorization story; read-only v1 ships the ecosystem-reach value without that surface.
