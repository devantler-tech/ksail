import { useCallback } from "react";
import { useWailsEvent } from "./useWailsEvent.ts";

// DesktopCommand is the action a native desktop menu item asks the SPA to perform over the Wails event
// bridge — the writable counterpart to the navigation deep links handled by useDeepLinks.
export type DesktopCommand = "refresh" | "new-cluster" | "toggle-theme";

// useDesktopCommands subscribes to the "ksail:command" Wails event emitted by the desktop shell's native
// menu (Refresh / New cluster / Toggle theme) and invokes onCommand with the parsed command. A no-op
// outside the Wails webview, via the shared useWailsEvent subscription.
export function useDesktopCommands(onCommand: (command: DesktopCommand) => void): void {
  const handle = useCallback(
    (data: unknown) => {
      if (typeof data === "string") {
        onCommand(data as DesktopCommand);
      }
    },
    [onCommand],
  );

  useWailsEvent("ksail:command", handle);
}
