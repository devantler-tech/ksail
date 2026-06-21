// React rendering slots for plugin-contributed UI. These are the points where the SPA renders
// extensions registered (by Headlamp plugins via pluginLib, or natively) into the registry: a route's
// component, and the extra sections appended to a resource's detail panel. Each plugin-rendered subtree
// is wrapped in an error boundary so a buggy or hostile plugin cannot white-screen the app.

import { Component, type ErrorInfo, type ReactNode } from "react";
import { registry, type PluginResource } from "./registry.ts";
import { usePluginRegistry } from "./usePlugins.ts";

// PluginErrorBoundary isolates a plugin's rendered subtree. A throw during render surfaces a compact
// inline notice (attributed to the plugin) instead of crashing the surrounding KSail UI.
class PluginErrorBoundary extends Component<{ name?: string; children: ReactNode }, { error: Error | null }> {
  constructor(props: { name?: string; children: ReactNode }) {
    super(props);
    this.state = { error: null };
  }

  static getDerivedStateFromError(error: Error): { error: Error } {
    return { error };
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    console.error("[plugin render error]", this.props.name, error, info);
  }

  override render(): ReactNode {
    if (this.state.error) {
      return (
        <div className="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300">
          Plugin{this.props.name ? ` "${this.props.name}"` : ""} failed to render: {this.state.error.message}
        </div>
      );
    }

    return this.props.children;
  }
}

// PluginDetailSections renders every registered details-view section for a resource, each isolated by an
// error boundary. It renders nothing when no sections are registered (the common case), so wiring it
// into the detail panel is zero-cost until a plugin contributes a section.
export function PluginDetailSections({ resource }: { resource: PluginResource }): ReactNode {
  // Subscribe so newly-registered sections appear without a manual refresh.
  usePluginRegistry();

  const sections = registry.getDetailsSections();
  if (sections.length === 0) {
    return null;
  }

  return (
    <>
      {sections.map((section) => (
        <PluginErrorBoundary key={section.id} name={section.pluginName}>
          <section>{section.render(resource)}</section>
        </PluginErrorBoundary>
      ))}
    </>
  );
}

// PluginRouteHost renders the component registered for a plugin route path, scoped to the active
// cluster, inside an error boundary. It shows a compact not-found notice when no route matches (e.g. a
// sidebar entry whose plugin failed to register its route).
export function PluginRouteHost({
  path,
  clusterName,
}: {
  path: string;
  clusterName: string | null;
}): ReactNode {
  usePluginRegistry();

  const route = registry.getRoute(path);
  if (!route) {
    return (
      <div className="mx-auto max-w-3xl rounded-lg border border-slate-200 bg-white p-6 text-sm text-slate-500 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-400">
        No plugin view is registered for <code className="font-mono">{path}</code>.
      </div>
    );
  }

  const RouteComponent = route.component;

  return (
    <PluginErrorBoundary name={route.pluginName}>
      <RouteComponent clusterName={clusterName} />
    </PluginErrorBoundary>
  );
}
