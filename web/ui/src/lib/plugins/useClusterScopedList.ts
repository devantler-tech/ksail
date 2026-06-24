// useClusterScopedList is the shared React hook behind both Headlamp-compat list surfaces plugins
// consume: `K8s.ResourceClasses.<Kind>.useList()` (k8s.ts) and `K8s.useResourceList()` (pluginLib.ts).
// It fetches a list scoped to the active cluster, re-fetches when the cluster — or any caller-supplied
// dependency (e.g. kind/namespace) — changes, and keeps the list current, returning [items, error]
// like Headlamp's useList.
//
// `fetchList` receives the resolved active cluster and returns its items; `deps` are extra reactive
// inputs appended to the cluster keys so the effect also re-runs when they change. It must be called
// from a component render (it is a hook).
//
// Live updates come from one of three sources, transparently to the caller (the same applied watch
// events flow through whichever transport is live). After seeding with one fetchList, the hook prefers
// a live watch and does NOT poll while one is running:
//   - The Headlamp WebSocket multiplexer (wsMultiplexer.ts) when the backend advertises the
//     wsMultiplexer capability AND the binding carries a `mux` subscription — one socket multiplexes
//     every list's watch, faithfully reproducing Headlamp's WebSocketManager. Preferred when available.
//   - The per-list apiserver WATCH over SSE (watchStream.ts) when wsMultiplexer is unavailable but the
//     backend advertises kubeWatch AND the binding carries a non-empty `url`.
//   - Otherwise (no capability, no binding, or the live watch errors/closes) it falls back to re-running
//     fetchList on a fixed interval, pausing while the tab is hidden and refetching when it returns.
// Either way the caller observes a live-updating [items, error] — the signature is unchanged.

import * as React from "react";
import type { ClusterRef } from "./pluginLib.ts";
import {
  isKubeWatchAvailable,
  kubeObjectKey,
  openWatchStream,
  type RawKubeObject,
  type WatchEvent,
} from "./watchStream.ts";
import {
  isWSMultiplexerAvailable,
  type MuxSubscription,
  subscribeWatchMux,
} from "./wsMultiplexer.ts";

// PLUGIN_LIST_POLL_INTERVAL_MS is how often the hook re-runs fetchList when polling (no live watch).
// Five seconds mirrors a typical Headlamp/Kubernetes dashboard refresh cadence — frequent enough to feel
// live, infrequent enough not to hammer the read-only kube-proxy.
export const PLUGIN_LIST_POLL_INTERVAL_MS = 5000;

// WatchBinding tells useClusterScopedList how to keep a list live via an apiserver WATCH. It is
// optional: a caller whose endpoint has no watch analogue (e.g. pluginLib's allowlisted /resources
// reader) omits it and the hook polls. The binding maps raw watch objects to the caller's item type and
// derives a stable key so an incremental event updates the right row.
export interface WatchBinding<T> {
  // url is the SSE watch endpoint to open (built by the caller from the same cluster/path context as the
  // fetcher, via watchStream.watchStreamURL). An empty string disables the SSE watch (e.g. no active
  // cluster), so the hook falls through to the multiplexer (if bound) or polling.
  url: string;
  // mux, when set, identifies the same collection to the Headlamp WebSocket multiplexer (one socket,
  // many watches). useClusterScopedList prefers it over the per-list SSE EventSource when the backend
  // advertises the wsMultiplexer capability, falling back to SSE (url) then polling. Omitted by callers
  // whose endpoint has no multiplexer route (e.g. the allowlisted /resources reader), which keep polling.
  mux?: MuxSubscription;
  // toItem wraps a raw apiserver object (a watch event's `object`) into the caller's item type, mirroring
  // how the fetcher wraps listed objects (e.g. new KubeObject(raw)), so watched and listed items match.
  toItem: (raw: RawKubeObject) => T;
  // keyOf derives the stable identity (uid, fallback namespace/name) an upsert/remove matches on. The
  // fetcher's items and watched items must key identically so a MODIFIED replaces the listed row.
  keyOf: (item: T) => string;
}

// applyWatchEvent returns the next items array after applying one watch event: ADDED/MODIFIED upsert the
// object by key (replace in place, else append), DELETED removes it, and other verbs (BOOKMARK/ERROR)
// leave the list unchanged. Pure so it composes inside a setItems updater.
function applyWatchEvent<T>(items: T[], event: WatchEvent, binding: WatchBinding<T>): T[] {
  const key = kubeObjectKey(event.object);

  if (event.type === "DELETED") {
    return items.filter((item) => binding.keyOf(item) !== key);
  }

  if (event.type !== "ADDED" && event.type !== "MODIFIED") {
    return items;
  }

  const next = binding.toItem(event.object);
  const index = items.findIndex((item) => binding.keyOf(item) === key);

  if (index === -1) {
    return [...items, next];
  }

  const copy = items.slice();
  copy[index] = next;

  return copy;
}

export function useClusterScopedList<T>(
  getCluster: () => ClusterRef | null,
  fetchList: (cluster: ClusterRef) => Promise<T[]>,
  deps: React.DependencyList = [],
  watch?: WatchBinding<T>,
): [T[], Error | null] {
  const [items, setItems] = React.useState<T[]>([]);
  const [error, setError] = React.useState<Error | null>(null);

  // Keep the latest watch binding in a ref so the effect reads it without listing it in deps: callers
  // pass a fresh binding object each render, but the effect must restart only on a cluster/deps change
  // (which is also when the watch URL changes), not on every render.
  const watchRef = React.useRef(watch);
  watchRef.current = watch;

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
    let closeWatch: (() => void) | null = null;
    let interval: number | undefined;

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

    // startPolling installs the interval + visibility refetch that keep the list current without a watch.
    // It is the live source when no watch runs, and the fallback when a watch errors.
    const startPolling = (): void => {
      if (interval !== undefined) {
        return;
      }

      // After the initial load, poll on an interval; pause while the tab is hidden and refetch
      // immediately when it becomes visible again, so a backgrounded UI does not poll needlessly yet is
      // up to date the moment it is looked at.
      interval = window.setInterval(() => {
        if (document.visibilityState !== "hidden") {
          load();
        }
      }, PLUGIN_LIST_POLL_INTERVAL_MS);
    };

    // tryWatch opens a live watch after the initial fetch, preferring the Headlamp WebSocket multiplexer
    // (one shared socket) when it is available and the binding carries a `mux` subscription, otherwise the
    // per-list SSE EventSource. Incremental events update items in place; a connection error closes the
    // watch and falls back to polling, so the list never goes stale even if the stream drops. Returns true
    // when a live watch was established (so the caller does not also start polling).
    const tryWatch = (): boolean => {
      const binding = watchRef.current;
      if (!binding) {
        return false;
      }

      const handlers = {
        onEvent: (event: WatchEvent): void => {
          if (active) {
            setItems((current) => applyWatchEvent(current, event, binding));
          }
        },
        onError: (): void => {
          if (closeWatch) {
            closeWatch();
            closeWatch = null;
          }
          // The watch dropped; resync once and resume polling so the list stays live.
          if (active) {
            load();
            startPolling();
          }
        },
      };

      // Prefer the multiplexer when the backend serves it and the binding identifies the collection.
      if (binding.mux && isWSMultiplexerAvailable()) {
        closeWatch = subscribeWatchMux(binding.mux, handlers);

        return closeWatch !== null;
      }

      // Fall back to the per-list SSE watch when the apiserver-watch capability is advertised.
      if (binding.url !== "" && isKubeWatchAvailable()) {
        closeWatch = openWatchStream(binding.url, handlers);

        return closeWatch !== null;
      }

      return false;
    };

    // Initial fetch, then prefer a live watch; only poll when a watch is not running.
    load();
    if (!tryWatch()) {
      startPolling();
    }

    const onVisibilityChange = (): void => {
      if (document.visibilityState !== "hidden") {
        load();
      }
    };

    document.addEventListener("visibilitychange", onVisibilityChange);

    return () => {
      active = false;
      if (interval !== undefined) {
        window.clearInterval(interval);
      }
      if (closeWatch) {
        closeWatch();
      }
      document.removeEventListener("visibilitychange", onVisibilityChange);
    };
  }, [clusterName, clusterNamespace, ...deps]);

  return [items, error];
}
