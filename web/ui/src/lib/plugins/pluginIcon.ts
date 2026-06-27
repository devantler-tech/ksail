// renderPluginIcon renders a Headlamp icon prop. Headlamp passes icon NAMES as plain strings (e.g.
// "mdi:cog", "simple-icons:flux") in registerSidebarEntry, ActionButton, HoverInfoLabel, etc.; KSail
// renders a string via the bundled @iconify/react `Icon` on window.pluginLib (the icon sets are
// registered offline in externals.ts, so they resolve without the CSP-blocked Iconify API). A ReactNode
// (the plugin already wrapped its own icon) is returned as-is; undefined stays undefined so callers can
// fall back to a default. Shared by commonComponents.tsx and pluginLib.ts so the string→Icon handling
// lives in one place.

import * as React from "react";

export function renderPluginIcon(icon: React.ReactNode): React.ReactNode {
  if (typeof icon !== "string") {
    return icon;
  }

  const iconify = window.pluginLib?.Iconify as { Icon?: React.ComponentType<{ icon: string }> } | undefined;

  return iconify?.Icon ? React.createElement(iconify.Icon, { icon }) : undefined;
}
