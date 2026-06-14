import type { Config } from "../api.ts";

// surfaceLabel names the running surface for the sidebar footer branding. The desktop shell serves
// the SPA from the wails:// origin (so it overrides whatever the backend reports); otherwise the
// backend's reported mode distinguishes the in-cluster operator from the local `ksail ui` server. An
// absent mode (an older backend) falls back to "Operator", the historical label.
export function surfaceLabel(mode: Config["mode"]): string {
  if (typeof window !== "undefined" && window.location.protocol === "wails:") {
    return "Desktop";
  }

  return mode === "local" ? "Local" : "Operator";
}
