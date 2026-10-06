import { Eye, EyeOff } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import {
  errorMessage,
  getSettings,
  getAppSettings,
  updateAppSettings,
  testCredential,
  updateSettings,
  type CredentialSetting,
  type CredentialUpdate,
  type AppSettings,
} from "../../api.ts";
import { cx } from "../../lib/cx.ts";
import { envVarError } from "../../lib/envVar.ts";
import { Button, TextField } from "../ui.tsx";
import { ErrorBanner, TableSkeleton } from "../states.tsx";
import { useToast } from "../Toast.tsx";

interface Draft {
  envVar: string;
  value: string;
  clear: boolean;
}

// TESTABLE_PROVIDERS are the providers whose credentials can be live-tested (a cheap authenticated
// API call). Keyed by the lowercased provider name (the API path segment). AWS is not yet supported.
const TESTABLE_PROVIDERS = new Set(["hetzner", "omni"]);

const SOURCE_TONES: Record<CredentialSetting["source"], string> = {
  store:
    "bg-emerald-50 text-emerald-700 ring-emerald-600/20 dark:bg-emerald-500/10 dark:text-emerald-400 dark:ring-emerald-500/30",
  env: "bg-slate-100 text-slate-600 ring-slate-500/20 dark:bg-slate-700/40 dark:text-slate-300 dark:ring-slate-600/40",
  unset:
    "bg-amber-50 text-amber-700 ring-amber-600/20 dark:bg-amber-500/10 dark:text-amber-400 dark:ring-amber-500/30",
};

function initDrafts(credentials: CredentialSetting[]): Record<string, Draft> {
  const drafts: Record<string, Draft> = {};
  for (const credential of credentials) {
    drafts[credential.key] = {
      envVar: credential.envVar,
      // Secret values are never returned, so the secret input always starts empty (write-only).
      value: credential.secret ? "" : (credential.value ?? ""),
      clear: false,
    };
  }
  return drafts;
}

// computeUpdates diffs the drafts against the loaded settings and returns only the changes.
function computeUpdates(
  credentials: CredentialSetting[],
  drafts: Record<string, Draft>,
): CredentialUpdate[] {
  const updates: CredentialUpdate[] = [];
  for (const credential of credentials) {
    const draft = drafts[credential.key];
    if (!draft) {
      continue;
    }
    const update: CredentialUpdate = { key: credential.key };
    let changed = false;

    if (draft.envVar !== credential.envVar) {
      update.envVar = draft.envVar;
      changed = true;
    }

    if (draft.clear) {
      update.value = "";
      changed = true;
    } else if (credential.secret) {
      // Only send a secret when the user typed one; empty means "leave unchanged".
      if (draft.value !== "") {
        update.value = draft.value;
        changed = true;
      }
    } else if (draft.value !== (credential.value ?? "")) {
      update.value = draft.value;
      changed = true;
    }

    if (changed) {
      updates.push(update);
    }
  }
  return updates;
}

function sourceLabel(credential: CredentialSetting, secureStorage: boolean): string {
  switch (credential.source) {
    case "store":
      return secureStorage ? "Stored securely" : "Stored (this session only)";
    case "env":
      return `From ${credential.envVar}`;
    default:
      return "Not set";
  }
}

function groupByProvider(credentials: CredentialSetting[]): [string, CredentialSetting[]][] {
  const groups = new Map<string, CredentialSetting[]>();
  for (const credential of credentials) {
    const list = groups.get(credential.provider) ?? [];
    list.push(credential);
    groups.set(credential.provider, list);
  }
  return [...groups.entries()];
}

// CredentialsSettings is the Settings page's "Credentials" category: the cloud-provider credential
// editor (envVar name + secret/value), backed by the local UI's settings endpoints. onSaved lets
// the host refresh deployment config after a save (a credential change can flip capabilities).
export function CredentialsSettings({ onSaved }: { onSaved?: () => void }) {
  const toast = useToast();
  const [credentials, setCredentials] = useState<CredentialSetting[] | null>(null);
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  // Defaults to true so the "not persisted" warning never flashes before the first load resolves it.
  const [secureStorage, setSecureStorage] = useState(true);
  // reveal tracks which secret inputs are shown in plain text; testing tracks per-provider test calls.
  const [reveal, setReveal] = useState<Record<string, boolean>>({});
  const [testing, setTesting] = useState<Record<string, boolean>>({});
  const [appSettings, setAppSettings] = useState<AppSettings | null>(null);
  const [savingSSO, setSavingSSO] = useState(false);

  const load = useCallback(async () => {
    // The two reads are independent: a failed preference read must not hide the credential editor.
    const [settings, app] = await Promise.allSettled([getSettings(), getAppSettings()]);
    if (settings.status === "fulfilled") {
      setCredentials(settings.value.credentials);
      setDrafts(initDrafts(settings.value.credentials));
      setSecureStorage(settings.value.secureStorageAvailable);
    }
    // Without a successful read the preference is unknown, so its checkbox stays disabled.
    setAppSettings(app.status === "fulfilled" ? app.value : null);
    const failed = settings.status === "rejected" ? settings : app.status === "rejected" ? app : null;
    setError(failed ? errorMessage(failed.reason) : null);
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  function setDraft(key: string, patch: Partial<Draft>) {
    setDrafts((current) => ({ ...current, [key]: { ...current[key], ...patch } }));
  }

  async function handleSave() {
    if (!credentials) {
      return;
    }
    const updates = computeUpdates(credentials, drafts);
    if (updates.length === 0) {
      toast.success("No changes to save");
      return;
    }
    setSaving(true);
    try {
      const response = await updateSettings({ updates });
      setCredentials(response.credentials);
      setDrafts(initDrafts(response.credentials));
      setSecureStorage(response.secureStorageAvailable);
      toast.success("Settings saved");
      onSaved?.();
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setSaving(false);
    }
  }

  async function handleTest(provider: string) {
    setTesting((current) => ({ ...current, [provider]: true }));
    try {
      const result = await testCredential(provider.toLowerCase());
      if (result.ok) {
        toast.success(`${provider}: ${result.message}`);
      } else {
        toast.error(`${provider}: ${result.message}`);
      }

    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setTesting((current) => ({ ...current, [provider]: false }));
    }
  }

  async function saveSSORenewal(enabled: boolean) {
    if (!appSettings) return;
    setSavingSSO(true);
    try {
      const current = await getAppSettings();
      setAppSettings(await updateAppSettings({ ...current, awsSsoRenewal: enabled }));
      toast.success("AWS SSO preference saved");
      onSaved?.();
    } catch (err: unknown) {
      toast.error(errorMessage(err));
    } finally {
      setSavingSSO(false);
    }
  }

  if (error && !credentials) {
    return <ErrorBanner message={error} onRetry={() => void load()} />;
  }

  if (!credentials) {
    return <TableSkeleton />;
  }

  const hasErrors = credentials.some((credential) => envVarError(drafts[credential.key]?.envVar ?? "") !== null);
  // Test connection validates the saved/overlaid credentials, so a provider with unsaved edits has its
  // Test button disabled — otherwise the result would reflect the old config, not what's on screen.
  const dirtyProviders = new Set(
    groupByProvider(credentials)
      .filter(([, group]) => computeUpdates(group, drafts).length > 0)
      .map(([provider]) => provider),
  );

  return (
    <div className="space-y-6">
      {error ? <ErrorBanner message={error} onRetry={() => void load()} /> : null}
      <section className="rounded-lg border border-slate-200 p-4 dark:border-slate-800">
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={appSettings?.awsSsoRenewal ?? false}
            disabled={!appSettings || savingSSO}
            onChange={(event) => void saveSSORenewal(event.target.checked)} />
          AWS SSO renewal (experimental)
        </label>
        <p className="mt-2 text-sm text-slate-500">
          Offer browser sign-in for an expired AWS SSO session on the selected cluster.
          Background monitoring never starts sign-in.
        </p>
      </section>
      <p className="text-sm text-slate-500 dark:text-slate-400">
        Configure the credentials KSail uses to reach cloud and AI providers. Each credential resolves
        from a stored value when set, otherwise from the named environment variable.
        {secureStorage
          ? " Stored secrets are kept in your operating system's secure store, never written to disk in plain text."
          : ""}
      </p>

      {!secureStorage ? (
        <p className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-700/60 dark:bg-amber-900/20 dark:text-amber-300">
          No OS secure store is available, so secret values entered here are kept only for this
          running session and are <strong>not persisted</strong> — they are lost on restart. For
          durable credentials, set the environment variables shown below instead.
        </p>
      ) : null}

      {groupByProvider(credentials).map(([provider, group]) => (
        <section
          key={provider}
          className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900"
        >
          <div className="mb-3 flex items-center justify-between gap-2">
            <h3 className="text-sm font-semibold text-slate-900 dark:text-white">{provider}</h3>
            {TESTABLE_PROVIDERS.has(provider.toLowerCase()) ? (
              <Button
                variant="secondary"
                size="sm"
                loading={testing[provider]}
                disabled={dirtyProviders.has(provider)}
                title={dirtyProviders.has(provider) ? "Save changes before testing" : undefined}
                onClick={() => void handleTest(provider)}
              >
                Test connection
              </Button>
            ) : null}
          </div>
          <div className="space-y-4">
            {group.map((credential) => {
              const draft = drafts[credential.key];
              const revealed = reveal[credential.key] ?? false;
              const varError = envVarError(draft?.envVar ?? "");

              return (
                <div
                  key={credential.key}
                  className="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:items-start"
                >
                  <div>
                    <TextField
                      label={`${credential.label} — variable`}
                      value={draft?.envVar ?? ""}
                      spellCheck={false}
                      aria-invalid={varError !== null}
                      onChange={(event) => setDraft(credential.key, { envVar: event.target.value })}
                    />
                    {varError ? (
                      <p className="mt-1 text-xs text-red-600 dark:text-red-400">{varError}</p>
                    ) : null}
                  </div>
                  <div>
                    <TextField
                      label={credential.secret ? `${credential.label} (secret)` : credential.label}
                      type={credential.secret && !revealed ? "password" : "text"}
                      autoComplete="off"
                      spellCheck={false}
                      placeholder={
                        credential.secret
                          ? credential.stored
                            ? "•••••••• (stored — leave blank to keep)"
                            : "Enter to store"
                          : ""
                      }
                      value={draft?.value ?? ""}
                      disabled={draft?.clear}
                      onChange={(event) => setDraft(credential.key, { value: event.target.value })}
                      trailing={
                        credential.secret ? (
                          <button
                            type="button"
                            aria-label={revealed ? "Hide value" : "Show value"}
                            onClick={() =>
                              setReveal((current) => ({ ...current, [credential.key]: !revealed }))
                            }
                            className="flex size-8 items-center justify-center rounded text-slate-400 transition-colors hover:text-slate-600 dark:hover:text-slate-200"
                          >
                            {revealed ? (
                              <EyeOff className="size-4" aria-hidden />
                            ) : (
                              <Eye className="size-4" aria-hidden />
                            )}
                          </button>
                        ) : undefined
                      }
                    />
                    <div className="mt-1 flex items-center justify-between gap-2 text-xs text-slate-500 dark:text-slate-400">
                      <span
                        className={cx(
                          "inline-flex items-center rounded-full px-2 py-0.5 font-medium ring-1 ring-inset",
                          SOURCE_TONES[credential.source],
                        )}
                      >
                        {sourceLabel(credential, secureStorage)}
                      </span>
                      {credential.stored ? (
                        <label className="inline-flex items-center gap-1">
                          <input
                            type="checkbox"
                            checked={draft?.clear ?? false}
                            onChange={(event) =>
                              setDraft(credential.key, { clear: event.target.checked })
                            }
                          />
                          Remove stored value
                        </label>
                      ) : null}
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </section>
      ))}

      <div className="flex justify-end">
        <Button onClick={() => void handleSave()} loading={saving} disabled={hasErrors}>
          Save settings
        </Button>
      </div>
    </div>
  );
}
