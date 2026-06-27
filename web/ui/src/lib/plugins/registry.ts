// The extension registry is KSail's native home for UI extensions. Both KSail-native extensions and
// (via the Headlamp-compat pluginLib) third-party Headlamp plugins register into this one registry, so
// the rest of the SPA renders extensions without knowing their origin. This is the "native registry +
// Headlamp-compat facade" design: pluginLib.ts adapts Headlamp's register*() call shapes onto the
// methods here.
//
// Rendering status today: sidebar entries, routes, and details-view sections are rendered by the SPA;
// app-bar actions and table-column processors are accepted (so a plugin calling them does not crash)
// but not yet rendered — see PluginsView's staged notice. Holding them here means wiring them later
// needs no plugin change.

import type { ComponentType, ReactNode } from "react";

// SidebarEntry is a plugin-contributed nav item shown in the AppShell's Plugins zone. A leaf entry
// navigates to its route (a registerRoute with the same `route` path renders the content); a group
// header (Headlamp's `parent: null` entry with no `url`, e.g. "Flux") has no route and only nests its
// children.
export interface SidebarEntry {
  // id is the entry's stable identity (Headlamp's sidebar `name`).
  id: string;
  label: string;
  // route is the path the entry navigates to (Headlamp's `url`); a matching PluginRoute renders it.
  // Optional: a group header has no route (it only expands/collapses its children).
  route?: string;
  // icon is an optional React node (an inline <svg> or an icon element); a default is shown if absent.
  icon?: ReactNode;
  // parent is the id (Headlamp `name`) of the entry this one nests under (Headlamp's `parent`); a
  // top-level entry/group has none. The nested Flux sidebar uses a parent group + children.
  parent?: string;
  // pluginName attributes the entry to the plugin that registered it (set by the loader).
  pluginName?: string;
}

// SidebarNode is a sidebar entry with its resolved children — the shape getSidebarTree() yields for the
// AppShell to render nested groups.
export interface SidebarNode extends SidebarEntry {
  children: SidebarEntry[];
}

// SidebarEntryFilter is a plugin-registered predicate (Headlamp's registerSidebarEntryFilter) that hides
// entries when it returns a falsy value (e.g. the Flux plugin hides its children when Flux is not
// installed). It receives the entry in a Headlamp-compatible shape (`name` aliases `id`, `url` aliases
// `route`).
export type SidebarEntryFilter = (entry: SidebarEntry & { name: string; url?: string }) => unknown;

// RouteProps are passed to a plugin route's component so it can scope its data to the active cluster.
export interface RouteProps {
  // clusterName is the active cluster's name, or null on a global route (no cluster drilled into).
  clusterName: string | null;
}

// PluginRoute renders a component for a route path. `sidebar` is the id of the sidebar entry to keep
// highlighted while the route is active (Headlamp's route `sidebar`), so a detail route under a section
// still highlights that section; `name` is the route's stable name used by createRouteURL/getRoute.
export interface PluginRoute {
  path: string;
  component: ComponentType<RouteProps>;
  sidebar?: string;
  name?: string;
  pluginName?: string;
}

// PluginResource is a loose view of a Kubernetes object handed to a details-view section. Plugins read
// whatever fields they need; the rest is passthrough.
export interface PluginResource {
  apiVersion?: string;
  kind?: string;
  metadata?: { name?: string; namespace?: string; [key: string]: unknown };
  [key: string]: unknown;
}

// DetailsViewSection renders extra content appended to a resource's detail panel (Headlamp's
// registerDetailsViewSection). `render` returns a node for the resource, or null to contribute nothing
// (e.g. a section that only applies to Pods).
export interface DetailsViewSection {
  id: string;
  render: (resource: PluginResource) => ReactNode;
  pluginName?: string;
}

// AppBarAction and ResourceTableColumnProcessor are registered for Headlamp API completeness (so a
// plugin calling them does not crash) but are not yet rendered by the SPA. They are retained so wiring
// them later needs no plugin change.
export interface AppBarAction {
  id: string;
  render: () => ReactNode;
  pluginName?: string;
}

export interface ResourceTableColumnProcessor {
  id: string;
  // process may transform the column set (Headlamp parity). Columns are opaque until rendering lands.
  process: (info: { columns: unknown[] }) => unknown[];
  pluginName?: string;
}

type Listener = () => void;

// ExtensionRegistry holds all registered extensions and notifies subscribers (via a monotonic version)
// when the set changes, so React surfaces re-render through useSyncExternalStore without snapshot churn.
class ExtensionRegistry {
  private sidebarEntries: SidebarEntry[] = [];
  private sidebarFilters: SidebarEntryFilter[] = [];
  private routes = new Map<string, PluginRoute>();
  private detailsSections: DetailsViewSection[] = [];
  private appBarActions: AppBarAction[] = [];
  private columnProcessors: ResourceTableColumnProcessor[] = [];
  private listeners = new Set<Listener>();
  private version = 0;
  // pluginContext is the plugin currently loading, so register*() calls attribute entries to it.
  private pluginContext: string | undefined;

  // setPluginContext marks which plugin's bundle is executing, so registrations made during its load
  // are attributed to it. The loader sets it before injecting a plugin's script and clears it after.
  setPluginContext(name: string | undefined): void {
    this.pluginContext = name;
  }

  registerSidebarEntry(entry: SidebarEntry): void {
    const next = { ...entry, pluginName: entry.pluginName ?? this.pluginContext };
    this.sidebarEntries = [...this.sidebarEntries.filter((existing) => existing.id !== entry.id), next];
    this.bump();
  }

  registerSidebarEntryFilter(filter: SidebarEntryFilter): void {
    this.sidebarFilters = [...this.sidebarFilters, filter];
    this.bump();
  }

  registerRoute(route: PluginRoute): void {
    this.routes.set(route.path, { ...route, pluginName: route.pluginName ?? this.pluginContext });
    this.bump();
  }

  registerDetailsViewSection(section: DetailsViewSection): void {
    const next = { ...section, pluginName: section.pluginName ?? this.pluginContext };
    this.detailsSections = [...this.detailsSections.filter((existing) => existing.id !== section.id), next];
    this.bump();
  }

  registerAppBarAction(action: AppBarAction): void {
    const next = { ...action, pluginName: action.pluginName ?? this.pluginContext };
    this.appBarActions = [...this.appBarActions.filter((existing) => existing.id !== action.id), next];
    this.bump();
  }

  registerResourceTableColumnsProcessor(processor: ResourceTableColumnProcessor): void {
    const next = { ...processor, pluginName: processor.pluginName ?? this.pluginContext };
    this.columnProcessors = [
      ...this.columnProcessors.filter((existing) => existing.id !== processor.id),
      next,
    ];
    this.bump();
  }

  getSidebarEntries(): readonly SidebarEntry[] {
    return this.sidebarEntries;
  }

  // getSidebarTree returns the visible sidebar entries as a parent→children forest: registered filters
  // are applied first (a hidden entry never produces an empty group), then each entry with a known
  // `parent` nests under it; entries with no parent (or an unknown one) become roots. Registration order
  // is preserved at every level, so the rendered nav matches the plugin's declaration order.
  getSidebarTree(): SidebarNode[] {
    const visible = this.sidebarEntries.filter((entry) => this.passesFilters(entry));
    const nodes: SidebarNode[] = visible.map((entry) => ({ ...entry, children: [] }));
    const byId = new Map(nodes.map((node) => [node.id, node]));
    const roots: SidebarNode[] = [];

    for (const node of nodes) {
      const parent = node.parent ? byId.get(node.parent) : undefined;
      if (parent) {
        parent.children.push(node);
      } else {
        roots.push(node);
      }
    }

    return roots;
  }

  getRoute(path: string): PluginRoute | undefined {
    return this.routes.get(path);
  }

  // getRoutes returns every registered route, for the plugin router host to build its <Routes>.
  getRoutes(): readonly PluginRoute[] {
    return [...this.routes.values()];
  }

  // getRouteByName finds a route by its Headlamp route `name` (used by Router.createRouteURL/getRoute).
  getRouteByName(name: string): PluginRoute | undefined {
    for (const route of this.routes.values()) {
      if (route.name === name) {
        return route;
      }
    }

    return undefined;
  }

  // passesFilters runs every registered sidebar filter against an entry (in Headlamp shape, `name`
  // aliasing `id`); the entry is hidden if any filter returns falsy. A throwing filter is ignored (it
  // never hides an entry), so a buggy plugin filter cannot blank the sidebar.
  private passesFilters(entry: SidebarEntry): boolean {
    for (const filter of this.sidebarFilters) {
      try {
        if (!filter({ ...entry, name: entry.id, url: entry.route })) {
          return false;
        }
      } catch {
        // A filter that throws does not hide the entry.
      }
    }

    return true;
  }

  getDetailsSections(): readonly DetailsViewSection[] {
    return this.detailsSections;
  }

  getAppBarActions(): readonly AppBarAction[] {
    return this.appBarActions;
  }

  // getVersion is the useSyncExternalStore snapshot: a primitive that changes on every mutation, so
  // subscribers re-render while reading the (mutable) getters above directly in render.
  getVersion(): number {
    return this.version;
  }

  subscribe(listener: Listener): () => void {
    this.listeners.add(listener);

    return () => {
      this.listeners.delete(listener);
    };
  }

  // reset clears every registration. The loader calls it before a fresh load so reloading reflects the
  // current installed set rather than accumulating duplicates across reloads.
  reset(): void {
    this.sidebarEntries = [];
    this.sidebarFilters = [];
    this.routes = new Map();
    this.detailsSections = [];
    this.appBarActions = [];
    this.columnProcessors = [];
    this.bump();
  }

  private bump(): void {
    this.version += 1;
    this.listeners.forEach((listener) => {
      listener();
    });
  }
}

// registry is the single app-wide extension registry instance.
export const registry = new ExtensionRegistry();
