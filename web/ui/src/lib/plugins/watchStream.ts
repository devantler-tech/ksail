// watchStream is the live-update substrate behind the plugin K8s data layer. It opens an EventSource to
// KSail's read-only kube-apiserver WATCH endpoint (see pkg/webui/api/kubewatch.go) and delivers parsed,
// incremental watch events (ADDED/MODIFIED/DELETED) to useAsyncList, which applies them to its list.
//
// This is the SSE fallback transport behind the plugin K8s data layer's live updates. The preferred
// transport is the Headlamp WebSocket multiplexer (wsMultiplexer.ts), which reproduces Headlamp's WS wire
// protocol so a plugin's own WebSocketManager works; useAsyncList uses this SSE watch when the backend
// advertises kubeWatch but not wsMultiplexer (EventSource is same-origin, GET-only, cookie-authenticated,
// and passes through the Wails desktop asset server). When neither is available, or a watch errors, the
// caller falls back to interval polling. Plugins consume K8s.useList() and observe a live-updating list
// regardless of which transport is active.

// kubeWatchAvailable mirrors the backend's capabilities.kubeWatch flag. The app sets it once config is
// loaded (setKubeWatchAvailable); the plugin K8s layer reads it through this module rather than prop-
// drilling capabilities into every hook, so useAsyncList stays a generic [items, error] hook. It
// defaults false so the hook never opens a watch before config is known (it polls until then).
let kubeWatchAvailable = false;

// setKubeWatchAvailable records whether the serving backend streams apiserver watches. Called from the
// app after it fetches /api/v1/config, so the plugin data layer opens a live watch only against a
// backend that can serve it (else it keeps polling).
export function setKubeWatchAvailable(available: boolean): void {
  kubeWatchAvailable = available;
}

// isKubeWatchAvailable reports the last value set by setKubeWatchAvailable. useAsyncList consults it
// before attempting a watch.
export function isKubeWatchAvailable(): boolean {
  return kubeWatchAvailable;
}

// WatchEventType is the apiserver watch verb. BOOKMARK is sent by the apiserver to advance the observed
// resourceVersion without a resource change; the list layer ignores it (no upsert/remove).
export type WatchEventType = "ADDED" | "MODIFIED" | "DELETED" | "BOOKMARK" | "ERROR";

// RawKubeObject is the minimal shape watchStream reads off a watch event's `object` to key it for
// upsert/remove: metadata.uid (preferred) with namespace/name as a fallback for objects without a uid.
export interface RawKubeObject {
  metadata?: { uid?: string; name?: string; namespace?: string };
}

// WatchEvent is one decoded apiserver watch frame: the verb plus the raw object it concerns.
export interface WatchEvent {
  type: WatchEventType;
  object: RawKubeObject;
}

// kubeObjectKey derives a stable identity for a watched object: metadata.uid when present, else
// "namespace/name" (cluster-scoped objects have no namespace, yielding "/name"). It is the key both
// ADDED/MODIFIED upserts and DELETED removals match on, so an update replaces the right row.
export function kubeObjectKey(object: RawKubeObject): string {
  const uid = object.metadata?.uid;
  if (uid) {
    return uid;
  }

  return `${object.metadata?.namespace ?? ""}/${object.metadata?.name ?? ""}`;
}

// watchStreamURL builds the same-origin SSE URL for watching an apiserver collection. It mirrors
// proxyList's URL (the kube-proxy GET) with /proxy swapped for /watch, so the watch observes exactly the
// collection the initial fetch listed. watch=true is forced server-side, so no query is needed here.
export function watchStreamURL(namespace: string, name: string, listPath: string): string {
  const base = `/api/v1/clusters/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`;

  return `${base}/watch/${listPath.replace(/^\//, "")}`;
}

// WatchStreamHandlers are the callbacks watchStream invokes as events arrive. onEvent fires once per
// decoded watch frame; onError fires on a connection-level EventSource error (the caller falls back to
// polling and closes the stream).
export interface WatchStreamHandlers {
  onEvent: (event: WatchEvent) => void;
  onError: () => void;
}

// openWatchStream opens an EventSource to the watch endpoint and forwards decoded "watch" events to
// handlers.onEvent until it is closed (the returned function) or errors (handlers.onError). A malformed
// frame is skipped rather than tearing down the stream. The returned function closes the EventSource and
// is idempotent. Returns null when EventSource is unavailable (e.g. SSR/test env), so the caller polls.
export function openWatchStream(url: string, handlers: WatchStreamHandlers): (() => void) | null {
  if (typeof EventSource === "undefined") {
    return null;
  }

  const source = new EventSource(url);

  // The backend frames each apiserver watch object as an `event: watch` SSE frame (see kubewatch.go).
  source.addEventListener("watch", (message: MessageEvent<string>) => {
    const event = decodeWatchEvent(message.data);
    if (event) {
      handlers.onEvent(event);
    }
  });

  // EventSource fires "error" on a connection failure (and would auto-reconnect); the caller instead
  // closes and falls back to polling, so the list stays current without an unbounded reconnect loop.
  source.addEventListener("error", () => {
    handlers.onError();
  });

  return () => {
    source.close();
  };
}

// decodeWatchEvent parses one watch frame's JSON into a WatchEvent, returning null for malformed input
// or a frame missing the verb/object so a single bad event cannot crash the stream.
function decodeWatchEvent(data: string): WatchEvent | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
  } catch {
    return null;
  }

  if (typeof parsed !== "object" || parsed === null) {
    return null;
  }

  const record = parsed as { type?: unknown; object?: unknown };
  if (typeof record.type !== "string" || typeof record.object !== "object" || record.object === null) {
    return null;
  }

  return { type: record.type as WatchEventType, object: record.object as RawKubeObject };
}
