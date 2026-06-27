// externals.ts lazily installs the heavy Headlamp plugin externals — Material UI, Redux, React Router,
// and common utility libs — onto window.pluginLib. Headlamp plugin bundles mark these as external and
// reference them as pluginLib.MuiMaterial / pluginLib.ReactRedux / pluginLib.ReactRouter / etc. Each is
// pulled with a dynamic import(), so it lands in its own Vite chunk and is fetched only when at least one
// plugin is present (see loader.ts) — KSail's own UI bundle never pays for Material UI unless a plugin
// needs it.
//
// Version note: these are pinned to React-19-compatible majors (MUI v6, react-redux v9, react-router v7),
// which differ from the React-18-era versions Headlamp ships. A plugin built against those majors mostly
// works (the component/hook surfaces are stable), but deep version-specific usage may need per-plugin
// checks. This is the unavoidable cost of running plugins on KSail's React 19 rather than Headlamp's 18.

import { makeReactRouterV5Compat, type ReactRouterCompatInput } from "./reactRouterCompat.ts";

// installPluginExternals augments window.pluginLib with the lazy externals. It is idempotent: it returns
// early once MuiMaterial is present, so repeated plugin (re)loads do not re-import the chunks.
export async function installPluginExternals(): Promise<void> {
  const lib = window.pluginLib;
  if (!lib || lib.MuiMaterial !== undefined) {
    return;
  }

  const [mui, muiIcons, muiStyles, reactRedux, reactRouter, lodash, iconify] = await Promise.all([
    import("@mui/material"),
    import("@mui/icons-material"),
    import("@mui/material/styles"),
    import("react-redux"),
    import("react-router-dom"),
    import("lodash"),
    import("@iconify/react"),
  ]);

  // Expose the full @mui/material namespace, with @mui/material/styles attached as `.styles`: Headlamp's
  // build externalizes the deep import `@mui/material/styles` to `pluginLib.MuiMaterial.styles` (where
  // useTheme/ThemeProvider/createTheme live), and `@mui/material/<Component>` to `pluginLib.MuiMaterial.
  // <Component>` — both satisfied by this merged object. (MuiStyles is kept for @mui/styles consumers.)
  lib.MuiMaterial = { ...(mui as Record<string, unknown>), styles: muiStyles };
  lib.MuiIconsMaterial = muiIcons;
  lib.MuiStyles = muiStyles;
  lib.ReactRedux = reactRedux;
  // Install the v5-shaped react-router shim (not raw v7), so a Headlamp plugin's useHistory/Switch/
  // Redirect — built against react-router v5 — resolve against KSail's v7 runtime (see reactRouterCompat).
  lib.ReactRouter = makeReactRouterV5Compat(reactRouter as unknown as ReactRouterCompatInput);
  // lodash is CJS; expose its callable default (the `_` object) when present, else the namespace.
  lib.Lodash = (lodash as { default?: unknown }).default ?? lodash;
  lib.Iconify = iconify;

  await registerPluginIcons(iconify);
}

// registerPluginIcons registers the bundled Iconify collections so a plugin's `<Icon icon="mdi:cog">`
// resolves OFFLINE: KSail's CSP (default-src 'self') blocks the Iconify API (api.iconify.design) and
// clusters may be airgapped, so the icon data must be local. mdi (functional UI icons) and simple-icons
// (brand logos, e.g. simple-icons:flux) are the sets Headlamp plugins use. Each JSON is its own dynamic
// chunk, fetched only here — alongside the other plugin externals — so KSail's own UI never loads them.
async function registerPluginIcons(iconify: unknown): Promise<void> {
  const addCollection = (iconify as { addCollection?: (collection: unknown) => void }).addCollection;
  if (typeof addCollection !== "function") {
    return;
  }

  const [mdi, simpleIcons] = await Promise.all([
    import("@iconify-json/mdi/icons.json"),
    import("@iconify-json/simple-icons/icons.json"),
  ]);

  addCollection((mdi as { default?: unknown }).default ?? mdi);
  addCollection((simpleIcons as { default?: unknown }).default ?? simpleIcons);
}
