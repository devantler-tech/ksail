// resourceDetail bridges a plugin's resource links (rendered inside the plugin MemoryRouter) to KSail's
// own resource-detail overlay (rendered by App.tsx, OUTSIDE that router). Headlamp plugins link to a
// resource by a built-in route NAME (e.g. <Link routeName="namespace" params={{name}}>); Headlamp ships
// detail routes for every kind, but KSail renders resource detail in its native slide-over instead. So:
//   1. createRouteURL (pluginLib.ts) resolves a built-in route name to a `ksail-detail:` sentinel URL
//      (encodeResourceDetailURL) carrying the resource's coordinates.
//   2. CommonComponents.Link recognizes that sentinel and calls openResourceDetail() instead of navigating
//      the plugin router.
//   3. App.tsx subscribes (subscribeResourceDetail) and renders the native ResourceDetailPanel for the
//      target, fetching it via the kube-proxy (k8s.getObjectByTarget).
// This is the same cross-the-React-boundary module-singleton idiom as pluginNavigation.ts /
// watchStream.setKubeWatchAvailable.

import type { ResourceTarget } from "./k8s.ts";

// RESOURCE_DETAIL_SCHEME marks a URL as "open the native resource-detail overlay" rather than a plugin
// route path. It is an unknown URL scheme, so a stray `<a href>` to it is an inert no-op (the browser
// cannot navigate to it) — the Link's onClick is what actually opens the overlay.
export const RESOURCE_DETAIL_SCHEME = "ksail-detail:";

// encodeResourceDetailURL serializes a ResourceTarget into a ksail-detail: URL (query-string body).
export function encodeResourceDetailURL(target: ResourceTarget): string {
  const params = new URLSearchParams({
    apiVersion: target.apiVersion,
    kind: target.kind,
    plural: target.plural,
    name: target.name,
  });
  if (target.namespace) {
    params.set("namespace", target.namespace);
  }

  return `${RESOURCE_DETAIL_SCHEME}${params.toString()}`;
}

// decodeResourceDetailURL parses a ksail-detail: URL back into a ResourceTarget, or null if the URL is not
// one (or is missing required coordinates), so the Link can fall through to plugin-router navigation.
export function decodeResourceDetailURL(url: string): ResourceTarget | null {
  if (!url.startsWith(RESOURCE_DETAIL_SCHEME)) {
    return null;
  }

  const params = new URLSearchParams(url.slice(RESOURCE_DETAIL_SCHEME.length));
  const apiVersion = params.get("apiVersion");
  const plural = params.get("plural");
  const name = params.get("name");
  if (!apiVersion || !plural || !name) {
    return null;
  }

  return {
    apiVersion,
    kind: params.get("kind") ?? plural,
    plural,
    name,
    namespace: params.get("namespace") ?? undefined,
  };
}

// BuiltinKind describes a built-in Kubernetes kind KSail can resolve a Headlamp route name to.
interface BuiltinKind {
  kind: string;
  apiVersion: string;
  plural: string;
  namespaced: boolean;
}

// BUILTIN_ROUTE_KINDS maps Headlamp's built-in detail route names to their kind. These are the routes
// Headlamp ships for core/apps/batch/networking kinds; a plugin links to them by name (e.g. a table cell's
// namespace link is routeName "namespace"). The generic "customresource" route is handled separately
// (its kind comes from params). Names match Headlamp's createRouteURL route names.
const BUILTIN_ROUTE_KINDS: Record<string, BuiltinKind> = {
  namespace: { kind: "Namespace", apiVersion: "v1", plural: "namespaces", namespaced: false },
  node: { kind: "Node", apiVersion: "v1", plural: "nodes", namespaced: false },
  persistentVolume: { kind: "PersistentVolume", apiVersion: "v1", plural: "persistentvolumes", namespaced: false },
  pod: { kind: "Pod", apiVersion: "v1", plural: "pods", namespaced: true },
  service: { kind: "Service", apiVersion: "v1", plural: "services", namespaced: true },
  configMap: { kind: "ConfigMap", apiVersion: "v1", plural: "configmaps", namespaced: true },
  secret: { kind: "Secret", apiVersion: "v1", plural: "secrets", namespaced: true },
  persistentVolumeClaim: {
    kind: "PersistentVolumeClaim",
    apiVersion: "v1",
    plural: "persistentvolumeclaims",
    namespaced: true,
  },
  serviceAccount: { kind: "ServiceAccount", apiVersion: "v1", plural: "serviceaccounts", namespaced: true },
  deployment: { kind: "Deployment", apiVersion: "apps/v1", plural: "deployments", namespaced: true },
  daemonSet: { kind: "DaemonSet", apiVersion: "apps/v1", plural: "daemonsets", namespaced: true },
  statefulSet: { kind: "StatefulSet", apiVersion: "apps/v1", plural: "statefulsets", namespaced: true },
  replicaSet: { kind: "ReplicaSet", apiVersion: "apps/v1", plural: "replicasets", namespaced: true },
  job: { kind: "Job", apiVersion: "batch/v1", plural: "jobs", namespaced: true },
  cronJob: { kind: "CronJob", apiVersion: "batch/v1", plural: "cronjobs", namespaced: true },
  ingress: { kind: "Ingress", apiVersion: "networking.k8s.io/v1", plural: "ingresses", namespaced: true },
};

// builtinRouteTarget resolves a Headlamp built-in route name + params to a ResourceTarget, or null when the
// name is not a known built-in (so createRouteURL falls back to plugin-registered routes). The generic
// "customresource" route carries its kind in params (group/version/pluralName); the rest are fixed kinds
// keyed by route name.
export function builtinRouteTarget(name: string, params?: Record<string, string>): ResourceTarget | null {
  if (name === "customresource" || name === "customResource") {
    const group = params?.group ?? "";
    const version = params?.version ?? "";
    const plural = params?.pluralName ?? params?.plural ?? "";
    const objectName = params?.name ?? params?.crName ?? "";
    if (!version || !plural || !objectName) {
      return null;
    }

    return {
      apiVersion: group ? `${group}/${version}` : version,
      kind: params?.kind ?? plural,
      plural,
      namespace: params?.namespace,
      name: objectName,
    };
  }

  const builtin = BUILTIN_ROUTE_KINDS[name];
  const objectName = params?.name ?? "";
  if (!builtin || !objectName) {
    return null;
  }

  return {
    apiVersion: builtin.apiVersion,
    kind: builtin.kind,
    plural: builtin.plural,
    namespace: builtin.namespaced ? params?.namespace : undefined,
    name: objectName,
  };
}

// --- current resource-detail target store (useSyncExternalStore-compatible) ---

let current: ResourceTarget | null = null;
const listeners = new Set<() => void>();

// openResourceDetail records the target to inspect and notifies subscribers (App.tsx), which renders the
// native detail overlay for it. Called by CommonComponents.Link on a built-in resource link, and by the
// detail panel itself to drill into a related resource.
export function openResourceDetail(target: ResourceTarget): void {
  current = target;
  listeners.forEach((listener) => {
    listener();
  });
}

// clearResourceDetail closes the overlay (App.tsx passes this as the panel's onClose).
export function clearResourceDetail(): void {
  if (current === null) {
    return;
  }

  current = null;
  listeners.forEach((listener) => {
    listener();
  });
}

export function subscribeResourceDetail(listener: () => void): () => void {
  listeners.add(listener);

  return () => {
    listeners.delete(listener);
  };
}

export function getResourceDetailTarget(): ResourceTarget | null {
  return current;
}
