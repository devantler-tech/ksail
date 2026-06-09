import { useEffect } from "react";

// WAILS_RUNTIME_PATH is the Wails v3 runtime served by the desktop AssetServer. Held in a variable (not
// a literal) so the bundler treats it as a runtime-only dynamic import — it does not exist on the
// browser surfaces (operator / `ksail ui`) and resolves only inside the Wails webview.
const WAILS_RUNTIME_PATH = "/wails/runtime.js";

type WailsRuntimeEvent = { data: unknown };
type WailsRuntime = {
  Events: { On(name: string, callback: (event: WailsRuntimeEvent) => void): () => void };
};

// useWailsEvent subscribes to a named Wails runtime event and invokes onEvent with each payload. It is
// a no-op everywhere except the Wails desktop webview (gated on the wails:// origin), so the browser
// surfaces are unaffected. Shared by useDeepLinks (ksail:open) and useDesktopCommands (ksail:command).
export function useWailsEvent(name: string, onEvent: (data: unknown) => void): void {
  useEffect(() => {
    if (typeof window === "undefined" || window.location.protocol !== "wails:") {
      return undefined;
    }

    let unsubscribe: (() => void) | undefined;
    let cancelled = false;

    import(/* @vite-ignore */ WAILS_RUNTIME_PATH)
      .then((runtime: WailsRuntime) => {
        if (cancelled) {
          return;
        }

        unsubscribe = runtime.Events.On(name, (event) => onEvent(event.data));
      })
      .catch(() => {
        // Wails runtime unavailable — the subscription is simply inactive. Never break startup.
      });

    return () => {
      cancelled = true;
      unsubscribe?.();
    };
  }, [name, onEvent]);
}
