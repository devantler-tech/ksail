// React rendering slots for plugin-contributed UI. These are the points where the SPA renders
// extensions registered (by Headlamp plugins via pluginLib, or natively) into the registry: a route's
// component, and the extra sections appended to a resource's detail panel. Each plugin-rendered subtree
// is wrapped in an error boundary so a buggy or hostile plugin cannot white-screen the app.

import { Component, type ComponentType, type ErrorInfo, type ReactNode } from "react";
import { registry, type PluginResource } from "./registry.ts";
import { usePluginRegistry } from "./usePlugins.ts";

// pluginStore is a minimal no-op Redux store. KSail has no Headlamp-shaped Redux state, but a plugin that
// calls useSelector/useDispatch must run inside a react-redux <Provider> or it throws; this satisfies the
// store contract (getState/subscribe/dispatch) so such plugins render against an empty state instead of
// crashing. Bridging real KSail state into this store is a follow-up.
const pluginStore = {
  getState: (): Record<string, unknown> => ({}),
  subscribe: (): (() => void) => () => undefined,
  dispatch: (action: unknown): unknown => action,
};

// PluginRuntimeProviders wraps a plugin-rendered subtree in the React context real Headlamp plugins
// assume is present: a Router (so react-router hooks such as useNavigate/useParams work) and a Redux
// Provider (so useSelector/useDispatch work). Both come from the lazily-loaded externals on
// window.pluginLib, present once a plugin has loaded; when one is absent the children render unwrapped.
function PluginRuntimeProviders({ children }: { children: ReactNode }): ReactNode {
  const lib = window.pluginLib;
  const router = lib?.ReactRouter as { MemoryRouter?: ComponentType<{ children: ReactNode }> } | undefined;
  const redux = lib?.ReactRedux as
    | { Provider?: ComponentType<{ store: unknown; children: ReactNode }> }
    | undefined;

  let tree: ReactNode = children;

  if (redux?.Provider) {
    const Provider = redux.Provider;
    tree = <Provider store={pluginStore}>{tree}</Provider>;
  }

  if (router?.MemoryRouter) {
    const Router = router.MemoryRouter;
    tree = <Router>{tree}</Router>;
  }

  return tree;
}

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

// renderExtensionList maps a registered-extension list to error-boundary-wrapped nodes — the shared
// shape behind the detail-section and app-bar slots. It renders nothing when the list is empty (so each
// slot is zero-cost until a plugin contributes), and keeps the two slots from duplicating the
// guard-and-map boilerplate.
function renderExtensionList<T extends { id: string; pluginName?: string }>(
  items: readonly T[],
  renderItem: (item: T) => ReactNode,
): ReactNode {
  if (items.length === 0) {
    return null;
  }

  return (
    <>
      {items.map((item) => (
        <PluginErrorBoundary key={item.id} name={item.pluginName}>
          <PluginRuntimeProviders>{renderItem(item)}</PluginRuntimeProviders>
        </PluginErrorBoundary>
      ))}
    </>
  );
}

// PluginDetailSections renders every registered details-view section for a resource, each isolated by an
// error boundary. It renders nothing when no sections are registered (the common case), so wiring it
// into the detail panel is zero-cost until a plugin contributes a section.
export function PluginDetailSections({ resource }: { resource: PluginResource }): ReactNode {
  // Subscribe so newly-registered sections appear without a manual refresh.
  usePluginRegistry();

  return renderExtensionList(registry.getDetailsSections(), (section) => (
    <section>{section.render(resource)}</section>
  ));
}

// PluginAppBarActions renders every registered app-bar action (Headlamp's registerAppBarAction), each
// isolated by an error boundary, into the top header. It renders nothing when none are registered (the
// common case), so placing it in the header is zero-cost until a plugin contributes an action.
export function PluginAppBarActions(): ReactNode {
  // Subscribe so a newly-registered action appears without a manual refresh.
  usePluginRegistry();

  return renderExtensionList(registry.getAppBarActions(), (action) => action.render());
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
      <PluginRuntimeProviders>
        <RouteComponent clusterName={clusterName} />
      </PluginRuntimeProviders>
    </PluginErrorBoundary>
  );
}
