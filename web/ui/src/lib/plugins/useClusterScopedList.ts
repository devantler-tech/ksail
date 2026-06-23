// useClusterScopedList is the shared React hook behind both Headlamp-compat list surfaces plugins
// consume: `K8s.ResourceClasses.<Kind>.useList()` (k8s.ts) and `K8s.useResourceList()` (pluginLib.ts).
// It fetches a list scoped to the active cluster, re-fetches when the cluster — or any caller-supplied
// dependency (e.g. kind/namespace) — changes, and keeps the list current by polling, returning
// [items, error] like Headlamp's useList.
//
// `fetchList` receives the resolved active cluster and returns its items; `deps` are extra reactive
// inputs appended to the cluster keys so the effect also re-runs when they change. It must be called
// from a component render (it is a hook). Polling is a tractable substitute for Headlamp's WebSocket
// watch multiplexer: the hook re-runs fetchList on a fixed interval (and immediately when the tab
// becomes visible again) so plugin resource tables stay fresh without any backend changes.

import * as React from "react";
import type { ClusterRef } from "./pluginLib.ts";

// PLUGIN_LIST_POLL_INTERVAL_MS is how often the hook re-runs fetchList after the initial load. Five
// seconds mirrors a typical Headlamp/Kubernetes dashboard refresh cadence — frequent enough to feel
// live, infrequent enough not to hammer the read-only kube-proxy.
export const PLUGIN_LIST_POLL_INTERVAL_MS = 5000;

export function useClusterScopedList<T>(
  getCluster: () => ClusterRef | null,
  fetchList: (cluster: ClusterRef) => Promise<T[]>,
  deps: React.DependencyList = [],
): [T[], Error | null] {
  const [items, setItems] = React.useState<T[]>([]);
  const [error, setError] = React.useState<Error | null>(null);

  // Read the active cluster during render and key the effect on its primitive name/namespace, so the
  // list re-fetches on a cluster switch — not only when the caller's own deps change.
  const cluster = getCluster();
  const clusterName = cluster?.name ?? null;
  const clusterNamespace = cluster?.namespace ?? null;

  React.useEffect(() => {
    if (clusterName === null || clusterNamespace === null) {
      setItems([]);

      return undefined;
    }

    let active = true;
    let inFlight = false;

    const load = (): void => {
      // Skip if a fetch is already in flight, so an overlapping poll or visibility refetch cannot
      // resolve out of order and overwrite newer data with an older response.
      if (inFlight) {
        return;
      }

      inFlight = true;

      fetchList({ namespace: clusterNamespace, name: clusterName })
        .then((list) => {
          if (active) {
            setItems(list);
            // Clear a prior error only on success, so a sustained failure stays visible instead of
            // flickering off and back on with each poll.
            setError(null);
          }
        })
        .catch((err: unknown) => {
          if (active) {
            setError(err instanceof Error ? err : new Error(String(err)));
          }
        })
        .finally(() => {
          inFlight = false;
        });
    };

    load();

    // After the initial load, poll on an interval; pause while the tab is hidden and refetch
    // immediately when it becomes visible again, so a backgrounded UI does not poll needlessly yet is
    // up to date the moment it is looked at.
    const interval = window.setInterval(() => {
      if (document.visibilityState !== "hidden") {
        load();
      }
    }, PLUGIN_LIST_POLL_INTERVAL_MS);

    const onVisibilityChange = (): void => {
      if (document.visibilityState !== "hidden") {
        load();
      }
    };

    document.addEventListener("visibilitychange", onVisibilityChange);

    return () => {
      active = false;
      window.clearInterval(interval);
      document.removeEventListener("visibilitychange", onVisibilityChange);
    };
  }, [clusterName, clusterNamespace, ...deps]);

  return [items, error];
}
