import { useCallback, useEffect, useState } from "react";
import { errorMessage, listResources, type K8sObject } from "../api.ts";
import { splitClusterKey } from "../lib/k8s.ts";

// useResourceList fetches a resource kind from a cluster (clusterId = "namespace/name") and exposes the
// list with loading/fetched/error flags plus a refresh() that refetches. Shared by the Resources and
// Events views so the fetch lifecycle lives in one place. An empty clusterId fetches nothing. Deps are
// primitives only, so live cluster-list status churn over SSE never triggers a spurious refetch.
export function useResourceList(clusterId: string, kind: string) {
  const [items, setItems] = useState<K8sObject[]>([]);
  const [loading, setLoading] = useState(clusterId !== "");
  const [hasFetched, setHasFetched] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);

  const refresh = useCallback(() => setNonce((value) => value + 1), []);

  useEffect(() => {
    if (clusterId === "") {
      return undefined;
    }

    const [namespace, name] = splitClusterKey(clusterId);
    let cancelled = false;
    setLoading(true);
    setError(null);

    listResources(namespace, name, kind)
      .then((list) => {
        if (!cancelled) {
          setItems(list.items ?? []);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(errorMessage(err));
          setItems([]);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
          setHasFetched(true);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [clusterId, kind, nonce]);

  return { items, loading, hasFetched, error, refresh };
}
