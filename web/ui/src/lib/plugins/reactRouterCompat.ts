// reactRouterCompat adapts react-router v7 (the version KSail bundles) to the v5 surface Headlamp
// plugins were built against. Headlamp's plugin toolchain bundles react-router v5 and re-exports
// `useHistory`/`Switch`/`Redirect`/`useRouteMatch`/`withRouter`; v7 removed all of those. The plugin
// bundle binds those names to `window.pluginLib.ReactRouter`, so externals.ts installs the SHIM (not raw
// v7) there — a plugin's `useHistory()` then works, backed by v7's useNavigate/useLocation. KSail's own
// router code (PluginRouterHost) imports react-router-dom directly and is unaffected.

import * as React from "react";

// ReactRouterCompatInput is the slice of the loaded react-router v7 module the shim needs. The rest of
// the module (Link, Route, Routes, useParams, Navigate, Outlet, generatePath, matchPath, MemoryRouter, …)
// is preserved by spreading, so plugins importing any v7 export via pluginLib.ReactRouter still get it.
export interface ReactRouterCompatInput {
  useNavigate: () => (to: unknown, options?: { replace?: boolean }) => void;
  useLocation: () => { pathname: string; search: string; hash: string; state: unknown };
  useParams: () => Record<string, string | undefined>;
  Navigate: React.ComponentType<{ to: unknown; replace?: boolean }>;
  Routes: React.ComponentType<{ children?: React.ReactNode }>;
  [key: string]: unknown;
}

// makeReactRouterV5Compat returns a react-router object exposing both the v7 surface (via spread) and the
// v5 names plugins expect.
export function makeReactRouterV5Compat(v7: ReactRouterCompatInput): Record<string, unknown> {
  // useHistory returns a v5 history backed by v7 navigation. push/replace/go(Back|Forward) map onto
  // useNavigate; `location` comes from useLocation.
  const useHistory = (): unknown => {
    const navigate = v7.useNavigate();
    const location = v7.useLocation();

    return {
      push: (to: unknown) => navigate(to),
      replace: (to: unknown) => navigate(to, { replace: true }),
      goBack: () => navigate(-1),
      goForward: () => navigate(1),
      go: (delta: number) => navigate(delta),
      location,
    };
  };

  // Redirect maps onto v7's <Navigate replace>.
  const Redirect = ({ to }: { to: unknown }): React.ReactElement =>
    React.createElement(v7.Navigate, { to, replace: true });

  // useRouteMatch approximates v5's match from v7 hooks (params + the current pathname). Good enough for
  // the read paths plugins use (params + url); exhaustive path matching is not reproduced.
  const useRouteMatch = (pattern?: string): unknown => {
    const params = v7.useParams();
    const location = v7.useLocation();

    return { params, path: pattern ?? location.pathname, url: location.pathname, isExact: true };
  };

  // withRouter injects v5 history/location/match props via the hooks above (a class-component HOC some
  // older plugins use).
  const withRouter =
    <P extends object>(Component: React.ComponentType<P>): React.FC<P> =>
    (props: P): React.ReactElement => {
      const history = useHistory();
      const location = v7.useLocation();
      const params = v7.useParams();

      return React.createElement(Component, { ...props, history, location, match: { params } } as P);
    };

  return {
    ...v7,
    useHistory,
    // v5 <Switch> renders the first matching child route — v7 <Routes> has the same role. (A plugin using
    // the v5 `<Route component=>` child form inside a Switch is the one unsupported corner; Headlamp's
    // modern plugins register routes through registerRoute instead, which KSail hosts with `element`.)
    Switch: v7.Routes,
    Redirect,
    useRouteMatch,
    withRouter,
  };
}
