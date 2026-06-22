// useAsyncList is the shared list-with-live-updates hook behind the plugin K8s data layer. Both
// Headlamp surfaces KSail reproduces — K8s.ResourceClasses.<Kind>.useList() (k8s.ts) and the
// K8s.useResourceList() shim (pluginLib.ts) — were independently doing the same one-shot fetch in a
// useEffect; this consolidates that into one hook and adds polling so the lists stay current. It is a
// tractable substitute for Headlamp's WebSocket watch multiplexer: instead of streaming, the hook
// re-runs the caller's fetcher on a fixed interval (and immediately when the tab becomes visible
// again), which keeps plugin resource tables fresh without any backend changes.

import * as React from "react";

// PLUGIN_LIST_POLL_INTERVAL_MS is how often the hook re-runs the fetcher after the initial load. Five
// seconds mirrors a typical Headlamp/Kubernetes dashboard refresh cadence — frequent enough to feel
// live, infrequent enough not to hammer the read-only kube-proxy.
export const PLUGIN_LIST_POLL_INTERVAL_MS = 5000;

// useAsyncList fetches a list via the caller-supplied fetcher and keeps it current by polling.
//
//   - It holds [items, error] state and runs `fetcher` in a useEffect keyed on `deps` (the caller
//     passes the cluster/kind/namespace primitives so a change re-fetches from scratch).
//   - An `active` guard discards results from a fetch that was superseded by a deps change or unmount.
//   - `error` is cleared on a successful fetch (not before each attempt), so a transient failure clears
//     once a later poll succeeds without flickering during a sustained outage; an in-flight guard keeps
//     overlapping poll/visibility refetches from racing each other.
//   - After the initial fetch it polls every PLUGIN_LIST_POLL_INTERVAL_MS; the interval is cleared on
//     cleanup.
//   - Polling pauses while the tab is hidden (document.visibilityState === "hidden") and fires an
//     immediate refetch when the tab becomes visible again, so a backgrounded UI does not poll
//     needlessly yet is up to date the moment it is looked at.
export function useAsyncList<T>(
  fetcher: () => Promise<T[]>,
  deps: ReadonlyArray<unknown>,
): [T[], Error | null] {
  const [items, setItems] = React.useState<T[]>([]);
  const [error, setError] = React.useState<Error | null>(null);

  // Keep the latest fetcher in a ref so the polling effect can call it without listing `fetcher` in its
  // deps (callers pass a fresh closure each render; we deliberately re-run only when `deps` change).
  const fetcherRef = React.useRef(fetcher);
  fetcherRef.current = fetcher;

  React.useEffect(() => {
    let active = true;
    let inFlight = false;

    const load = (): void => {
      // Skip if a fetch is already in flight, so an overlapping poll or visibility refetch cannot resolve
      // out of order and overwrite newer data with an older response.
      if (inFlight) {
        return;
      }

      inFlight = true;

      fetcherRef
        .current()
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
    // The caller owns the dependency list; the fetcher is read from a ref (above) so passing a fresh
    // closure each render does not restart polling — only a change in `deps` re-fetches.
  }, deps);

  return [items, error];
}
