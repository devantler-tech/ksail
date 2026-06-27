// PluginRouterHost is the single, persistent router for the plugin surface. It replaces the old
// per-render throwaway MemoryRouter + exact-path Map lookup with ONE MemoryRouter that hosts every
// registered plugin route in a single <Routes>. That is what makes a real multi-page plugin (e.g. the
// Flux plugin) work: param routes (`/flux/kustomizations/:namespace/:name`) match, in-plugin
// <Link>/useNavigate/useParams navigate within the same router, and the router persists across
// navigations instead of being recreated each render.
//
// KSail is a react-router v7 app, so this host imports react-router-dom DIRECTLY (v7). Plugin code, by
// contrast, binds to the v5-shaped shim on window.pluginLib.ReactRouter (see reactRouterCompat.ts) — the
// two coexist because both are backed by the same v7 runtime.

import type { ReactNode } from "react";
import { useEffect } from "react";
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { registry } from "./registry.ts";
import { usePluginRegistry } from "./usePlugins.ts";
import { PluginErrorBoundary, PluginProviders } from "./PluginSlots.tsx";
import { publishPluginLocation, setPluginNavigate } from "./pluginNavigation.ts";

// PluginRouterBridge runs inside the router and publishes its navigate() + current location to the
// pluginNavigation module, so KSail's out-of-router sidebar/header can drive and follow it.
function PluginRouterBridge(): null {
  const navigate = useNavigate();
  const location = useLocation();

  useEffect(() => {
    setPluginNavigate((to) => navigate(to));

    return () => setPluginNavigate(null);
  }, [navigate]);

  useEffect(() => {
    publishPluginLocation(location.pathname);
  }, [location.pathname]);

  return null;
}

// NoRouteNotice is the catch-all shown when the active path matches no registered plugin route (e.g. a
// sidebar entry whose plugin failed to register its route).
function NoRouteNotice(): ReactNode {
  return (
    <div className="mx-auto max-w-3xl rounded-lg border border-slate-200 bg-white p-6 text-sm text-slate-500 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-400">
      No plugin view is registered for this route.
    </div>
  );
}

// PluginRoutes builds a <Route> per registered plugin route, each wrapped in an error boundary + the
// (router-free) plugin providers. usePluginRegistry re-renders this when routes are (un)registered.
function PluginRoutes({ clusterName }: { clusterName: string | null }): ReactNode {
  usePluginRegistry();
  const routes = registry.getRoutes();

  return (
    <Routes>
      {routes.map((route) => {
        const RouteComponent = route.component;

        return (
          <Route
            key={route.path}
            path={route.path}
            element={
              <PluginErrorBoundary name={route.pluginName}>
                <PluginProviders>
                  <RouteComponent clusterName={clusterName} />
                </PluginProviders>
              </PluginErrorBoundary>
            }
          />
        );
      })}
      <Route path="*" element={<NoRouteNotice />} />
    </Routes>
  );
}

// PluginRouterHost mounts the persistent MemoryRouter for the plugin surface. initialPath seeds the
// first view (the sidebar route the user clicked to enter the surface); subsequent navigation flows
// through the router (sidebar clicks via pluginNavigate, in-plugin links via the router itself).
export function PluginRouterHost({
  initialPath,
  clusterName,
}: {
  initialPath: string;
  clusterName: string | null;
}): ReactNode {
  return (
    <MemoryRouter initialEntries={[initialPath || "/"]}>
      <PluginRouterBridge />
      <PluginRoutes clusterName={clusterName} />
    </MemoryRouter>
  );
}
