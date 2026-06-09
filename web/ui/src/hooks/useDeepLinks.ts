import { useEffect } from "react";
import { parseDeepLink, type DeepLinkTarget } from "../lib/deepLink.ts";

// WAILS_RUNTIME_PATH is the Wails v3 runtime the desktop AssetServer serves. It is held in a variable
// (not a string literal) so the bundler and tsc treat it as a runtime-only dynamic import: the module
// does not exist in the browser surfaces (operator / `ksail ui`), and is only resolved inside the Wails
// webview, gated on the wails:// origin below.
const WAILS_RUNTIME_PATH = "/wails/runtime.js";

type WailsRuntimeEvent = { data: unknown };
type WailsRuntime = {
  Events: { On(name: string, callback: (event: WailsRuntimeEvent) => void): () => void };
};

// useDeepLinks subscribes to ksail:// deep links delivered by the desktop shell and calls onNavigate
// with the parsed target. It is a no-op everywhere except the Wails desktop webview — deep links only
// exist there — so the browser bundle and the operator / `ksail ui` surfaces are unaffected.
export function useDeepLinks(onNavigate: (target: DeepLinkTarget) => void): void {
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

        unsubscribe = runtime.Events.On("ksail:open", (event) => {
          if (typeof event.data !== "string") {
            return;
          }

          const target = parseDeepLink(event.data);
          if (target) {
            onNavigate(target);
          }
        });
      })
      .catch(() => {
        // Wails runtime unavailable — deep links are simply inactive. Never break startup.
      });

    return () => {
      cancelled = true;
      unsubscribe?.();
    };
  }, [onNavigate]);
}
