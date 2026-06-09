import { useEffect } from "react";

// WAILS_RUNTIME_PATH is the Wails v3 runtime served by the desktop AssetServer. Held in a variable (not
// a literal) so the bundler treats it as a runtime-only dynamic import — it does not exist on the
// browser surfaces (operator / `ksail ui`) and resolves only inside the Wails webview. Mirrors
// useDeepLinks.ts.
const WAILS_RUNTIME_PATH = "/wails/runtime.js";

type WailsRuntimeEvent = { data: unknown };
type WailsRuntime = {
  Events: { On(name: string, callback: (event: WailsRuntimeEvent) => void): () => void };
};

// DesktopCommand is the action a native desktop menu item asks the SPA to perform over the Wails event
// bridge — the writable counterpart to the navigation deep links handled by useDeepLinks.
export type DesktopCommand = "refresh" | "new-cluster" | "toggle-theme";

// useDesktopCommands subscribes to "ksail:command" events emitted by the desktop shell's native menu
// (e.g. Refresh / New cluster / Toggle theme) and invokes onCommand with the parsed command. It is a
// no-op everywhere except the Wails desktop webview, so the browser surfaces are unaffected.
export function useDesktopCommands(onCommand: (command: DesktopCommand) => void): void {
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

        unsubscribe = runtime.Events.On("ksail:command", (event) => {
          if (typeof event.data === "string") {
            onCommand(event.data as DesktopCommand);
          }
        });
      })
      .catch(() => {
        // Wails runtime unavailable — desktop commands are simply inactive. Never break startup.
      });

    return () => {
      cancelled = true;
      unsubscribe?.();
    };
  }, [onCommand]);
}
