// useClusterScopedList is the shared React hook behind both Headlamp-compat list surfaces plugins
// consume: `K8s.ResourceClasses.<Kind>.useList()` (k8s.ts) and `K8s.useResourceList()` (pluginLib.ts).
// It fetches a list scoped to the active cluster and re-fetches when the cluster — or any caller-supplied
// dependency (e.g. kind/namespace) — changes, returning [items, error] like Headlamp's useList.
//
// `fetchList` receives the resolved active cluster and returns its items; `deps` are extra reactive
// inputs appended to the cluster keys so the effect also re-runs when they change. It must be called
// from a component render (it is a hook). Both call sites previously inlined this effect verbatim,
// differing only in the item type, the fetch call, and the extra deps — so the logic lives here once.

import * as React from "react";
import type { ClusterRef } from "./pluginLib.ts";

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

    fetchList({ namespace: clusterNamespace, name: clusterName })
      .then((list) => {
        if (active) {
          setItems(list);
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
  }, [clusterName, clusterNamespace, ...deps]);

  return [items, error];
}
