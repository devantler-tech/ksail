import { AlertTriangle, CheckCircle2, ChevronRight, Download, RotateCw, Search, Trash2, XCircle } from "lucide-react";
import { useState } from "react";
import {
  type CatalogEntry,
  errorMessage,
  installPlugin,
  type PluginCosign,
  searchPluginCatalog,
  uninstallPlugin,
} from "../api.ts";
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
  canBrowseCatalog,
}: {
  plugins: LoadedPlugin[];
  loading: boolean;
  error: string | null;
  onReload: () => void;
  canInstall: boolean;
  canBrowseCatalog: boolean;
}) {
  return (
    <div className="mx-auto max-w-4xl space-y-4">
      {/* Plugins run unsandboxed, with full access to the UI and the user's clusters — make the trust
          boundary explicit, the way Headlamp does for its own (also unsandboxed) plugins. */}
      <div className="flex items-start gap-2.5 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300">
        <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden />
        <p>Plugins run with full access to this UI and your clusters. Only install plugins you trust.</p>
      </div>

      {/* The catalog browses Artifact Hub for Headlamp plugins; installing one still runs unsandboxed,
          so it is offered only when this backend can actually install (canInstall). */}
      {canBrowseCatalog && canInstall ? <PluginCatalogSection onInstalled={onReload} /> : null}

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
            <PluginCard key={plugin.info.name} plugin={plugin} canUninstall={canInstall} onUninstalled={onReload} />
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
// cluster access, so the Install action is gated on an explicit consent checkbox (the trust gate). The
// "Advanced" section exposes the verification tiers, strongest first: cosign/sigstore (the strongest —
// a sigstore bundle verified keyless against an expected certificate identity, or key-based against a
// cosign public key), then a SHA-256 checksum (integrity), then a base64 ed25519 signature (a lighter
// authenticity check against the backend's trusted key, KSAIL_PLUGIN_SIGNING_PUBKEY). All are optional;
// the backend rejects supplied material it cannot verify rather than downgrading.
function PluginInstallForm({ onInstalled }: { onInstalled: () => void }) {
  const [url, setUrl] = useState("");
  const [sha256, setSha256] = useState("");
  const [signature, setSignature] = useState("");
  const [cosign, setCosign] = useState<PluginCosign>({});
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [consent, setConsent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [installError, setInstallError] = useState<string | null>(null);

  const canSubmit = url.trim() !== "" && consent && !busy;

  const submit = async (): Promise<void> => {
    setBusy(true);
    setInstallError(null);

    try {
      await installPlugin({
        url: url.trim(),
        sha256: sha256.trim() || undefined,
        trusted: consent,
        signature: signature.trim() || undefined,
        cosign: cleanCosign(cosign),
      });
      setUrl("");
      setSha256("");
      setSignature("");
      setCosign({});
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
        <button
          type="button"
          id="plugin-install-advanced-toggle"
          onClick={() => setShowAdvanced((shown) => !shown)}
          aria-expanded={showAdvanced}
          aria-controls="plugin-install-advanced"
          className="flex items-center gap-1 text-xs text-slate-500 hover:text-slate-700 dark:text-slate-400 dark:hover:text-slate-200"
        >
          <ChevronRight className={cx("size-3.5 transition-transform", showAdvanced && "rotate-90")} aria-hidden />
          Advanced (integrity &amp; signature)
        </button>
        {showAdvanced ? (
          <div id="plugin-install-advanced" className="space-y-2">
            <input
              id="plugin-install-sha"
              type="text"
              value={sha256}
              onChange={(event) => setSha256(event.target.value)}
              placeholder="SHA-256 checksum (optional)"
              className={inputClass}
            />
            <input
              id="plugin-install-signature"
              type="text"
              value={signature}
              onChange={(event) => setSignature(event.target.value)}
              placeholder="ed25519 signature, base64 (optional)"
              className={inputClass}
            />
            {/* Authenticity is verified against the backend's trusted key (KSAIL_PLUGIN_SIGNING_PUBKEY);
                a signature is rejected when no key is configured. */}
            <p className="text-xs text-slate-400 dark:text-slate-500">
              A signature is verified against the backend's trusted key and rejected when none is configured.
            </p>
            <CosignFields cosign={cosign} onChange={setCosign} />
          </div>
        ) : null}
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

// cleanCosign returns the cosign material to send, or undefined when every field is blank — so an
// untouched Advanced section sends no cosign block and the install falls through to the lighter tiers.
function cleanCosign(cosign: PluginCosign): PluginCosign | undefined {
  const trimmed: PluginCosign = {
    bundle: cosign.bundle?.trim() || undefined,
    publicKey: cosign.publicKey?.trim() || undefined,
    identitySubject: cosign.identitySubject?.trim() || undefined,
    identityIssuer: cosign.identityIssuer?.trim() || undefined,
  };
  const hasMaterial =
    trimmed.bundle !== undefined ||
    trimmed.publicKey !== undefined ||
    trimmed.identitySubject !== undefined ||
    trimmed.identityIssuer !== undefined;

  return hasMaterial ? trimmed : undefined;
}

// CosignFields renders the cosign/sigstore inputs (the strongest verification tier): a sigstore bundle
// (inline), and then either a cosign public key (key-based) or an expected keyless identity
// (subject SAN + OIDC issuer). It is a controlled sub-form — the parent owns the cosign state and sends
// it on install (a verification failure is reported by the backend as an install error).
function CosignFields({ cosign, onChange }: { cosign: PluginCosign; onChange: (next: PluginCosign) => void }) {
  const set = (patch: Partial<PluginCosign>): void => onChange({ ...cosign, ...patch });

  return (
    <div className="space-y-2 rounded-md border border-slate-200 p-2.5 dark:border-slate-700">
      <p className="text-xs font-medium text-slate-600 dark:text-slate-300">Cosign / sigstore (strongest)</p>
      <input
        id="plugin-install-cosign-bundle"
        type="text"
        value={cosign.bundle ?? ""}
        onChange={(event) => set({ bundle: event.target.value })}
        placeholder="sigstore bundle JSON or base64 (optional)"
        className={inputClass}
      />
      <input
        id="plugin-install-cosign-pubkey"
        type="text"
        value={cosign.publicKey ?? ""}
        onChange={(event) => set({ publicKey: event.target.value })}
        placeholder="cosign public key, PEM (key-based)"
        className={inputClass}
      />
      <input
        id="plugin-install-cosign-identity-subject"
        type="text"
        value={cosign.identitySubject ?? ""}
        onChange={(event) => set({ identitySubject: event.target.value })}
        placeholder="expected identity subject / SAN (keyless)"
        className={inputClass}
      />
      <input
        id="plugin-install-cosign-identity-issuer"
        type="text"
        value={cosign.identityIssuer ?? ""}
        onChange={(event) => set({ identityIssuer: event.target.value })}
        placeholder="expected OIDC issuer (keyless)"
        className={inputClass}
      />
      <p className="text-xs text-slate-400 dark:text-slate-500">
        Provide a sigstore bundle, then a public key (key-based) or an expected identity (keyless, verified against the
        public-good trust root). The install is rejected if verification fails.
      </p>
    </div>
  );
}

// PluginCatalogSection searches the backend's installable-plugin catalog (Artifact Hub Headlamp
// plugins) and lists the results, each with an Install button that runs the existing tarball-URL
// install flow. It owns the search query and results; onInstalled refreshes the installed list above.
function PluginCatalogSection({ onInstalled }: { onInstalled: () => void }) {
  const [query, setQuery] = useState("");
  const [entries, setEntries] = useState<CatalogEntry[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);
  const [consent, setConsent] = useState(false);

  const runSearch = async (): Promise<void> => {
    setBusy(true);
    setSearchError(null);

    try {
      const result = await searchPluginCatalog(query);
      setEntries(result.entries);
    } catch (err) {
      setSearchError(errorMessage(err));
      setEntries(null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="space-y-3 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      <div>
        <h2 className="text-sm font-medium text-slate-900 dark:text-white">Browse the plugin catalog</h2>
        <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
          Search Artifact Hub for Headlamp-compatible plugins and install one directly.
        </p>
      </div>
      <form
        className="flex gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          if (!busy) {
            void runSearch();
          }
        }}
      >
        <input
          id="plugin-catalog-search"
          type="search"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Search plugins (e.g. flux, kubescape)…"
          className={inputClass}
        />
        <Button id="plugin-catalog-search-btn" type="submit" size="sm" loading={busy}>
          {busy ? null : <Search className="size-4" aria-hidden />}
          Search
        </Button>
      </form>
      {searchError ? (
        <p
          id="plugin-catalog-error"
          className="rounded-md bg-red-50 px-2.5 py-1.5 font-mono text-xs text-red-700 dark:bg-red-500/10 dark:text-red-300"
        >
          {searchError}
        </p>
      ) : null}
      {entries && entries.length > 0 ? (
        <label className="flex items-start gap-2 text-xs text-slate-600 dark:text-slate-300">
          <input
            id="plugin-catalog-consent"
            type="checkbox"
            checked={consent}
            onChange={(event) => setConsent(event.target.checked)}
            className="mt-0.5"
          />
          I understand a catalog plugin runs unsandboxed with full access to my clusters.
        </label>
      ) : null}
      <PluginCatalogResults entries={entries} onInstalled={onInstalled} consented={consent} />
    </section>
  );
}

// PluginCatalogResults renders the catalog search outcome: nothing before the first search, an empty
// notice when a search returns no matches, otherwise the result rows.
function PluginCatalogResults({
  entries,
  onInstalled,
  consented,
}: {
  entries: CatalogEntry[] | null;
  onInstalled: () => void;
  consented: boolean;
}) {
  if (entries === null) {
    return null;
  }

  if (entries.length === 0) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">No plugins matched your search.</p>;
  }

  return (
    <ul className="space-y-2">
      {entries.map((entry) => (
        <CatalogEntryRow
          key={`${entry.repository ?? ""}/${entry.name}@${entry.version ?? ""}`}
          entry={entry}
          onInstalled={onInstalled}
          consented={consented}
        />
      ))}
    </ul>
  );
}

// CatalogEntryRow renders one catalog result with an Install button that runs the existing tarball-URL
// install flow, then refreshes the installed list. It tracks its own busy/installed/error state so a
// failure surfaces on the row without disturbing the rest of the results.
function CatalogEntryRow({
  entry,
  onInstalled,
  consented,
}: {
  entry: CatalogEntry;
  onInstalled: () => void;
  consented: boolean;
}) {
  const [busy, setBusy] = useState(false);
  const [installed, setInstalled] = useState(false);
  const [installError, setInstallError] = useState<string | null>(null);

  const install = async (): Promise<void> => {
    setBusy(true);
    setInstallError(null);

    try {
      // Forward the catalog's published SHA-256 (when present) so the install flow verifies integrity.
      await installPlugin({ url: entry.url, sha256: entry.checksum, trusted: consented });
      setInstalled(true);
      onInstalled();
    } catch (err) {
      setInstallError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <li className="rounded-md border border-slate-200 p-3 dark:border-slate-800">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="truncate font-medium text-slate-900 dark:text-white">{entry.name}</span>
            {entry.version ? (
              <span className="rounded bg-slate-100 px-1.5 py-0.5 font-mono text-xs text-slate-500 dark:bg-slate-800 dark:text-slate-400">
                v{entry.version}
              </span>
            ) : null}
          </div>
          {entry.description ? (
            <p className="mt-1 line-clamp-2 text-sm text-slate-500 dark:text-slate-400">{entry.description}</p>
          ) : null}
          {entry.repository ? (
            <p className="mt-1 font-mono text-xs text-slate-400 dark:text-slate-500">{entry.repository}</p>
          ) : null}
        </div>
        <Button
          id={`plugin-catalog-install-${entry.url}`}
          variant="secondary"
          size="sm"
          onClick={() => void install()}
          disabled={busy || installed || !consented}
          loading={busy}
        >
          {busy ? null : installed ? (
            <CheckCircle2 className="size-4" aria-hidden />
          ) : (
            <Download className="size-4" aria-hidden />
          )}
          {installed ? "Installed" : "Install"}
        </Button>
      </div>
      {installError ? (
        <p className="mt-2 rounded-md bg-red-50 px-2.5 py-1.5 font-mono text-xs text-red-700 dark:bg-red-500/10 dark:text-red-300">
          {installError}
        </p>
      ) : null}
    </li>
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
