// pluginNavigation bridges KSail's chrome (its sidebar + header, which live OUTSIDE the plugin router)
// to the single persistent plugin router in PluginRouterHost. A child rendered inside that router
// publishes its navigate function and current location here, so the out-of-router sidebar can drive the
// router on click and reflect the active route (highlight + header title). This is the same
// cross-the-React-boundary module-singleton idiom KSail already uses for watch availability
// (watchStream.setKubeWatchAvailable) and plugin context (registry.setPluginContext).

import { registry } from "./registry.ts";

type NavigateFn = (to: string) => void;

// navigateFn is the active plugin router's navigate(), or null when the plugin surface is not mounted.
let navigateFn: NavigateFn | null = null;

// setPluginNavigate is called by the in-router bridge on mount/unmount to publish (or clear) navigate().
export function setPluginNavigate(fn: NavigateFn | null): void {
  navigateFn = fn;
}

// pluginNavigate asks the plugin router to navigate (e.g. from a sidebar click). A no-op when no plugin
// router is mounted yet — the caller seeds the initial route via PluginRouterHost's initialPath instead.
export function pluginNavigate(to: string): void {
  navigateFn?.(to);
}

// --- current plugin location store (useSyncExternalStore-compatible) ---

let currentPath = "/";
const listeners = new Set<() => void>();

// publishPluginLocation records the plugin router's current pathname (from the in-router bridge) and
// notifies subscribers, so the sidebar highlight + header title follow in-plugin navigation (e.g. a
// plugin <Link> to a detail page).
export function publishPluginLocation(path: string): void {
  if (path === currentPath) {
    return;
  }

  currentPath = path;
  listeners.forEach((listener) => {
    listener();
  });
}

export function subscribePluginLocation(listener: () => void): () => void {
  listeners.add(listener);

  return () => {
    listeners.delete(listener);
  };
}

export function getPluginLocation(): string {
  return currentPath;
}

// pathMatches reports whether a plugin location matches a route pattern, treating `:param` segments as
// single-segment wildcards (e.g. `/flux/kustomizations/:namespace/:name` matches
// `/flux/kustomizations/default/podinfo`). A small inline matcher is used rather than react-router's
// matchPath so this module — eagerly imported by KSail's chrome — does not pull react-router into the
// main bundle (it stays a plugin-only lazy external). Highlight is best-effort, so exactness is enough.
function pathMatches(pattern: string, path: string): boolean {
  const normalize = (value: string): string => value.replace(/\/+$/, "");
  if (normalize(pattern) === normalize(path)) {
    return true;
  }

  const source = normalize(pattern)
    .split("/")
    .map((segment) => (segment.startsWith(":") ? "[^/]+" : segment.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")))
    .join("/");

  return new RegExp(`^${source}/?$`).test(normalize(path));
}

// activeSidebarId resolves which sidebar entry should be highlighted for a plugin location: match the
// path against each registered route; the matched route's `sidebar` (Headlamp's route→sidebar link) is
// the active id, so a detail route under a section still highlights that section. Falls back to a
// sidebar entry whose own route equals the path.
export function activeSidebarId(path: string): string | null {
  for (const route of registry.getRoutes()) {
    if (pathMatches(route.path, path)) {
      if (route.sidebar) {
        return route.sidebar;
      }

      const entry = registry.getSidebarEntries().find((candidate) => candidate.route === route.path);

      return entry?.id ?? null;
    }
  }

  const entry = registry.getSidebarEntries().find((candidate) => candidate.route === path);

  return entry?.id ?? null;
}
