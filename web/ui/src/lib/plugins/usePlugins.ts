// React hooks bridging the imperative extension registry + plugin loader to the SPA's render cycle.

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { loadPlugins, type LoadedPlugin } from "./loader.ts";
import { getPluginLocation, subscribePluginLocation } from "./pluginNavigation.ts";
import type { ClusterRef } from "./pluginLib.ts";
import { registry } from "./registry.ts";

// usePluginRegistry subscribes a component to extension-registry changes. The returned number is the
// registry version (a primitive snapshot) used only to trigger re-renders — read registry.getX() in
// render to get the current entries.
export function usePluginRegistry(): number {
  return useSyncExternalStore(
    (onChange) => registry.subscribe(onChange),
    () => registry.getVersion(),
    () => registry.getVersion(),
  );
}

// usePluginLocation subscribes a component to the current plugin-router pathname (published by
// PluginRouterHost). KSail's sidebar/header use it to highlight the active plugin nav item and title,
// following in-plugin navigation (e.g. a plugin <Link> to a detail page).
export function usePluginLocation(): string {
  return useSyncExternalStore(subscribePluginLocation, getPluginLocation, getPluginLocation);
}

// PluginLoaderState is the one-shot plugin load result surfaced by usePluginLoader.
export interface PluginLoaderState {
  plugins: LoadedPlugin[];
  loading: boolean;
  error: string | null;
  reload: () => void;
}

/**
 * usePluginLoader loads the installed plugins once when `enabled` becomes true, exposing each plugin's
 * load outcome (for the Plugins view) plus a `reload()` trigger.
 *
 * `getCluster` is a function (rather than a `ClusterRef | null`) so the latest value can be held in a
 * ref and read by the K8s data shim at fetch time. That keeps the active cluster out of the load
 * dependencies, so switching clusters does not reload plugins — the shim reads the live cluster on each
 * fetch instead.
 */
export function usePluginLoader(enabled: boolean, getCluster: () => ClusterRef | null): PluginLoaderState {
  const [plugins, setPlugins] = useState<LoadedPlugin[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const clusterRef = useRef(getCluster);
  clusterRef.current = getCluster;

  const reload = useCallback(() => {
    if (!enabled) {
      return;
    }

    setLoading(true);
    setError(null);

    loadPlugins(() => clusterRef.current())
      .then((loaded) => {
        setPlugins(loaded);
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        setLoading(false);
      });
  }, [enabled]);

  useEffect(() => {
    if (enabled) {
      reload();
    }
  }, [enabled, reload]);

  return { plugins, loading, error, reload };
}
