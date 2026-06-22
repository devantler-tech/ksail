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
import { CommonComponents, type CommonComponentsShape } from "./commonComponents.tsx";
import { makeResourceClasses, type ResourceClasses } from "./k8s.ts";
import { registry, type PluginResource, type RouteProps } from "./registry.ts";
import { useAsyncList } from "./useAsyncList.ts";
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
  Notification: (message: string, ...rest: unknown[]) => void;
}

interface K8sShim {
  useResourceList: (kind: string, namespace?: string) => [PluginResource[], Error | null];
  ResourceClasses: ResourceClasses;
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

// installPluginLib assigns window.pluginLib once, wiring the Headlamp register*() surface onto the
// native registry and the K8s shim onto KSail's cluster-scoped resource API. getCluster is read live
// on each data fetch so the shim follows the active cluster.
export function installPluginLib(getCluster: () => ClusterRef | null): void {
  const registerSidebarEntry = (entry: HeadlampSidebarEntry): void => {
    registry.registerSidebarEntry({
      id: entry.name,
      label: entry.label,
      route: entry.url ?? entry.name,
      icon: entry.icon,
    });
  };

  const registerRoute = (route: HeadlampRoute): void => {
    registry.registerRoute({ path: route.path, component: route.component });
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
  "registerSidebarEntryFilter",
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

  // Router URL helpers — return a harmless hash anchor so a plugin building a link does not break.
  compat.Router = {
    createRouteURL: (): string => "#",
    getRoute: (): null => null,
    getRoutePath: (): string => "#",
  };

  // Namespaces a plugin may import (and subclass, for Plugin) even when it never exercises behaviour KSail
  // does not implement — exposed as empty objects / base classes so the import resolves.
  compat.Utils = {};
  compat.Plugin = class {};
  compat.Activity = {};
  compat.ConfigStore = class {};
  compat.PluginManager = {};

  // Misc top-level helpers from the lib's registry export.
  compat.clusterAction = noop;
  compat.runCommand = noop;
  compat.getHeadlampAPIHeaders = (): Record<string, string> => ({});
  compat.isLocaleSupported = (): boolean => true;
  compat.getSupportedLocales = (): Record<string, unknown> => ({});
}

// makeK8sShim builds the minimal-but-real Kubernetes data surface plugins consume. useResourceList is a
// React hook over KSail's allowlisted resource endpoint, returning [items, error] like Headlamp's
// useList for the common kinds (Pods, Deployments, …). It must be called from a component render.
// Live updates come from useAsyncList, which polls on an interval; an absent cluster yields an empty
// list.
function makeK8sShim(getCluster: () => ClusterRef | null): K8sShim {
  return {
    useResourceList(kind: string, namespace?: string): [PluginResource[], Error | null] {
      // Read the active cluster during render and key the fetch on it, so the list re-fetches when the
      // user switches clusters — not only when the kind/namespace arguments change.
      const cluster = getCluster();
      const clusterName = cluster?.name ?? null;
      const clusterNamespace = cluster?.namespace ?? null;

      return useAsyncList<PluginResource>(async () => {
        if (clusterName === null || clusterNamespace === null) {
          return [];
        }

        const list = await listResources(clusterNamespace, clusterName, kind, namespace);

        return list.items ?? [];
      }, [kind, namespace, clusterName, clusterNamespace]);
    },
    ResourceClasses: makeResourceClasses(getCluster),
  };
}
