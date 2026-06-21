import { AlertTriangle, CheckCircle2, RotateCw, XCircle } from "lucide-react";
import type { LoadedPlugin } from "../lib/plugins/loader.ts";
import { cx } from "../lib/cx.ts";
import { EmptyState, ErrorBanner } from "./states.tsx";
import { Button } from "./ui.tsx";

// PluginsView is the management surface for installed web-UI plugins: a trust notice, the install
// location, and the per-plugin load status (so a bundle that failed to load is visible with its error
// rather than silently absent). It is presentational — App owns the loader state (usePluginLoader) and
// passes it in, mirroring how the other views receive their data.
export function PluginsView({
  plugins,
  loading,
  error,
  onReload,
}: {
  plugins: LoadedPlugin[];
  loading: boolean;
  error: string | null;
  onReload: () => void;
}) {
  return (
    <div className="mx-auto max-w-4xl space-y-4">
      {/* Plugins run unsandboxed, with full access to the UI and the user's clusters — make the trust
          boundary explicit, the way Headlamp does for its own (also unsandboxed) plugins. */}
      <div className="flex items-start gap-2.5 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300">
        <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden />
        <p>
          Plugins run with full access to this UI and your clusters. Only install plugins you trust.
        </p>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-slate-500 dark:text-slate-400">
          Headlamp-compatible plugins are loaded from{" "}
          <code className="rounded bg-slate-100 px-1 py-0.5 font-mono text-xs text-slate-700 dark:bg-slate-800 dark:text-slate-300">
            ~/.ksail/plugins
          </code>
          . Each plugin is a folder with a <span className="font-mono text-xs">package.json</span> and an
          entry bundle.
        </p>
        <Button variant="secondary" size="sm" onClick={onReload} loading={loading}>
          {loading ? null : <RotateCw className="size-4" aria-hidden />}
          Reload
        </Button>
      </div>

      {error ? <ErrorBanner message={error} onRetry={onReload} /> : null}

      {plugins.length === 0 && !error ? (
        <EmptyState
          title="No plugins installed"
          description="Drop a Headlamp-compatible plugin (a folder with package.json and main.js) into ~/.ksail/plugins, then reload."
        />
      ) : (
        <ul className="space-y-2">
          {plugins.map((plugin) => (
            <PluginCard key={plugin.info.name} plugin={plugin} />
          ))}
        </ul>
      )}
    </div>
  );
}

// PluginCard renders one installed plugin's metadata and its load outcome.
function PluginCard({ plugin }: { plugin: LoadedPlugin }) {
  const { info, status } = plugin;
  const loaded = status === "loaded";

  return (
    <li className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="truncate font-medium text-slate-900 dark:text-white">{info.title ?? info.name}</span>
            {info.version ? (
              <span className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-xs text-slate-500 dark:bg-slate-800 dark:text-slate-400">
                v{info.version}
              </span>
            ) : null}
          </div>
          {info.description ? (
            <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">{info.description}</p>
          ) : null}
          <p className="mt-1 font-mono text-xs text-slate-400 dark:text-slate-500">{info.name}</p>
        </div>
        <span
          className={cx(
            "inline-flex shrink-0 items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium",
            loaded
              ? "bg-emerald-50 text-emerald-700 ring-1 ring-inset ring-emerald-600/20 dark:bg-emerald-500/10 dark:text-emerald-400"
              : "bg-red-50 text-red-700 ring-1 ring-inset ring-red-600/20 dark:bg-red-500/10 dark:text-red-400",
          )}
        >
          {loaded ? <CheckCircle2 className="size-3.5" aria-hidden /> : <XCircle className="size-3.5" aria-hidden />}
          {loaded ? "Loaded" : "Failed"}
        </span>
      </div>
      {!loaded && plugin.error ? (
        <p className="mt-2 rounded-md bg-red-50 px-2.5 py-1.5 font-mono text-xs text-red-700 dark:bg-red-500/10 dark:text-red-300">
          {plugin.error}
        </p>
      ) : null}
    </li>
  );
}
