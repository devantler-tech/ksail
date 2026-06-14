import { useCallback } from "react";
import { parseDeepLink, type DeepLinkTarget } from "../lib/deepLink.ts";
import { useWailsEvent } from "./useWailsEvent.ts";

// useDeepLinks subscribes to ksail:// deep links delivered by the desktop shell (the "ksail:open" Wails
// event) and calls onNavigate with the parsed target. It is a no-op everywhere except the Wails desktop
// webview — deep links only exist there — via the shared useWailsEvent subscription.
export function useDeepLinks(onNavigate: (target: DeepLinkTarget) => void): void {
  const handle = useCallback(
    (data: unknown) => {
      if (typeof data !== "string") {
        return;
      }

      const target = parseDeepLink(data);
      if (target) {
        onNavigate(target);
      }
    },
    [onNavigate],
  );

  useWailsEvent("ksail:open", handle);
}
