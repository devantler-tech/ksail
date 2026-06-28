import {
  Activity,
  KeyRound,
  Layers,
  LayoutDashboard,
  Puzzle,
  Server,
  Settings,
  Sparkles,
  type LucideIcon,
} from "lucide-react";

// The view registry is the single source of truth for the SPA's top-level sections: their title,
// nav/palette icon, scope, and availability gate. AppShell's sidebar nav, the command palette, the
// header title, and ksail:// deep-link parsing all derive from it, so adding a view means one entry
// here instead of editing ~6 parallel sites across three files. The View union itself is derived from
// the registry via `typeof` (see View below), so the registry and the union can never drift.

// ViewScope distinguishes the cluster-scoped workspace views (overview/resources/events — they operate
// on the active cluster and are hidden when none is selected) from the always-available global views.
export type ViewScope = "global" | "cluster";

// ViewGates is the runtime state the registry's availability predicates read: whether a cluster is
// drilled into, and the capability flags that gate individual views. Consumers build it from the
// current app state.
export interface ViewGates {
  // activeCluster is true when a cluster is drilled into (so cluster-scoped views can appear).
  activeCluster: boolean;
  // workloadEnabled gates Resources/Events on the backend's workload-read capability.
  workloadEnabled: boolean;
  // secretsEnabled gates the Secrets view on the backend's SOPS-cipher capability.
  secretsEnabled: boolean;
  // pluginsEnabled gates the Plugins view on the backend's plugins capability (it serves UI plugins).
  pluginsEnabled: boolean;
  // aiChatEnabled gates the Assistant view on the backend's AI chat capability (e.g. Copilot configured).
  aiChatEnabled: boolean;
}

// ViewDef is one registry entry. `enabled` encodes only the view's capability gate; the cluster-scope
// gate (an active cluster must exist for a cluster-scoped view) is applied uniformly by
// isViewAvailable, so individual entries do not repeat it.
export interface ViewDef {
  id: string;
  title: string;
  icon: LucideIcon;
  scope: ViewScope;
  enabled: (gates: ViewGates) => boolean;
}

// VIEWS is the ordered registry. Order is the canonical nav/palette order: the global Clusters home,
// then the cluster workspace (overview/resources/events), then the remaining global views. AppShell
// splits it by scope into the cluster and global nav zones, preserving this order within each.
export const VIEWS = [
  { id: "clusters", title: "Clusters", icon: Server, scope: "global", enabled: () => true },
  { id: "overview", title: "Overview", icon: LayoutDashboard, scope: "cluster", enabled: () => true },
  { id: "resources", title: "Resources", icon: Layers, scope: "cluster", enabled: (g) => g.workloadEnabled },
  { id: "events", title: "Events", icon: Activity, scope: "cluster", enabled: (g) => g.workloadEnabled },
  { id: "secrets", title: "Secrets", icon: KeyRound, scope: "global", enabled: (g) => g.secretsEnabled },
  { id: "assistant", title: "Assistant", icon: Sparkles, scope: "global", enabled: (g) => g.aiChatEnabled },
  { id: "plugins", title: "Plugins", icon: Puzzle, scope: "global", enabled: (g) => g.pluginsEnabled },
  // Settings is always available: its General/Appearance/About categories are pure client-side
  // preferences (useful on every backend, including the operator). The Credentials category gates
  // itself on the backend's settings endpoints from within the page (see settings/catalog.ts).
  { id: "settings", title: "Settings", icon: Settings, scope: "global", enabled: () => true },
] as const satisfies readonly ViewDef[];

// RegisteredView is one concrete registry entry (its id narrowed to the View literal), the element
// type of VIEWS and its scope partitions. Consumers iterate these to render nav/palette items.
export type RegisteredView = (typeof VIEWS)[number];

// View is the top-level SPA section, derived from the registry so it can never drift from VIEWS.
// Cluster-scoped views (overview/resources/events) operate on the active cluster; the rest are global.
// Routing is view-state (no router dependency).
export type View = RegisteredView["id"];

// VIEW_BY_ID indexes the registry for O(1) lookups (the header title, deep-link validation).
const VIEW_BY_ID = new Map<View, (typeof VIEWS)[number]>(VIEWS.map((view) => [view.id, view]));

// viewTitle returns a view's display title (used by the header and elsewhere a label is needed).
export function viewTitle(view: View): string {
  return VIEW_BY_ID.get(view)?.title ?? view;
}

// isView is a type guard for narrowing an arbitrary string (e.g. a deep-link segment) to a View.
export function isView(value: string): value is View {
  return VIEW_BY_ID.has(value as View);
}

// isViewAvailable reports whether a view should be shown for the current gates: a cluster-scoped view
// additionally requires an active cluster. Used by the command palette (which spans both scopes) so a
// cluster view never appears without a cluster selected.
export function isViewAvailable(view: ViewDef, gates: ViewGates): boolean {
  return (view.scope === "global" || gates.activeCluster) && view.enabled(gates);
}

// clusterViews / globalViews partition the registry by scope, in registry order, for the two AppShell
// nav zones. Each is still gate-filtered by the caller via view.enabled(gates).
export const clusterViews = VIEWS.filter((view) => view.scope === "cluster");
export const globalViews = VIEWS.filter((view) => view.scope === "global");

// CLUSTER_VIEW_IDS are the ids of the cluster-scoped views (the drill-in workspace) — the gate App.tsx
// uses to decide whether a view requires an active cluster.
export const CLUSTER_VIEW_IDS: View[] = clusterViews.map((view) => view.id);
