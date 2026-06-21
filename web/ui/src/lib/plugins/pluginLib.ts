// pluginLib builds the `window.pluginLib` global a Headlamp plugin binds to at runtime. Headlamp's
// plugin bundles are built with their shared libraries marked external and mapped to `pluginLib.*`
// (e.g. `import { registerSidebarEntry } from "@kinvolk/headlamp-plugin/lib"` resolves to
// `pluginLib.registerSidebarEntry`). KSail reproduces that contract — React plus the register*()
// surface plus a Kubernetes data shim — so an existing Headlamp plugin's register*() calls land in
// KSail's native extension registry (see registry.ts) without modifying the plugin.
//
// Scope (honest): the React + register* + a real, minimal K8s.useResourceList surface are implemented.
// The heavier Headlamp externals — Material UI, Redux, React Router, Monaco, Recharts — and the full
// K8s/ApiProxy data layer (raw apiserver proxy, ResourceClasses, useApiGet) are STAGED, pending the
// MUI-in-pluginLib bundle and KSail's generic cluster proxy. A plugin that uses only React + register*
// (+ K8s.useResourceList for live data) works today; one that imports MUI/Redux/Router does not yet.
// See docs/BEST-KUBERNETES-UI-PLAN.md §4.3.

import * as React from "react";
import * as ReactDOM from "react-dom";
import { listResources } from "../../api.ts";
import { registry, type PluginResource, type RouteProps } from "./registry.ts";

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
  registerSidebarEntry: (entry: HeadlampSidebarEntry) => void;
  registerRoute: (route: HeadlampRoute) => void;
  registerDetailsViewSection: (section: DetailsSectionComponent) => void;
  registerAppBarAction: (render: () => React.ReactNode) => void;
  registerResourceTableColumnsProcessor: (id: string, process: (columns: unknown[]) => unknown[]) => void;
  Registry: {
    registerSidebarEntry: (entry: HeadlampSidebarEntry) => void;
    registerRoute: (route: HeadlampRoute) => void;
    registerDetailsViewSection: (section: DetailsSectionComponent) => void;
  };
  K8s: K8sShim;
  ApiProxy: ApiProxyShim;
  Notification: (message: string, ...rest: unknown[]) => void;
}

interface K8sShim {
  useResourceList: (kind: string, namespace?: string) => [PluginResource[], Error | null];
}

interface ApiProxyShim {
  request: (path: string) => Promise<never>;
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
    registerSidebarEntry,
    registerRoute,
    registerDetailsViewSection,
    registerAppBarAction: (render) => {
      registry.registerAppBarAction({ id: nextId("appbar"), render });
    },
    registerResourceTableColumnsProcessor: (id, process) => {
      registry.registerResourceTableColumnsProcessor({ id, process: (info) => process(info.columns) });
    },
    Registry: { registerSidebarEntry, registerRoute, registerDetailsViewSection },
    K8s: makeK8sShim(getCluster),
    ApiProxy: {
      // Raw apiserver proxying is staged; reject explicitly so a plugin sees a clear, debuggable error
      // rather than a silent wrong result. Use K8s.useResourceList for live data today.
      request: (path) =>
        Promise.reject(
          new Error(`pluginLib.ApiProxy.request("${path}") is not yet supported in KSail; use K8s.useResourceList`),
        ),
    },
    Notification: (message, ...rest) => {
      // Surface plugin notifications via a DOM event the SPA can later route to toasts; log meanwhile.
      window.dispatchEvent(new CustomEvent("ksail:plugin-notification", { detail: { message, rest } }));
      console.info("[plugin notification]", message, ...rest);
    },
  };

  window.pluginLib = lib;
}

// makeK8sShim builds the minimal-but-real Kubernetes data surface plugins consume. useResourceList is a
// React hook over KSail's allowlisted resource endpoint, returning [items, error] like Headlamp's
// useList for the common kinds (Pods, Deployments, …). It must be called from a component render.
function makeK8sShim(getCluster: () => ClusterRef | null): K8sShim {
  return {
    useResourceList(kind: string, namespace?: string): [PluginResource[], Error | null] {
      const [items, setItems] = React.useState<PluginResource[]>([]);
      const [error, setError] = React.useState<Error | null>(null);

      React.useEffect(() => {
        const cluster = getCluster();
        if (!cluster) {
          setItems([]);

          return undefined;
        }

        let active = true;

        listResources(cluster.namespace, cluster.name, kind, namespace)
          .then((list) => {
            if (active) {
              setItems(list.items ?? []);
            }
          })
          .catch((err: unknown) => {
            if (active) {
              setError(err instanceof Error ? err : new Error(String(err)));
            }
          });

        return () => {
          active = false;
        };
      }, [kind, namespace]);

      return [items, error];
    },
  };
}
