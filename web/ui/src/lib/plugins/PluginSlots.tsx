// React rendering slots for plugin-contributed UI. These are the points where the SPA renders
// extensions registered (by Headlamp plugins via pluginLib, or natively) into the registry: a route's
// component, and the extra sections appended to a resource's detail panel. Each plugin-rendered subtree
// is wrapped in an error boundary so a buggy or hostile plugin cannot white-screen the app.

import { Component, useMemo, type ComponentType, type ErrorInfo, type ReactNode } from "react";
import { registry, type PluginResource } from "./registry.ts";
import { usePluginRegistry } from "./usePlugins.ts";

// pluginState is the minimal Redux state a Headlamp plugin assumes the host provides. Notably
// `filter.namespaces` is a JS Set (Headlamp's useNamespaces selects it); it is a stable module-level
// instance so a plugin's useSelector does not see a new value each render (which would loop/warn). KSail
// does not yet drive real state in — plugins read these defaults rather than crashing.
const pluginState = {
  filter: { namespaces: new Set<string>(), search: "" },
  config: { settings: {} },
  ui: {},
};

// pluginStore is a minimal Redux store satisfying the contract a plugin's react-redux <Provider> needs
// (getState/subscribe/dispatch). dispatch is a no-op echo; bridging real KSail state is a follow-up.
const pluginStore = {
  getState: (): typeof pluginState => pluginState,
  subscribe: (): (() => void) => () => undefined,
  dispatch: (action: unknown): unknown => action,
};

// MuiStylesShape is the slice of @mui/material/styles (window.pluginLib.MuiStyles) PluginProviders uses
// to build and provide the plugin theme.
interface MuiStylesShape {
  createTheme?: (options: unknown) => unknown;
  ThemeProvider?: ComponentType<{ theme: unknown; children: ReactNode }>;
  StyledEngineProvider?: ComponentType<{ injectFirst?: boolean; children: ReactNode }>;
}

// isDarkMode reads KSail's current theme from the documentElement `dark` class (Tailwind dark mode), so
// the plugin MUI theme matches KSail's light/dark.
function isDarkMode(): boolean {
  return typeof document !== "undefined" && document.documentElement.classList.contains("dark");
}

// KSAIL_FONT mirrors Tailwind's default sans stack (KSail sets no custom font), so MUI-rendered plugin
// text matches KSail's typography instead of MUI's Roboto default.
const KSAIL_FONT =
  'ui-sans-serif, system-ui, sans-serif, "Apple Color Emoji", "Segoe UI Emoji", "Segoe UI Symbol", "Noto Color Emoji"';

// buildPluginTheme creates an MUI theme matching KSail's design tokens — the Tailwind slate/blue palette,
// an 8px radius, and KSail's font — so MUI-rendered plugin chrome (Paper/Card/Button/Typography) blends
// into KSail's UI instead of reading as default Material. MuiPaper's dark elevation overlay is removed so
// plugin cards stay flat like KSail's. It also carries Headlamp's `chartStyles` palette augmentation,
// which the Flux Overview reads via useTheme().palette.chartStyles (absent it, those reads throw).
function buildPluginTheme(muiStyles: MuiStylesShape, dark: boolean): unknown {
  return muiStyles.createTheme?.({
    shape: { borderRadius: 8 },
    typography: { fontFamily: KSAIL_FONT },
    components: {
      // Flatten Paper so MUI cards match KSail's flat surfaces (no dark-mode elevation tint).
      MuiPaper: { styleOverrides: { root: { backgroundImage: "none" } } },
    },
    palette: dark
      ? {
          mode: "dark",
          primary: { main: "#3b82f6" }, // blue-500
          background: { default: "#0f172a", paper: "#0f172a" }, // slate-900
          text: { primary: "#f8fafc", secondary: "#94a3b8" }, // slate-50 / slate-400
          divider: "#1e293b", // slate-800
          chartStyles: { defaultFillColor: "rgba(20, 20, 20, 0.1)", fillColor: "#475569", labelColor: "#f8fafc" },
        }
      : {
          mode: "light",
          primary: { main: "#2563eb" }, // blue-600
          background: { default: "#ffffff", paper: "#ffffff" },
          text: { primary: "#0f172a", secondary: "#475569" }, // slate-900 / slate-600
          divider: "#e2e8f0", // slate-200
          chartStyles: { defaultFillColor: "rgba(0, 0, 0, 0.08)", fillColor: "#cbd5e1", labelColor: "#0f172a" },
        },
  });
}

// PluginProviders wraps a plugin-rendered subtree in the non-router React context real Headlamp plugins
// assume is present: a Redux Provider (so useSelector/useDispatch work). It comes from the lazily-loaded
// react-redux external on window.pluginLib, present once a plugin has loaded; absent it renders children
// unwrapped. The Router context is supplied separately — by the persistent PluginRouterHost for plugin
// routes, and by PluginRuntimeProviders for the out-of-router slots below. (The MUI ThemeProvider with
// Headlamp's chartStyles palette is layered in here too.)
export function PluginProviders({ children }: { children: ReactNode }): ReactNode {
  const lib = typeof window === "undefined" ? undefined : window.pluginLib;
  const redux = lib?.ReactRedux as
    | { Provider?: ComponentType<{ store: unknown; children: ReactNode }> }
    | undefined;
  const muiStyles = lib?.MuiStyles as MuiStylesShape | undefined;
  const dark = isDarkMode();
  const theme = useMemo(
    () => (muiStyles?.createTheme ? buildPluginTheme(muiStyles, dark) : undefined),
    [muiStyles, dark],
  );

  let tree: ReactNode = children;

  // MUI ThemeProvider (carrying Headlamp's chartStyles palette) so plugin components reading the theme
  // work; StyledEngineProvider injectFirst keeps MUI's styles overridable.
  if (theme && muiStyles?.ThemeProvider) {
    const ThemeProvider = muiStyles.ThemeProvider;
    let themed: ReactNode = <ThemeProvider theme={theme}>{tree}</ThemeProvider>;

    if (muiStyles.StyledEngineProvider) {
      const StyledEngineProvider = muiStyles.StyledEngineProvider;
      themed = <StyledEngineProvider injectFirst>{themed}</StyledEngineProvider>;
    }

    tree = themed;
  }

  if (redux?.Provider) {
    const Provider = redux.Provider;
    tree = <Provider store={pluginStore}>{tree}</Provider>;
  }

  return tree;
}

// PluginRuntimeProviders wraps an out-of-router plugin slot (detail-view section, app-bar action) in a
// throwaway Router plus PluginProviders, so a slot using react-router hooks or redux does not crash.
// Plugin *routes* instead render under PluginRouterHost's single persistent router.
function PluginRuntimeProviders({ children }: { children: ReactNode }): ReactNode {
  const lib = typeof window === "undefined" ? undefined : window.pluginLib;
  const router = lib?.ReactRouter as { MemoryRouter?: ComponentType<{ children: ReactNode }> } | undefined;

  const tree: ReactNode = <PluginProviders>{children}</PluginProviders>;

  if (router?.MemoryRouter) {
    const Router = router.MemoryRouter;

    return <Router>{tree}</Router>;
  }

  return tree;
}

// PluginErrorBoundary isolates a plugin's rendered subtree. A throw during render surfaces a compact
// inline notice (attributed to the plugin) instead of crashing the surrounding KSail UI.
export class PluginErrorBoundary extends Component<{ name?: string; children: ReactNode }, { error: Error | null }> {
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

// Plugin *routes* render under the single persistent router in PluginRouterHost.tsx (which reuses the
// PluginErrorBoundary + PluginProviders exported above); the per-route host that used to live here was
// replaced so in-plugin navigation, param routes, and the route↔sidebar highlight work.
