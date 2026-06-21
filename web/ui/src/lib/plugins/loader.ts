// The plugin loader fetches the installed plugins from the backend and loads each entry bundle into
// the page so its register*() calls populate the extension registry.
//
// CSP note: the SPA runs under a strict Content-Security-Policy (default-src 'self', no 'unsafe-eval'),
// so Headlamp's own loader — which runs plugin bundles through the Function constructor — would be
// blocked. KSail instead injects each plugin as a same-origin classic <script src> pointing at the
// backend's /api/v1/plugins/{name}/{main} route. A same-origin classic script executes under
// default-src 'self' without 'unsafe-eval', and still satisfies the Headlamp contract (the bundle reads
// the window.pluginLib global), so plugins load with no CSP relaxation.

import { listPlugins, pluginAssetURL, type PluginInfo } from "../../api.ts";
import { installPluginLib, type ClusterRef } from "./pluginLib.ts";
import { registry } from "./registry.ts";

// LoadedPlugin is a plugin's load outcome, surfaced in the Plugins view so a failed bundle is visible
// (with its error) rather than silently absent.
export interface LoadedPlugin {
  info: PluginInfo;
  status: "loaded" | "error";
  error?: string;
}

// installed tracks the one-time window.pluginLib installation so reloads do not reinstall it.
let installed = false;

// loadPlugins fetches the installed plugins and loads each entry bundle in order. It is idempotent: the
// registry is reset first, so a reload reflects the current installed set instead of accumulating
// duplicates. getCluster supplies the active cluster to the K8s data shim (read live on each fetch).
export async function loadPlugins(getCluster: () => ClusterRef | null): Promise<LoadedPlugin[]> {
  if (!installed) {
    installPluginLib(getCluster);
    installed = true;
  }

  registry.reset();

  const { plugins } = await listPlugins();
  const results: LoadedPlugin[] = [];

  for (const info of plugins) {
    // Attribute registrations made during this bundle's synchronous execution to the plugin. A classic
    // script executes before its load event fires, so the context is correct for the common case.
    registry.setPluginContext(info.name);

    try {
      // Awaited in sequence on purpose: plugins load in a deterministic order (async=false below).
      await loadScript(pluginAssetURL(info.name, info.main));
      results.push({ info, status: "loaded" });
    } catch (err) {
      results.push({ info, status: "error", error: err instanceof Error ? err.message : String(err) });
    } finally {
      registry.setPluginContext(undefined);
    }
  }

  return results;
}

// loadScript injects a same-origin classic <script src> and resolves when it has loaded and executed
// (or rejects on a load error). async=false preserves execution order across plugins.
function loadScript(src: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const script = document.createElement("script");
    script.src = src;
    script.async = false;
    script.dataset.ksailPlugin = "true";
    script.addEventListener("load", () => {
      resolve();
    });
    script.addEventListener("error", () => {
      reject(new Error(`failed to load plugin bundle ${src}`));
    });
    document.head.appendChild(script);
  });
}
