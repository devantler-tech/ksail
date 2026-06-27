// pluginLib builds the `window.pluginLib` global a Headlamp plugin binds to at runtime. Headlamp's
// plugin bundles are built with their shared libraries marked external and mapped to `pluginLib.*`
// (e.g. `import { registerSidebarEntry } from "@kinvolk/headlamp-plugin/lib"` resolves to
// `pluginLib.registerSidebarEntry`). KSail reproduces that contract — React plus the register*()
// surface plus a Kubernetes data shim — so an existing Headlamp plugin's register*() calls land in
// KSail's native extension registry (see registry.ts) without modifying the plugin.
//
// Scope (honest): React + register* + the K8s data layer (useResourceList, the kube-proxy-backed
// ApiProxy, and the ResourceClasses.<Kind>.useList() class hierarchy) + CommonComponents are
// implemented, so a Headlamp plugin that lists and renders cluster resources works unmodified. The
// heavier UI externals — Material UI, Redux, React Router, Monaco, Recharts — are still STAGED, pending
// the lazily-loaded MUI-in-pluginLib bundle; a plugin that imports those does not run yet.
// See docs/BEST-KUBERNETES-UI-PLAN.md §4.3.

import * as React from "react";
import * as ReactDOM from "react-dom";
import * as ReactJSX from "react/jsx-runtime";
import { listResources } from "../../api.ts";
import { CommonComponents, localeDate, timeAgo, type CommonComponentsShape } from "./commonComponents.tsx";
import { renderPluginIcon } from "./pluginIcon.ts";
import { KubeObject, makeResourceClasses, setActiveCluster, type ResourceClasses } from "./k8s.ts";
import { apiFactory, apiFactoryWithNamespace, makeCustomResourceClass } from "./makeCustomResourceClass.ts";
import { registry, type PluginResource, type RouteProps, type SidebarEntryFilter } from "./registry.ts";
import { builtinRouteTarget, encodeResourceDetailURL } from "./resourceDetail.ts";
import { useClusterScopedList } from "./useClusterScopedList.ts";
import { WebSocketManager } from "./wsMultiplexer.ts";

// ClusterRef identifies the active cluster the K8s shim scopes reads to (KSail's resource API is
// cluster-scoped). The loader supplies a live getter so the shim always reads the current cluster.
export interface ClusterRef {
  namespace: string;
  name: string;
}

// Headlamp register*() argument shapes (the subset the compat layer maps). These mirror the public
// @kinvolk/headlamp-plugin/lib signatures closely enough for the common extension points.
interface HeadlampSidebarEntry {
  name: string;
  label: string;
  url?: string;
  icon?: React.ReactNode;
  parent?: string;
}

interface HeadlampRoute {
  path: string;
  component: React.ComponentType<RouteProps>;
  name?: string;
  // sidebar is the id of the sidebar entry kept highlighted while this route is active (Headlamp parity).
  sidebar?: string;
}

// A details-view section is the modern Headlamp shape: a component rendered with the resource as a
// prop. KSail normalizes it to the registry's render(resource) function.
type DetailsSectionComponent = React.ComponentType<{ resource: PluginResource }>;

// PluginLib is the shape assigned to window.pluginLib. register* functions are exposed flat (the
// Headlamp externals map the whole lib module to this object) and again under Registry for parity.
export interface PluginLib {
  React: typeof React;
  ReactDOM: typeof ReactDOM;
  // ReactJSX is react/jsx-runtime. Real toolchain-built Headlamp bundles compile JSX to the automatic
  // runtime (ReactJSX.jsx(...)) and map react/jsx-runtime → pluginLib.ReactJSX, so the facade must expose
  // it or every JSX-using plugin bundle throws on load (our hand-written tests used React.createElement
  // and never hit this — a real bundle did).
  ReactJSX: typeof ReactJSX;
  registerSidebarEntry: (entry: HeadlampSidebarEntry) => void;
  registerSidebarEntryFilter: (filter: SidebarEntryFilter) => void;
  registerRoute: (route: HeadlampRoute) => void;
  registerDetailsViewSection: (section: DetailsSectionComponent) => void;
  registerAppBarAction: (action: React.ReactNode | React.ComponentType) => void;
  registerResourceTableColumnsProcessor: (id: string, process: (columns: unknown[]) => unknown[]) => void;
  Registry: {
    registerSidebarEntry: (entry: HeadlampSidebarEntry) => void;
    registerRoute: (route: HeadlampRoute) => void;
    registerDetailsViewSection: (section: DetailsSectionComponent) => void;
  };
  K8s: K8sShim;
  ApiProxy: ApiProxyShim;
  // WebSocketManager is the Headlamp multiplexer client (wsMultiplexer.ts), exposed so a plugin that
  // imports Headlamp's `WebSocketManager` to multiplex its own resource watches binds to KSail's
  // API-compatible implementation (same subscribe/createKey surface, same /wsMultiplexer wire protocol).
  WebSocketManager: typeof WebSocketManager;
  CommonComponents: CommonComponentsShape;
  // useTranslation is Headlamp's i18n hook; KSail provides a passthrough returning the key (or the string
  // default if one is passed), so untranslated plugins render their default English strings. A full
  // i18next integration is a follow-up.
  useTranslation: () => {
    t: (key: string, options?: unknown) => string;
    i18n: { language: string; changeLanguage: () => Promise<void> };
  };
  // Headlamp exposes the active-cluster surface plugins read (KSail owns cluster switching via its UI).
  Headlamp: HeadlampApi;
  // Lazily-installed heavy externals (see externals.ts) — present only after a plugin loads. Typed
  // loosely: they are external module namespaces consumed by plugin JS, not by KSail's own code.
  MuiMaterial?: unknown;
  MuiIconsMaterial?: unknown;
  MuiStyles?: unknown;
  ReactRedux?: unknown;
  ReactRouter?: unknown;
  Lodash?: unknown;
  Iconify?: unknown;
  // Router is the Headlamp routing namespace (createRouteURL/getRoute/getRoutePath), installed by
  // installCompatStubs and read by CommonComponents.Link; typed loosely as it is a compat surface.
  Router?: unknown;
  Notification: (message: string, ...rest: unknown[]) => void;
}

interface K8sShim {
  useResourceList: (kind: string, namespace?: string) => [PluginResource[], Error | null];
  ResourceClasses: ResourceClasses;
  // KubeObject + the CRD factories let a plugin that does not subclass directly mint custom-resource
  // classes (Headlamp's K8s.KubeObject / K8s.makeCustomResourceClass / K8s.apiFactory surface).
  KubeObject: typeof KubeObject;
  makeCustomResourceClass: typeof makeCustomResourceClass;
  apiFactory: typeof apiFactory;
  apiFactoryWithNamespace: typeof apiFactoryWithNamespace;
  // cluster mirrors Headlamp's lib/k8s/cluster module: real plugins import `KubeObject` from there
  // (`pluginLib.K8s.cluster.KubeObject`) to subclass it for their CRD types — read at plugin load, so it
  // must exist or the bundle crashes.
  cluster: { KubeObject: typeof KubeObject; KubeObjectClass: typeof KubeObject };
}

interface ApiProxyShim {
  request: (path: string) => Promise<unknown>;
}

// HeadlampApi is the subset of Headlamp's `Headlamp` namespace KSail implements — reading the active
// cluster name (KSail owns switching, so setCluster is a no-op and getClusters is empty for now).
interface HeadlampApi {
  getCluster: () => string | null;
  getClusters: () => Record<string, unknown>;
  setCluster: (name: string) => void;
}

declare global {
  interface Window {
    pluginLib?: PluginLib;
  }
}

// autoId mints stable-ish ids for register*() calls that do not carry one (Headlamp's app-bar action
// and column-processor registrations are positional), so the registry can de-dupe across reloads.
let autoId = 0;

function nextId(prefix: string): string {
  autoId += 1;

  return `${prefix}-${autoId}`;
}

// fillRouteParams substitutes :param segments in a route path from params — the createRouteURL fallback
// for when react-router's generatePath is not yet on window.pluginLib. A param with no value is left as
// the literal segment.
function fillRouteParams(path: string, params?: Record<string, string>): string {
  if (!params) {
    return path;
  }

  return path.replace(/:([A-Za-z0-9_]+)\??/g, (whole, key: string) =>
    params[key] === undefined ? whole : encodeURIComponent(params[key]),
  );
}

// installPluginLib assigns window.pluginLib once, wiring the Headlamp register*() surface onto the
// native registry and the K8s shim onto KSail's cluster-scoped resource API. getCluster is read live
// on each data fetch so the shim follows the active cluster.
export function installPluginLib(getCluster: () => ClusterRef | null): void {
  // The KubeObject statics (apiList/useList) read the active cluster through a module-level getter; set
  // it from the loader's live getter so a plugin's `Kustomization.apiList(...)` follows the active cluster.
  setActiveCluster(getCluster);

  const registerSidebarEntry = (entry: HeadlampSidebarEntry): void => {
    registry.registerSidebarEntry({
      id: entry.name,
      label: entry.label,
      // A group header (e.g. Flux's `parent: null` "Flux" entry) carries no url — leave route undefined
      // so it renders as a non-navigable collapsible group rather than a dead link.
      route: entry.url,
      // Headlamp's sidebar icon is often a plain icon-name string (e.g. "simple-icons:flux"); wrap it in
      // the @iconify/react Icon so the sidebar shows the plugin's logo, not the literal name as text.
      icon: renderPluginIcon(entry.icon),
      // Headlamp passes `parent: null` for a top-level entry; normalize to undefined.
      parent: entry.parent ?? undefined,
    });
  };

  const registerRoute = (route: HeadlampRoute): void => {
    registry.registerRoute({
      path: route.path,
      component: route.component,
      sidebar: route.sidebar,
      name: route.name,
    });
  };

  const registerSidebarEntryFilter = (filter: SidebarEntryFilter): void => {
    registry.registerSidebarEntryFilter(filter);
  };

  const registerDetailsViewSection = (section: DetailsSectionComponent): void => {
    registry.registerDetailsViewSection({
      id: nextId("section"),
      render: (resource) => React.createElement(section, { resource }),
    });
  };

  const lib: PluginLib = {
    React,
    ReactDOM,
    ReactJSX,
    registerSidebarEntry,
    registerSidebarEntryFilter,
    registerRoute,
    registerDetailsViewSection,
    registerAppBarAction: (action) => {
      // Headlamp's registerAppBarAction takes a bare ReactNode (e.g. registerAppBarAction(<span>Hi</span>))
      // or a component; normalize both to the registry's render() thunk so either form renders.
      const render =
        typeof action === "function"
          ? (): React.ReactNode => React.createElement(action as React.ComponentType)
          : (): React.ReactNode => action;
      registry.registerAppBarAction({ id: nextId("appbar"), render });
    },
    registerResourceTableColumnsProcessor: (id, process) => {
      registry.registerResourceTableColumnsProcessor({ id, process: (info) => process(info.columns) });
    },
    Registry: { registerSidebarEntry, registerRoute, registerDetailsViewSection },
    K8s: makeK8sShim(getCluster),
    ApiProxy: {
      // Proxy a read-only apiserver GET for the active cluster (via the backend KubeProxy) and return the
      // parsed JSON — the read subset of Headlamp's ApiProxy.request the data layer needs. Mutating verbs
      // and the full ResourceClasses/useApiGet surface remain staged (see this module's header).
      request: async (path: string): Promise<unknown> => {
        const cluster = getCluster();
        if (!cluster) {
          throw new Error("pluginLib.ApiProxy.request: no active cluster");
        }

        const base = `/api/v1/clusters/${encodeURIComponent(cluster.namespace)}/${encodeURIComponent(cluster.name)}`;
        const response = await fetch(`${base}/proxy/${path.replace(/^\//, "")}`);
        if (!response.ok) {
          throw new Error(`pluginLib.ApiProxy.request("${path}") failed: ${response.status}`);
        }

        return response.json();
      },
    },
    WebSocketManager,
    CommonComponents,
    useTranslation: () => ({
      t: (key, options) => (typeof options === "string" ? options : key),
      i18n: { language: "en", changeLanguage: () => Promise.resolve() },
    }),
    Headlamp: {
      getCluster: () => getCluster()?.name ?? null,
      getClusters: () => ({}),
      setCluster: () => {
        // KSail owns cluster switching through its own UI; a plugin cannot change the active cluster.
      },
    },
    Notification: (message, ...rest) => {
      // Surface plugin notifications via a DOM event the SPA can later route to toasts; log meanwhile.
      window.dispatchEvent(new CustomEvent("ksail:plugin-notification", { detail: { message, rest } }));
      console.info("[plugin notification]", message, ...rest);
    },
  };

  installCompatStubs(lib);
  window.pluginLib = lib;
}

// UNSUPPORTED_REGISTRATIONS are the Headlamp register*() functions KSail does not (yet) render an
// extension point for. The full set is large (see @kinvolk/headlamp-plugin/lib's registry export); KSail
// exposes each as a no-op so a plugin that calls one loads without crashing — the registration simply has
// no effect rather than throwing on an undefined function. The five KSail DOES render
// (sidebar/route/details-section/app-bar/table-columns) are wired in installPluginLib above.
const UNSUPPORTED_REGISTRATIONS = [
  "registerAppLogo",
  "registerAppTheme",
  "registerClusterChooser",
  "registerClusterStatus",
  "registerDetailsViewHeaderAction",
  "registerDetailsViewHeaderActionsProcessor",
  "registerDetailsViewSectionsProcessor",
  "registerGetTokenFunction",
  "registerHeadlampEventCallback",
  "registerKindIcon",
  "registerKubeObjectGlance",
  "registerMapSource",
  "registerOverviewChartsProcessor",
  "registerPluginSettings",
  "registerRouteFilter",
  "registerUIPanel",
  "registerAddClusterProvider",
  "registerClusterProviderDialog",
  "registerClusterProviderMenuItem",
  "registerCustomCreateProject",
  "registerProjectDetailsTab",
  "registerProjectOverviewSection",
  "registerProjectHeaderAction",
  "registerProjectDeleteButton",
];

// installCompatStubs fills in the rest of the Headlamp lib surface KSail does not natively render: every
// other register*() function as a no-op, plus minimal Router/Utils/Plugin/Activity/ConfigStore namespaces
// and the misc top-level helpers. The goal is that any real Headlamp plugin loads and runs the parts KSail
// supports, instead of throwing on the first unknown export it touches.
function installCompatStubs(lib: PluginLib): void {
  const compat = lib as unknown as Record<string, unknown>;
  const noop = (): undefined => undefined;

  for (const name of UNSUPPORTED_REGISTRATIONS) {
    compat[name] = noop;
  }

  // Router URL helpers backed by the registered plugin routes. createRouteURL resolves a Headlamp route
  // name to its URL (filling `:param` segments), so a plugin's `<Link to={Router.createRouteURL('source',
  // {namespace, name})}>` produces a real path the persistent plugin router (PluginRouterHost) matches.
  // generatePath is read off the lazily-loaded react-router (present once a plugin has loaded); a manual
  // fill is the fallback. An unknown route name returns "#" so a stray link is inert rather than throwing.
  compat.Router = {
    createRouteURL: (name: string, params?: Record<string, string>): string => {
      // A plugin-registered route wins (a plugin may define its own detail page, e.g. the Flux plugin's
      // Kustomization view): resolve it to its in-plugin path so the persistent plugin router matches it.
      const route = registry.getRouteByName(name);
      if (route) {
        const reactRouter = window.pluginLib?.ReactRouter as
          | { generatePath?: (path: string, params?: Record<string, string>) => string }
          | undefined;
        if (reactRouter?.generatePath) {
          try {
            return reactRouter.generatePath(route.path, params ?? {});
          } catch {
            // Fall through to the manual fill below.
          }
        }

        return fillRouteParams(route.path, params);
      }

      // Otherwise a Headlamp built-in resource route (namespace/pod/deployment/customresource/…). KSail
      // has no in-plugin page for those — it renders resource detail in its native overlay — so emit a
      // ksail-detail: URL that CommonComponents.Link opens via openResourceDetail. Unknown names stay "#".
      const target = builtinRouteTarget(name, params);

      return target ? encodeResourceDetailURL(target) : "#";
    },
    getRoute: (name: string): unknown => registry.getRouteByName(name) ?? null,
    getRoutePath: (name: string): string => registry.getRouteByName(name)?.path ?? "#",
  };

  // Utils is Headlamp's lib/util surface. KSail provides the read helpers plugins commonly call:
  // getCluster (the active cluster name), useFilterFunc (a no-op pass-through filter hook — KSail does not
  // drive Headlamp's search/namespace filter state), and the locale/relative date formatters. Unknown
  // filter helpers default to "keep".
  compat.Utils = {
    getCluster: (): string | null => lib.Headlamp.getCluster(),
    getClusterPrefixedPath: (path?: string): string => path ?? "/",
    useFilterFunc:
      () =>
      (): boolean =>
        true,
    localeDate: (date: unknown): string => localeDate(date),
    timeAgo: (date: unknown): string => timeAgo(date),
    filterResource: (): boolean => true,
    filterGeneric: (): boolean => true,
  };
  compat.Plugin = class {};
  compat.Activity = {};
  // ConfigStore is Headlamp's per-plugin settings store. Real plugins construct one at load
  // (`new ConfigStore('@headlamp-k8s/flux')`) and read it during render (`store.get()`, the
  // `store.useConfig()()` hook), so an empty class throws "get is not a function". KSail backs it with
  // localStorage so plugin settings persist; useConfig returns a (non-reactive) hook reading the config.
  compat.ConfigStore = class {
    private readonly storeKey: string;

    constructor(name: string) {
      this.storeKey = `ksail-plugin-config:${name}`;
    }

    get(): Record<string, unknown> {
      try {
        return JSON.parse(globalThis.localStorage?.getItem(this.storeKey) ?? "{}") as Record<string, unknown>;
      } catch {
        return {};
      }
    }

    set(config: Record<string, unknown>): void {
      try {
        globalThis.localStorage?.setItem(this.storeKey, JSON.stringify(config ?? {}));
      } catch {
        // Storage unavailable — settings simply do not persist.
      }
    }

    update(config: Record<string, unknown>): void {
      this.set({ ...this.get(), ...(config ?? {}) });
    }

    useConfig(): () => Record<string, unknown> {
      return (): Record<string, unknown> => this.get();
    }
  };
  compat.PluginManager = {};

  // Crd is Headlamp's lib/k8s/crd module: real plugins call pluginLib.Crd.makeCustomResourceClass at load
  // to mint their CRD classes (e.g. the Flux plugin's Kustomization/HelmRelease types), so it must exist
  // and be wired to the real factories or the bundle crashes on load.
  compat.Crd = { makeCustomResourceClass, apiFactory, apiFactoryWithNamespace };

  // Externals KSail does not bundle but a real plugin references at load (passed to its UMD factory, so
  // they must be defined objects, not undefined): a notistack snackbar shim (action notifications no-op
  // instead of crashing) and empty Monaco editor stubs (a plugin's YAML-editor view renders nothing
  // rather than crashing the whole plugin).
  compat.Notistack = {
    useSnackbar: () => ({ enqueueSnackbar: noop, closeSnackbar: noop }),
    SnackbarProvider: ({ children }: { children?: unknown }) => children,
  };
  compat.MonacoEditor = {};
  compat.ReactMonacoEditor = { default: (): null => null, Editor: (): null => null };

  // Misc top-level helpers from the lib's registry export.
  compat.clusterAction = noop;
  compat.runCommand = noop;
  compat.getHeadlampAPIHeaders = (): Record<string, string> => ({});
  compat.isLocaleSupported = (): boolean => true;
  compat.getSupportedLocales = (): Record<string, unknown> => ({});
}

// makeK8sShim builds the minimal-but-real Kubernetes data surface plugins consume. useResourceList is a
// React hook over KSail's allowlisted resource endpoint, returning [items, error] like Headlamp's
// useList for the common kinds (Pods, Deployments, …). It must be called from a component render. The
// cluster-scoped fetch-on-change effect is shared with k8s.ts's useList via useClusterScopedList; the
// kind/namespace arguments are passed as extra deps so the list also re-fetches when they change.
function makeK8sShim(getCluster: () => ClusterRef | null): K8sShim {
  return {
    useResourceList(kind: string, namespace?: string): [PluginResource[], Error | null] {
      return useClusterScopedList(
        getCluster,
        async (cluster) => {
          const list = await listResources(cluster.namespace, cluster.name, kind, namespace);

          return list.items ?? [];
        },
        [kind, namespace],
      );
    },
    ResourceClasses: makeResourceClasses(),
    KubeObject,
    makeCustomResourceClass,
    apiFactory,
    apiFactoryWithNamespace,
    cluster: { KubeObject, KubeObjectClass: KubeObject },
  };
}
