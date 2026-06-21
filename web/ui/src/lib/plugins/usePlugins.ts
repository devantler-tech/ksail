// React hooks bridging the imperative extension registry + plugin loader to the SPA's render cycle.

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { loadPlugins, type LoadedPlugin } from "./loader.ts";
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

// PluginLoaderState is the one-shot plugin load result surfaced by usePluginLoader.
export interface PluginLoaderState {
  plugins: LoadedPlugin[];
  loading: boolean;
  error: string | null;
  reload: () => void;
}

// usePluginLoader loads the installed plugins once when `enabled` becomes true, exposing each plugin's
// load outcome (for the Plugins view) and a reload(). getCluster supplies the active cluster to the K8s
// data shim; it is read through a ref so switching clusters does not reload plugins — the shim reads the
// live cluster on each fetch instead.
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
