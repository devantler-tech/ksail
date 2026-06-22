import { AlertTriangle, CheckCircle2, Download, RotateCw, Trash2, XCircle } from "lucide-react";
import { useState } from "react";
import { installPlugin, uninstallPlugin } from "../api.ts";
import type { LoadedPlugin } from "../lib/plugins/loader.ts";
import { cx } from "../lib/cx.ts";
import { EmptyState, ErrorBanner } from "./states.tsx";
import { Button } from "./ui.tsx";

// PluginsView is the management surface for installed web-UI plugins: a trust notice, an install form
// (when the backend supports it), and the per-plugin load status (so a bundle that failed to load is
// visible with its error rather than silently absent). It is presentational — App owns the loader state
// (usePluginLoader) and passes it in; install/uninstall call the API directly and then onReload.
export function PluginsView({
  plugins,
  loading,
  error,
  onReload,
  canInstall,
}: {
  plugins: LoadedPlugin[];
  loading: boolean;
  error: string | null;
  onReload: () => void;
  canInstall: boolean;
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

      {canInstall ? <PluginInstallForm onInstalled={onReload} /> : null}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-slate-500 dark:text-slate-400">
          Headlamp-compatible plugins load from{" "}
          <code className="rounded bg-slate-100 px-1 py-0.5 font-mono text-xs text-slate-700 dark:bg-slate-800 dark:text-slate-300">
            ~/.ksail/plugins
          </code>
          {canInstall ? " — install one from a tarball URL above, or drop a folder in." : "."}
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
          description="Install a Headlamp-compatible plugin from a tarball URL, or drop a folder (package.json + main.js) into ~/.ksail/plugins, then reload."
        />
      ) : (
        <ul className="space-y-2">
          {plugins.map((plugin) => (
            <PluginCard
              key={plugin.info.name}
              plugin={plugin}
              canUninstall={canInstall}
              onUninstalled={onReload}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

// inputClass styles the install form's text inputs consistently with the rest of the UI.
const inputClass =
  "w-full rounded-md border border-slate-300 bg-white px-3 py-1.5 text-sm text-slate-900 placeholder:text-slate-400 focus:border-sky-500 focus:outline-none focus:ring-1 focus:ring-sky-500 dark:border-slate-700 dark:bg-slate-950 dark:text-white";

// PluginInstallForm installs a plugin from a tarball URL. Installing runs the plugin's code with full
// cluster access, so the Install action is gated on an explicit consent checkbox (the trust gate);
// signature verification is a follow-up — an optional SHA-256 pins the download in the meantime.
function PluginInstallForm({ onInstalled }: { onInstalled: () => void }) {
  const [url, setUrl] = useState("");
  const [sha256, setSha256] = useState("");
  const [consent, setConsent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [installError, setInstallError] = useState<string | null>(null);

  const canSubmit = url.trim() !== "" && consent && !busy;

  const submit = async (): Promise<void> => {
    setBusy(true);
    setInstallError(null);

    try {
      await installPlugin({ url: url.trim(), sha256: sha256.trim() || undefined });
      setUrl("");
      setSha256("");
      setConsent(false);
      onInstalled();
    } catch (err) {
      setInstallError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form
      className="space-y-3 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900"
      onSubmit={(event) => {
        event.preventDefault();
        if (canSubmit) {
          void submit();
        }
      }}
    >
      <div className="space-y-2">
        <input
          id="plugin-install-url"
          type="url"
          value={url}
          onChange={(event) => setUrl(event.target.value)}
          placeholder="https://…/my-plugin.tar.gz"
          className={inputClass}
        />
        <input
          id="plugin-install-sha"
          type="text"
          value={sha256}
          onChange={(event) => setSha256(event.target.value)}
          placeholder="SHA-256 checksum (optional)"
          className={inputClass}
        />
      </div>
      <label className="flex items-start gap-2 text-xs text-slate-600 dark:text-slate-300">
        <input
          id="plugin-install-consent"
          type="checkbox"
          checked={consent}
          onChange={(event) => setConsent(event.target.checked)}
          className="mt-0.5"
        />
        I understand this plugin will run unsandboxed with full access to my clusters.
      </label>
      {installError ? (
        <p
          id="plugin-install-error"
          className="rounded-md bg-red-50 px-2.5 py-1.5 font-mono text-xs text-red-700 dark:bg-red-500/10 dark:text-red-300"
        >
          {installError}
        </p>
      ) : null}
      <Button id="plugin-install-btn" type="submit" size="sm" disabled={!canSubmit} loading={busy}>
        {busy ? null : <Download className="size-4" aria-hidden />}
        Install plugin
      </Button>
    </form>
  );
}

// PluginCard renders one installed plugin's metadata, its load outcome, and (when supported) an
// uninstall action.
function PluginCard({
  plugin,
  canUninstall,
  onUninstalled,
}: {
  plugin: LoadedPlugin;
  canUninstall: boolean;
  onUninstalled: () => void;
}) {
  const { info, status } = plugin;
  const loaded = status === "loaded";
  const [busy, setBusy] = useState(false);

  const remove = async (): Promise<void> => {
    setBusy(true);

    try {
      await uninstallPlugin(info.name);
      onUninstalled();
    } catch {
      // Re-enable the button; a failed uninstall leaves the plugin in place and a reload will show it.
      setBusy(false);
    }
  };

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
        <div className="flex shrink-0 items-center gap-2">
          <span
            className={cx(
              "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium",
              loaded
                ? "bg-emerald-50 text-emerald-700 ring-1 ring-inset ring-emerald-600/20 dark:bg-emerald-500/10 dark:text-emerald-400"
                : "bg-red-50 text-red-700 ring-1 ring-inset ring-red-600/20 dark:bg-red-500/10 dark:text-red-400",
            )}
          >
            {loaded ? <CheckCircle2 className="size-3.5" aria-hidden /> : <XCircle className="size-3.5" aria-hidden />}
            {loaded ? "Loaded" : "Failed"}
          </span>
          {canUninstall ? (
            <button
              type="button"
              id={`plugin-uninstall-${info.name}`}
              onClick={() => void remove()}
              disabled={busy}
              title={`Uninstall ${info.name}`}
              className="rounded-md p-1.5 text-slate-400 hover:bg-red-50 hover:text-red-600 disabled:opacity-50 dark:hover:bg-red-500/10 dark:hover:text-red-400"
            >
              <Trash2 className="size-4" aria-hidden />
            </button>
          ) : null}
        </div>
      </div>
      {!loaded && plugin.error ? (
        <p className="mt-2 rounded-md bg-red-50 px-2.5 py-1.5 font-mono text-xs text-red-700 dark:bg-red-500/10 dark:text-red-300">
          {plugin.error}
        </p>
      ) : null}
    </li>
  );
}
