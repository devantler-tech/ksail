import { useEffect, useRef } from "react";
import { eventsPath, type Cluster, type ClusterList } from "../api.ts";

interface UseClusterStreamOptions {
  // enabled gates the subscription: open the stream only once the app is authenticated and the
  // initial load succeeded. Flipping it false (e.g. on a lost session) tears the stream down.
  enabled: boolean;
  // onClusters receives each pushed cluster list (the SSE "clusters" event).
  onClusters: (clusters: Cluster[]) => void;
  // onError fires on a stream-level error so the caller can re-sync via fetch (which can detect a
  // lost session and surface the login screen) while EventSource reconnects on its own.
  onError: () => void;
}

// useClusterStream subscribes to the server's Server-Sent Events stream (GET /api/v1/events) and
// pushes live cluster updates to the caller, replacing client-side polling. The browser's
// EventSource reconnects automatically on transient drops; the subscription is torn down when
// disabled or on unmount. Callbacks are held in refs so passing inline functions does not churn the
// subscription on every render.
export function useClusterStream({ enabled, onClusters, onError }: UseClusterStreamOptions) {
  const onClustersRef = useRef(onClusters);
  const onErrorRef = useRef(onError);
  onClustersRef.current = onClusters;
  onErrorRef.current = onError;

  useEffect(() => {
    // Guard against environments without EventSource (very old browsers / non-DOM test runners): the
    // caller keeps its last-known list and can still refresh manually.
    if (!enabled || typeof EventSource === "undefined") {
      return;
    }

    const source = new EventSource(eventsPath);

    source.addEventListener("clusters", (event) => {
      try {
        const list = JSON.parse((event as MessageEvent).data) as ClusterList;
        onClustersRef.current(list.items ?? []);
      } catch {
        // Ignore a malformed frame; the next event (or a reconnect) re-syncs the list.
      }
    });

    // The server emits "stream-error" (not "error", which EventSource reserves for connection
    // failures) when a backend List fails while the connection is healthy. Re-sync via the caller so
    // a sustained backend failure surfaces to the user instead of silently going stale.
    source.addEventListener("stream-error", () => {
      onErrorRef.current();
    });

    source.onerror = () => {
      // Connection-level error: EventSource retries on its own. Ask the caller to re-sync via fetch so
      // a lost session surfaces promptly instead of looping on silent background reconnects.
      onErrorRef.current();
    };

    return () => source.close();
  }, [enabled]);
}
