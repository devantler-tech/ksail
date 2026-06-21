# KSail: The Best Kubernetes UI — Strategy & Implementation Plan

> Status: **proposal / planning doc** (not yet ratified). Goal: make KSail a credible
> **alternative to Headlamp** — full feature parity, a **Headlamp‑plugin‑compatible**
> extension system, KSail's own **cluster‑first dive‑in** information architecture, and the
> ability to **manage and operate clusters, workloads, and tenants via both UI and AI**.

This document is the output of an investigation into (a) whether KSail can support Headlamp
plugins, (b) whether doing so is a licensing/policy problem, and (c) how to reach and exceed
Headlamp feature parity on KSail's own stack.

---

## 1. Decisions taken (inputs to this plan)

| Decision | Choice | Rationale |
|---|---|---|
| **Extension model** | **Headlamp‑plugin compatible** | Inherit the existing Headlamp plugin ecosystem; let third‑party plugins load unmodified. |
| **Path to parity** | **Extend KSail's own UI** (React + Tailwind + Headless UI) | Keep the lean, Go‑native, design‑consistent stack and KSail's distinct product identity — *not* embed Headlamp's frontend. |
| **First milestone scope** | **All four pillars**: resource browser · AI‑operated UI · cluster‑first IA · plugin SDK | Ship a thin vertical slice across all four, then deepen each. |

The first two decisions are in **deliberate tension** — Headlamp‑compatible plugins normally
require Headlamp's *entire* frontend runtime (Material UI + Redux + React Router + its K8s data
layer), which is exactly what "extend KSail's own UI" says we don't want to adopt wholesale.
**Resolving that tension cleanly is the central architectural idea of this plan** (§4).

---

## 2. Policy & licensing verdict (cleared)

**Supporting Headlamp plugins is not against any policy or license.**

| | KSail | Headlamp |
|---|---|---|
| License | **PolyForm Shield 1.0.0** — source‑available; commercial use OK; only bars *products that compete with KSail*; **no copyleft on your own deps** ("use any license for your own project") | **Apache‑2.0** — OSI‑approved, permissive, no copyleft, includes a patent grant |
| Governance | devantler‑tech | **CNCF Sandbox** + now hosted under **`kubernetes-sigs`** (Kubernetes SIG UI); latest **v0.43.0**, **pre‑1.0** |

Because Apache‑2.0 is permissive and PolyForm Shield imposes no copyleft, KSail may legally:

1. **Clean‑room reimplement** the Headlamp plugin API (the recommended route — APIs are
   reimplementable; *Google v. Oracle* reinforces this in the US). **Lowest risk.**
2. **Vendor** Headlamp's plugin‑loader source directly into KSail's UI.
3. **Bundle / redistribute** Headlamp itself.

**Obligations & caveats (none blocking):**

- **Attribution** — if any Apache‑2.0 source is vendored (routes 2/3), retain its license header
  and `NOTICE`, and record it in the existing **`THIRD_PARTY_LICENSES`** file (established pattern
  in this repo). Mark modified files as changed.
- **Trademark** — do **not** brand anything "Headlamp" (CNCF/LF marks). *Nominative* use
  ("compatible with Headlamp plugins") is fine; productized name/logo use is not.
- **Third‑party plugin licenses** — community plugins on Artifact Hub carry heterogeneous licenses
  (MIT/Apache/GPL/unspecified). Safe pattern: **load at runtime, don't redistribute them**, and
  surface each plugin's declared license to the user before install.
- **Security model** — Headlamp plugins run **unsandboxed**, in the app's JS context, with full
  access to the user's cluster credentials. Adopting that model inherits a real supply‑chain risk;
  §4.4 proposes mitigations (trust gate + optional sandbox) that would make KSail *safer* than
  Headlamp here.

---

## 3. Where KSail stands today vs Headlamp

### 3.1 KSail UI today (`web/ui/`, `pkg/operator/api/`)

A polished but **narrow cluster‑lifecycle manager**:

- **Frontend**: React 19 + Vite 7 + Tailwind 4 + Headless UI + Lucide; raw `fetch`; **no router**,
  **no Redux/MUI**; single view + slide‑overs; 10s polling. (`web/ui/src/App.tsx`, `web/ui/src/api.ts`)
- **Backend**: Go `api.Server` (stdlib `net/http`, Go 1.22 routing) exposing **only** Cluster‑CR
  CRUD + `/meta` + OIDC auth. (`pkg/operator/api/server.go`)
- **Two service backends** behind one `ClusterService` interface: CR‑backed (operator,
  `cr_service.go`) and local‑lifecycle (`ksail ui`/desktop, `pkg/cli/clusterapi/local_service.go`).
- **No** resource browsing, logs, exec, port‑forward, YAML editing, metrics, multi‑cluster
  switching, plugins, or AI‑in‑UI.

### 3.2 Headlamp (the parity target)

Full resource browser (core + CRDs), multi‑cluster (WebSocket multiplexer: one browser socket →
many apiserver sockets), logs, exec/terminal, port‑forward, **Monaco** YAML edit/apply,
create/delete/scale, metrics charts (Recharts), global + label search, Helm release management,
notifications, dark mode/theming, i18n, OIDC + token auth, an Activities task‑bar, a Projects
(app‑centric) view, and an extensible **plugin system** with an in‑app catalog.

### 3.3 Feature‑parity matrix

| Capability | KSail today | Headlamp | Plan phase |
|---|---|---|---|
| Cluster lifecycle (create/update/delete) | ✅ **(KSail‑unique)** | ❌ | exists |
| GitOps bootstrap (Flux/ArgoCD) | ✅ CLI; ❌ UI | ❌ | P2 |
| Multi‑tenancy (tenant onboarding) | ✅ CLI; ❌ UI | ❌ | P2 |
| SOPS / secret cipher | ✅ CLI; ❌ UI | partial | P2 |
| Resource browser (workloads, config, net, RBAC, CRDs) | ❌ | ✅ | P1 |
| Events / describe | ❌ | ✅ | P1 |
| Log streaming | ❌ | ✅ | P1 |
| Metrics charts | ❌ | ✅ | P1 |
| YAML view + edit/apply (Monaco) | ❌ | ✅ | P1/P2 |
| Create / delete / scale / rollout | ❌ | ✅ | P2 |
| Exec terminal | ❌ | ✅ | P2 |
| Port‑forward | ❌ | ✅ | P2 |
| Multi‑cluster switcher | ❌ | ✅ | P1 |
| AI assistant (explain **and act**) | ❌ UI (CLI chat exists) | partial (plugin) | P3 **(KSail‑unique depth)** |
| Plugin system | ❌ | ✅ | P4 |
| Headlamp‑plugin compatibility | ❌ | n/a | P4 |
| OIDC auth | ✅ | ✅ | exists |
| i18n / theming | partial (dark mode) | ✅ | P5 |
| Desktop app | ✅ (Wails v3) | ✅ (Electron) | P5 |

**Takeaway:** KSail already owns the layer *above* Headlamp (lifecycle, GitOps, tenancy, AI tool
surface). The gap is essentially "build a tasteful in‑cluster resource browser," plus the plugin
host. That gap is large but well‑understood.

---

## 4. Target architecture

The design reconciles "Headlamp‑compatible plugins" with "our own UI" by treating them as **two
layers that converge on one native registry**, rather than adopting Headlamp's app.

```
KSail Web UI (own stack: React + Tailwind + Headless UI)
  ├─ Cluster-first IA · Resource browser · AI assistant · GitOps/Tenant/Cipher views
  ▼ reads
KSail Extension Registry (native)  ◀── Headlamp-compat runtime (window.pluginLib)  ◀── Headlamp plugin main.js
  ▼ both UI and AI act through
Action layer: toolgen tools (cluster/workload/tenant/cipher) + Chat (Copilot SDK)
  ▼ calls
Go backend · api.Server  =  Control plane (Cluster CRs, exists)  +  Data plane (NEW)
                                                                    proxy · watch-mux · logs · exec · port-forward
  ▼ reuses pkg/client in-process
Kubernetes clusters: Kind · K3d · Talos · vCluster · KWOK · EKS
```

### 4.1 Backend — a new "cluster data plane" (Go, reuses `pkg/client` in‑process)

Today the backend is a *control plane* (Cluster‑CR CRUD). Parity needs a *data plane* that talks
to the workloads **inside** each cluster. New endpoints on `api.Server`, all Go‑native:

- **Generic authenticated K8s proxy**: `/api/v1/clusters/{ns}/{name}/proxy/{k8s-path...}` →
  passthrough to the target cluster's kube‑apiserver. Deliberately mirror Headlamp's
  `/clusters/{cluster}/...` shape so the Headlamp `ApiProxy` contract (§4.3) maps with minimal glue.
- **Watch multiplexer**: one client connection fans out to many upstream watches.
  - KSail already has an **SSE streaming substrate** on `api.Server` — use it for KSail‑native views.
  - **Headlamp plugins expect a WebSocket multiplexer** — add a Headlamp‑protocol‑compatible
    `wsMultiplexer` endpoint for the compat layer.
- **Log streaming** (`.../pods/{pod}/log?follow`), **exec** (WebSocket ↔ client‑go
  `remotecommand`/SPDY, xterm.js on the front end), **port‑forward** (client‑go).
- **Discovery + RBAC**: API‑group/resource discovery and `SelfSubjectAccessReview` so the UI can
  hide unauthorized actions.
- **Credential / context resolution**: reuse `pkg/svc/detector/cluster` + kubeconfig helpers.
- **AuthZ**: for the multi‑user *operator* deployment, pass the user's token / impersonate so
  per‑user RBAC is honored (today the operator acts with its own RBAC). For local `ksail ui`
  (loopback) the user's own kubeconfig is the identity.

### 4.2 Frontend — KSail‑native resource browser (own stack)

- **Introduce routing** (React Router or a minimal router; none exists today) implementing the
  **cluster‑first IA**: `/` cluster picker → `/c/{cluster}` workspace (Overview = cluster home) →
  `/c/{cluster}/{group}/{kind}` list → `/c/{cluster}/{group}/{kind}/{ns}/{name}` detail; logs/exec
  as "activities"/tabs. **One** cluster switcher (no per‑view selectors).
- **Data hooks**: `useResourceList` / `useResource` / `useWatch` over the proxy + stream endpoints;
  shared `table.tsx` / `EventList.tsx` primitives (reused everywhere to keep jscpd at 0).
- **Generic resource framework** driven by discovery: typed columns/detail for common kinds
  (Pods, Deployments, …), generic fallback for arbitrary CRDs. **Monaco** for YAML view/edit.
- **Advanced surfaces**: log viewer (multi‑pod, follow), xterm.js exec terminal, metrics charts
  (Recharts), search (incl. label search), namespace selector.

### 4.3 Extension layer — native registry + Headlamp‑compat facade

This is how we get "runs Headlamp plugins unmodified" without becoming a Headlamp fork:

1. **KSail Extension Registry (native)** — an internal registry of extension points: sidebar
   entries, routes, detail‑view sections, resource‑table column processors, app‑bar actions,
   cluster chooser, themes, plugin settings, **and AI tools**. KSail's *own* first‑party features
   (GitOps, Tenants) can be authored against this same registry.
2. **Headlamp loader** — replicate Headlamp's mechanism: backend serves a `/plugins` listing and
   each plugin's `main.js`; the frontend fetches each bundle as text and executes it via a
   `Function` constructor with a `pluginLib` injected. Honor Headlamp's install model
   (`-plugins-dir`, `~/.config/Headlamp/plugins` layout, Artifact Hub `artifacthub-pkg.yml`
   annotations: `archive-url`, `archive-checksum`, `version-compat`, `distro-compat`) so existing
   plugin tarballs drop in.
3. **`window.pluginLib` compat runtime** — a **lazily‑loaded** module (only pulled when a plugin is
   present, so KSail's own UI stays lean) exposing the exact externals Headlamp plugins bind to:
   React, ReactDOM, React Router, React‑Redux, **MUI** (`@mui/material`/`lab`/`styles`), Monaco,
   Recharts, Lodash, Iconify, Notistack — **plus** Headlamp's own `K8s`, `ApiProxy`, `Crd`,
   `Router`, `Registry`, `Notification`, `CommonComponents`, `Utils`, `Activity`.
4. **The critical sub‑project — `K8s`/`ApiProxy` parity**: reimplement Headlamp's *frontend* K8s
   data API (`K8s.useList`, `K8s.ResourceClasses`, `K8s.useApiGet`, `ApiProxy.request/stream/
   apiFactory`, the `KubeObject` classes) on top of KSail's proxy/stream endpoints. **This is the
   largest unknown and the critical path** for plugin compatibility.
5. **`register*` mapping** — Headlamp's `registerSidebarEntry`, `registerRoute`,
   `registerDetailsViewSection`, `registerResourceTableColumnsProcessor`, `registerAppBarAction`,
   `registerClusterChooser`, `registerAppTheme`, `registerPluginSettings`, `registerUIPanel`,
   `registerHeadlampEventCallback`, the `Headlamp.setCluster`/`setAppMenu` helpers, and the
   Activity API — all map onto the **native registry** in (1). Native and Headlamp plugins thus
   feed one registry.

### 4.4 Plugin trust & safety (improve on Headlamp)

Because plugins are unsandboxed by default, add: an **install consent** ("runs with full cluster
access"), an **allowlist**, optional **checksum/signature verification** (Artifact Hub already
provides `archive-checksum`), and — as a differentiator — an **optional iframe/Web‑Worker sandbox**
for untrusted plugins. This makes KSail *safer than Headlamp* out of the box.

### 4.5 AI layer — operate via UI **and** AI on one action path

KSail already has a unified action surface: **`pkg/toolgen`** auto‑generates typed tools from the
Cobra command tree, consumed by both the **MCP server** (`pkg/svc/mcp`) and **Copilot chat**
(`pkg/svc/chat`) — `cluster_*`, `workload_*`, `tenant_*`, `cipher_*`. The UI plugs into the *same*
surface:

- **Backend AI endpoint** — expose `pkg/svc/chat` over an **SSE chat endpoint** on `api.Server`.
- **Context‑aware assistant panel** (KSail‑native) — knows the selected cluster/namespace/resource;
  can **explain** (read tools) and **act** (write tools) behind a **diff‑preview → confirm** gate.
- **One action path** — an AI proposal ("scale deploy/foo to 3", "apply this YAML", "reconcile
  Flux") renders as the *same* previewed operation a UI button would trigger, then executes via the
  *same* backend handler. UI and AI never diverge.
- **Inline entry points** — "Ask AI about this resource" / "Diagnose this failing pod" seed the
  assistant with context from any view.

---

## 5. Phased roadmap

Each phase is independently shippable. Phase 0 is a thin vertical slice that de‑risks all four
pillars at once (per the "first milestone = all four" decision); later phases deepen each.

### Phase 0 — Foundation / vertical slice (de‑risk the architecture)
- Backend: generic **read‑only** K8s proxy + discovery for one cluster; reuse `pkg/client`.
- Frontend: introduce the **router** + cluster‑first shell; one resource **list (Pods)** + detail +
  YAML view; a **stub AI panel** (read‑only chat over SSE); a **stub native registry** with one
  example extension point wired.
- Outcome: every pillar proven end‑to‑end; architecture validated before scale‑out.

### Phase 1 — Read‑only resource browser parity
- Full resource coverage (workloads, config, storage, networking, RBAC, nodes, **CRDs**), events,
  describe; **log streaming**; **metrics** charts; search/label‑search; namespace selector;
  **multi‑cluster switcher**; the watch multiplexer (SSE for native; lay groundwork for WS).

### Phase 2 — Write operations + cluster‑first depth (KSail's differentiation surfaces)
- YAML **edit/apply**, create/delete/**scale/rollout**, **exec** terminal, **port‑forward**;
  RBAC‑aware action gating.
- **Overview** cluster‑home; **GitOps** views (Flux/ArgoCD — reuse `pkg/svc/detector/gitops`);
  **Tenant** onboarding + **Cipher/SOPS** views; surface **lifecycle** actions (create/update/delete).

### Phase 3 — AI‑operated UI (deepen the differentiator)
- Full assistant: act‑with‑confirm, context seeding from any resource, diff previews, the
  toolgen write‑tool bridge; "diagnose"/"explain" inline actions.

### Phase 4 — Plugin system (native → Headlamp‑compatible)

> **Foundation landed (this PR):** backend `PluginService` (`/api/v1/plugins` list + asset serving
> from `~/.ksail/plugins`, capability‑gated, path‑traversal‑safe); a native **extension registry**
> exposing the Headlamp `register*` surface; a **CSP‑safe loader** (same‑origin classic `<script>`,
> not `eval`, so no CSP relaxation) plus the `window.pluginLib` facade (React + register\* + a real
> minimal `K8s.useResourceList` over KSail's resource API); rendered seams for **sidebar entries +
> routes** and **resource detail‑view sections** (each in an error boundary); and a **Plugins** view
> with the trust notice. **Staged:** the heavier `pluginLib` externals (MUI/Redux/React Router), full
> `K8s`/`ApiProxy` parity + the WS multiplexer, and the install/Artifact‑Hub/signing flow.

- **4a** ✅ Native Extension Registry; **4b** ✅ loader + `pluginLib` facade + `register*` mapping
  (MUI/Redux/Router externals staged); then port 1–2 first‑party features onto the registry.
- **4c** `K8s`/`ApiProxy` data‑layer parity (critical path) + WS multiplexer.
- **4d** Install flow + Artifact Hub format + **trust/sandbox** gate (consent + error boundary shipped;
  signature verification staged) + a compatibility matrix of known‑working Headlamp plugins.

### Phase 5 — Hardening & distribution
- **Desktop** (Wails v3) parity; multi‑cluster polish; **i18n**; theming; plugin **catalog UI**;
  sandbox exploration; docs + generated‑artifact updates.

---

## 6. Risks & mitigations

| # | Risk | Impact | Mitigation |
|---|---|---|---|
| 1 | **React version skew** — KSail is on React **19**; Headlamp targets **18.x**. Plugins need a *single shared* React instance. | Plugins crash / hooks break. | Pin the **plugin‑host** React to Headlamp's major (isolated from KSail's own UI), or validate 19↔plugin interop early in P4. Resolve before 4b. |
| 2 | **Pre‑1.0 moving plugin API** — `register*`/`pluginLib`/`K8s` surface shifts across 0.x. | Compat rot. | Pin a **target Headlamp version**, maintain a compat matrix, treat the surface as a versioned contract; CI test against pinned plugins. |
| 3 | **`K8s`/`ApiProxy` reimplementation** is large/underspecified. | P4 slips. | Treat as the critical path; spike it in P0 (read‑only) and grow; consider *vendoring* Headlamp's `frontend/src/lib/k8s` (Apache‑2.0 allows) to bootstrap. |
| 4 | **WebSocket multiplexer protocol** — plugins expect Headlamp's WS, KSail uses SSE. | Plugins can't watch. | Add a Headlamp‑protocol WS multiplexer alongside SSE in 4c. |
| 5 | **Unsandboxed plugins** = full cluster‑cred access. | Supply‑chain risk. | Trust gate + checksum verify + optional iframe/worker sandbox (§4.4) — ship *safer* than Headlamp. |
| 6 | **MUI/Redux bundle weight** could bloat KSail's lean UI. | Perf/identity. | Load the compat runtime **lazily, only when a plugin is present**; KSail's own UI never imports MUI. |
| 7 | **Multi‑user RBAC** — operator acts with its own identity today. | AuthZ gap in‑cluster. | Token passthrough / impersonation in the data plane for the operator deployment. |
| 8 | **Maintainer philosophy** (Go‑native, avoid Node/Electron heaviness). | Scope/values drift. | Backend stays Go (`pkg/client` in‑process); JS heaviness is **contained to the plugin boundary**; desktop stays Wails. |

---

## 7. Open decisions (need a call before/within Phase 4)

1. **Pinned Headlamp target version** for plugin compatibility (and React‑version strategy, Risk 1).
2. **Bootstrap `K8s`/`ApiProxy` by vendoring Headlamp's Apache‑2.0 `frontend/src/lib/k8s`** vs full
   clean‑room reimplementation (licensing allows either; trade‑off is speed vs. independence).
3. **Sandbox posture** — ship unsandboxed‑with‑consent first (match Headlamp) or invest in the
   iframe/worker sandbox up front (differentiator, more work).
4. **Router choice** — React Router (familiar, matches Headlamp) vs. a minimal custom router (lean).
5. **In‑cluster multi‑user authZ** — token passthrough vs. impersonation vs. keep operator‑RBAC for v1.

---

## 8. Appendix — key sources & current‑state file map

**Headlamp facts** (verified June 2026): Apache‑2.0; CNCF Sandbox + `kubernetes-sigs/headlamp`
(SIG UI); v0.43.0, pre‑1.0. Plugins: TS/React → single `main.js` (Vite) → loaded via `Function`
constructor with `window.pluginLib` externals (not Module Federation, not `eval`/`import`);
distributed as `.tar.gz` indexed on Artifact Hub; **unsandboxed**, **frontend‑only**. Backend: Go
(Gorilla), proxy + WebSocket multiplexer, serves `/plugins`.

**KSail current‑state map**:
- Frontend SPA — `web/ui/src/` (`App.tsx`, `api.ts`, `components/`, `hooks/`, `lib/`, `generated/`)
- Backend API — `pkg/operator/api/server.go` (handlers/middleware/OIDC), `service.go`
  (`ClusterService`), `cr_service.go`, `pkg/cli/clusterapi/local_service.go`,
  `pkg/cli/uiserver/uiserver.go`
- Embedding — `pkg/webui/embed.go`
- Operator — `pkg/operator/`, `internal/controller/cluster_controller.go`
- AI surface — `pkg/toolgen/`, `pkg/svc/mcp/`, `pkg/svc/chat/`, `pkg/cli/cmd/chat/tools.go`
- GitOps detection — `pkg/svc/detector/gitops/`; cluster/context — `pkg/svc/detector/cluster/`
- Embedded tool clients — `pkg/client/` (kubectl, helm, flux, argocd, …)
- Desktop — `desktop/` (Wails v3 `alpha.98`)
