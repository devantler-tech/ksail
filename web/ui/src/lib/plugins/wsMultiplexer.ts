// wsMultiplexer is KSail's faithful reproduction of Headlamp's WebSocket multiplexer client
// (frontend/src/lib/k8s/api/v2/multiplexer.ts). It speaks the exact wire protocol KSail's backend
// /wsMultiplexer endpoint serves (pkg/webui/api/wsmultiplexer.go), so:
//
//   - a Headlamp plugin that imports Headlamp's `WebSocketManager` finds an API-compatible object on
//     window.pluginLib.WebSocketManager and multiplexes its resource watches over one socket, and
//   - KSail's own plugin K8s data layer (useAsyncList) routes its list watches through the same single
//     multiplexer connection when the backend advertises the wsMultiplexer capability.
//
// Wire protocol reproduced verbatim from Headlamp (cited so it cannot silently drift):
//   - Endpoint: `${baseWsUrl}/wsMultiplexer` (MULTIPLEXER_ENDPOINT = 'wsMultiplexer').
//   - Subscribe: send {clusterId, path, query, userId, type: 'REQUEST'}.
//   - Unsubscribe: send {clusterId, path, query, userId, type: 'CLOSE'}.
//   - Correlation key: `${clusterId}:${path}:${query}` (createKey) — userId is NOT part of the key.
//   - Server frames: {clusterId, path, query, data, type}; type 'COMPLETE' marks a stream's end,
//     type 'ERROR' carries a JSON {error} in `data`, otherwise `data` is the JSON the listener receives.
//
// Like Headlamp it multiplexes onto a single socket, debounces unsubscribes to survive React
// StrictMode's mount/unmount churn, and resubscribes everything after a reconnect.

import type { WatchEvent, WatchEventType, WatchStreamHandlers } from "./watchStream.ts";

// WSMultiplexerMessage is the wire frame, byte-compatible with Headlamp's WebSocketMessage and KSail's
// backend wsMessage. The same shape is sent (REQUEST/CLOSE) and received (DATA/COMPLETE/ERROR).
export interface WSMultiplexerMessage {
  clusterId: string;
  path: string;
  query: string;
  userId: string;
  data?: string;
  type: "REQUEST" | "CLOSE" | "COMPLETE" | "DATA" | "ERROR";
}

// MULTIPLEXER_ENDPOINT is Headlamp's multiplexer path, appended to the base WS URL.
const MULTIPLEXER_ENDPOINT = "wsMultiplexer";

// UNSUBSCRIBE_DEBOUNCE_MS matches Headlamp's 100ms: a rapid unmount/remount (StrictMode, route change)
// must not churn the socket, so a CLOSE is delayed and cancelled if the same key re-subscribes.
const UNSUBSCRIBE_DEBOUNCE_MS = 100;

// wsMultiplexerAvailable mirrors the backend's capabilities.wsMultiplexer flag. The app sets it once
// config loads (setWSMultiplexerAvailable); the plugin K8s layer reads it before preferring the
// multiplexer over the SSE watch. Defaults false so nothing connects before config is known.
let wsMultiplexerAvailable = false;

// setWSMultiplexerAvailable records whether the serving backend exposes /wsMultiplexer.
export function setWSMultiplexerAvailable(available: boolean): void {
  wsMultiplexerAvailable = available;
}

// isWSMultiplexerAvailable reports the last value set by setWSMultiplexerAvailable.
export function isWSMultiplexerAvailable(): boolean {
  return wsMultiplexerAvailable;
}

// baseWsUrl derives the WebSocket origin from the page origin (http→ws, https→wss), matching Headlamp's
// getBaseWsUrl(). The multiplexer is same-origin (the SPA is served by the same backend), so no host is
// configured here. Falls back to an empty string in non-browser/test environments without window.
function baseWsUrl(): string {
  if (typeof window === "undefined" || !window.location) {
    return "";
  }

  return window.location.origin.replace(/^http/, "ws");
}

// MessageListener receives the parsed `data` payload of a DATA frame (the apiserver watch object), or the
// synthesized Status object on an ERROR with no error listener — mirroring Headlamp's listener contract.
type MessageListener = (data: unknown) => void;
type ErrorListener = (error: Error) => void;

// createKey builds Headlamp's subscription key. Kept a free function (not a method) so it is identical
// to the backend's wsSubscriptionKey and trivially unit-testable.
export function createKey(clusterId: string, path: string, query: string): string {
  return `${clusterId}:${path}:${query}`;
}

// WSMultiplexerManager reproduces Headlamp's WebSocketManager singleton. It is exported as an object (not
// a class) so window.pluginLib.WebSocketManager has the same shape a Headlamp plugin expects to bind to.
export const WebSocketManager = {
  socketMultiplexer: null as WebSocket | null,
  connecting: false,
  isReconnecting: false,
  listeners: new Map<string, Set<MessageListener>>(),
  errorListeners: new Map<string, Set<ErrorListener>>(),
  completedPaths: new Set<string>(),
  activeSubscriptions: new Map<string, { clusterId: string; path: string; query: string }>(),
  pendingUnsubscribes: new Map<string, ReturnType<typeof setTimeout>>(),

  createKey,

  // connect returns the open socket, opening one if needed. Concurrent callers during an in-progress
  // connect await the same attempt (polling readyState, as Headlamp does) rather than opening a second
  // socket. On reconnect it resubscribes every active subscription so live lists recover transparently.
  async connect(): Promise<WebSocket> {
    if (this.socketMultiplexer?.readyState === WebSocket.OPEN) {
      return this.socketMultiplexer;
    }

    if (this.connecting) {
      return this.awaitConnecting();
    }

    this.connecting = true;
    const wsUrl = `${baseWsUrl()}/${MULTIPLEXER_ENDPOINT}`;

    return new Promise<WebSocket>((resolve, reject) => {
      let socket: WebSocket;
      try {
        socket = new WebSocket(wsUrl);
      } catch (error) {
        this.connecting = false;
        reject(error instanceof Error ? error : new Error(String(error)));

        return;
      }

      socket.onopen = (): void => {
        this.socketMultiplexer = socket;
        this.connecting = false;
        if (this.isReconnecting) {
          this.resubscribeAll(socket);
        }
        this.isReconnecting = false;
        resolve(socket);
      };

      socket.onmessage = (event: MessageEvent): void => this.handleMessage(event);

      socket.onerror = (): void => {
        this.connecting = false;
        reject(new Error("WebSocket connection failed"));
      };

      socket.onclose = (): void => this.handleClose();
    });
  },

  // awaitConnecting resolves once an in-progress connect reaches OPEN, or rejects if that attempt fails
  // (clears `connecting`). Polls every 100ms, matching Headlamp's deliberately simple approach.
  awaitConnecting(): Promise<WebSocket> {
    return new Promise<WebSocket>((resolve, reject) => {
      const check = setInterval(() => {
        if (this.socketMultiplexer?.readyState === WebSocket.OPEN) {
          clearInterval(check);
          resolve(this.socketMultiplexer);
        } else if (!this.connecting) {
          clearInterval(check);
          reject(new Error("WebSocket connection failed"));
        }
      }, 100);
    });
  },

  // resubscribeAll re-sends a REQUEST for every active subscription on a freshly opened socket.
  resubscribeAll(socket: WebSocket): void {
    this.activeSubscriptions.forEach(({ clusterId, path, query }) => {
      socket.send(JSON.stringify(this.requestMessage(clusterId, path, query)));
    });
  },

  // requestMessage builds the REQUEST control frame. userId is empty: KSail authenticates the whole
  // connection (session cookie / auth guard), so per-message userId is only echoed, not used for authz.
  requestMessage(clusterId: string, path: string, query: string): WSMultiplexerMessage {
    return { clusterId, path, query, userId: "", type: "REQUEST" };
  },

  // subscribe registers a listener for {clusterId, path, query}, opens the socket if needed, and sends a
  // REQUEST. It returns an unsubscribe function (the cleanup React effects call). Multiple listeners for
  // the same key share one upstream subscription — the watch is opened once and closed when the last
  // listener leaves.
  async subscribe(
    clusterId: string,
    path: string,
    query: string,
    onMessage: MessageListener,
    onError?: ErrorListener,
  ): Promise<() => void> {
    const key = this.createKey(clusterId, path, query);
    this.activeSubscriptions.set(key, { clusterId, path, query });

    const listeners = this.listeners.get(key) ?? new Set<MessageListener>();
    listeners.add(onMessage);
    this.listeners.set(key, listeners);

    if (onError) {
      const errs = this.errorListeners.get(key) ?? new Set<ErrorListener>();
      errs.add(onError);
      this.errorListeners.set(key, errs);
    }

    const socket = await this.connect();
    socket.send(JSON.stringify(this.requestMessage(clusterId, path, query)));

    return () => this.unsubscribe(key, clusterId, path, query, onMessage, onError);
  },

  // unsubscribe removes a listener and, once no listeners remain for the key, debounces sending the CLOSE
  // (cancelled if a re-subscribe arrives within the window) — Headlamp's churn-avoidance behaviour.
  unsubscribe(
    key: string,
    clusterId: string,
    path: string,
    query: string,
    onMessage: MessageListener,
    onError?: ErrorListener,
  ): void {
    const pending = this.pendingUnsubscribes.get(key);
    if (pending) {
      clearTimeout(pending);
      this.pendingUnsubscribes.delete(key);
    }

    this.removeMessageListener(key, onMessage, clusterId, path, query);

    if (onError) {
      this.removeErrorListener(key, onError);
    }
  },

  // removeMessageListener drops one message listener and, if it was the last, schedules the debounced
  // CLOSE that actually unsubscribes upstream.
  removeMessageListener(
    key: string,
    onMessage: MessageListener,
    clusterId: string,
    path: string,
    query: string,
  ): void {
    const listeners = this.listeners.get(key);
    if (!listeners) {
      return;
    }

    listeners.delete(onMessage);
    if (listeners.size > 0) {
      return;
    }

    this.listeners.delete(key);

    const timeout = setTimeout(() => {
      if (!this.listeners.has(key)) {
        this.activeSubscriptions.delete(key);
        this.completedPaths.delete(key);

        if (this.socketMultiplexer?.readyState === WebSocket.OPEN) {
          const closeMsg: WSMultiplexerMessage = { clusterId, path, query, userId: "", type: "CLOSE" };
          this.socketMultiplexer.send(JSON.stringify(closeMsg));
        }
      }
      this.pendingUnsubscribes.delete(key);
    }, UNSUBSCRIBE_DEBOUNCE_MS);

    this.pendingUnsubscribes.set(key, timeout);
  },

  // removeErrorListener drops one error listener for a key.
  removeErrorListener(key: string, onError: ErrorListener): void {
    const errs = this.errorListeners.get(key);
    if (!errs) {
      return;
    }

    errs.delete(onError);
    if (errs.size === 0) {
      this.errorListeners.delete(key);
    }
  },

  // handleClose resets connection state and flags a reconnect when subscriptions remain, so the next
  // connect() resubscribes them.
  handleClose(): void {
    this.socketMultiplexer = null;
    this.connecting = false;
    this.completedPaths.clear();
    this.isReconnecting = this.activeSubscriptions.size > 0;
  },

  // handleMessage parses one server frame and dispatches it, reproducing Headlamp's handler: ignore
  // frames missing clusterId/path; COMPLETE records completion; ERROR notifies error listeners (parsing
  // data.error); otherwise the parsed `data` object is delivered to the key's message listeners.
  handleMessage(event: MessageEvent): void {
    const frame = parseFrame(event.data);
    if (!frame) {
      return;
    }

    const key = this.createKey(frame.clusterId, frame.path, frame.query ?? "");

    if (frame.type === "COMPLETE") {
      this.completedPaths.add(key);

      return;
    }

    if (frame.type === "ERROR") {
      this.dispatchError(key, frame);

      return;
    }

    const update = parseData(frame.data);
    if (update === undefined) {
      return;
    }

    const listeners = this.listeners.get(key);
    if (!listeners) {
      return;
    }

    for (const listener of listeners) {
      try {
        listener(update);
      } catch (error) {
        console.error("Failed to process WebSocket message:", error);
      }
    }
  },

  // dispatchError notifies a key's error listeners with the parsed data.error (Headlamp's ERROR shape).
  dispatchError(key: string, frame: ServerFrame): void {
    const message = errorMessageFromFrame(frame.data);
    const errs = this.errorListeners.get(key);
    if (!errs || errs.size === 0) {
      return;
    }

    const error = new Error(message);
    for (const errListener of errs) {
      try {
        errListener(error);
      } catch (caught) {
        console.error("Failed to process WebSocket error message:", caught);
      }
    }
  },
};

// ServerFrame is the decoded shape of a server→client frame the handler reads off the wire.
interface ServerFrame {
  clusterId: string;
  path: string;
  query?: string;
  data?: string;
  type: string;
}

// parseFrame decodes one raw WebSocket message into a ServerFrame, returning null for malformed input or
// a frame missing the routing fields (clusterId/path) the key is built from — exactly Headlamp's guard.
function parseFrame(raw: unknown): ServerFrame | null {
  if (typeof raw !== "string") {
    return null;
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return null;
  }

  if (typeof parsed !== "object" || parsed === null) {
    return null;
  }

  const record = parsed as Record<string, unknown>;
  if (typeof record.clusterId !== "string" || typeof record.path !== "string") {
    return null;
  }

  return {
    clusterId: record.clusterId,
    path: record.path,
    query: typeof record.query === "string" ? record.query : "",
    data: typeof record.data === "string" ? record.data : undefined,
    type: typeof record.type === "string" ? record.type : "DATA",
  };
}

// parseData parses a DATA frame's `data` (the apiserver watch object JSON) into an object, returning
// undefined when there is no data or it is malformed so a bad frame is skipped, not crashing the stream.
function parseData(data: string | undefined): unknown {
  if (data === undefined) {
    return undefined;
  }

  try {
    const value: unknown = JSON.parse(data);

    return typeof value === "object" && value !== null ? value : undefined;
  } catch {
    return undefined;
  }
}

// errorMessageFromFrame extracts the error text from an ERROR frame's data: it is a JSON {error} object
// (KSail's backend frames it so), falling back to the raw string or a generic message.
function errorMessageFromFrame(data: string | undefined): string {
  if (data === undefined) {
    return "Unknown error";
  }

  try {
    const parsed = JSON.parse(data) as { error?: unknown };
    if (typeof parsed.error === "string") {
      return parsed.error;
    }
  } catch {
    return data;
  }

  return data;
}

// MuxSubscription identifies a watch to route through the multiplexer: the cluster, the apiserver
// collection path, and a query string (the same triple Headlamp's client keys a subscription on).
export interface MuxSubscription {
  clusterId: string;
  path: string;
  query: string;
}

// muxEventFromData coerces one DATA payload (the parsed apiserver watch object the multiplexer delivers,
// shaped {type, object}) into a WatchEvent, returning null for anything that is not a valid watch frame
// so a stray payload is skipped rather than crashing the list.
function muxEventFromData(data: unknown): WatchEvent | null {
  if (typeof data !== "object" || data === null) {
    return null;
  }

  const record = data as { type?: unknown; object?: unknown };
  if (typeof record.type !== "string" || typeof record.object !== "object" || record.object === null) {
    return null;
  }

  return { type: record.type as WatchEventType, object: record.object as WatchEvent["object"] };
}

// subscribeWatchMux opens a watch through the shared multiplexer connection and forwards decoded events to
// handlers, mirroring openWatchStream's contract so useAsyncList can use either transport interchangeably.
// It returns a synchronous close function (the subscription is established asynchronously; the closer
// unsubscribes once it resolves, and short-circuits if closed before then). A connection failure invokes
// handlers.onError so the caller falls back to SSE/polling.
export function subscribeWatchMux(sub: MuxSubscription, handlers: WatchStreamHandlers): () => void {
  let closed = false;
  let cleanup: (() => void) | null = null;

  const onMessage = (data: unknown): void => {
    const event = muxEventFromData(data);
    if (event) {
      handlers.onEvent(event);
    }
  };

  const onError = (): void => {
    handlers.onError();
  };

  WebSocketManager.subscribe(sub.clusterId, sub.path, sub.query, onMessage, onError)
    .then((unsubscribe) => {
      if (closed) {
        unsubscribe();
      } else {
        cleanup = unsubscribe;
      }
    })
    .catch(() => {
      // The connection could not be established; fall back via onError (the caller resumes SSE/polling).
      handlers.onError();
    });

  return () => {
    closed = true;
    if (cleanup) {
      cleanup();
      cleanup = null;
    }
  };
}
