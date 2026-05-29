import { useCallback, useEffect, useState } from "react";
import {
  ApiError,
  getSettings,
  updateSettings,
  type CredentialSetting,
  type CredentialUpdate,
} from "../api.ts";
import { Button, TextField } from "./ui.tsx";
import { ErrorBanner, TableSkeleton } from "./states.tsx";
import { useToast } from "./Toast.tsx";

interface Draft {
  envVar: string;
  value: string;
  clear: boolean;
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    return err.message;
  }
  return err instanceof Error ? err.message : String(err);
}

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

export function SettingsPage({ onSaved }: { onSaved?: () => void }) {
  const toast = useToast();
  const [credentials, setCredentials] = useState<CredentialSetting[] | null>(null);
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  // Defaults to true so the "not persisted" warning never flashes before the first load resolves it.
  const [secureStorage, setSecureStorage] = useState(true);

  const load = useCallback(async () => {
    try {
      const response = await getSettings();
      setCredentials(response.credentials);
      setDrafts(initDrafts(response.credentials));
      setSecureStorage(response.secureStorageAvailable);
      setError(null);
    } catch (err) {
      setError(errorMessage(err));
    }
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

  if (error && !credentials) {
    return (
      <div className="mx-auto max-w-3xl">
        <ErrorBanner message={error} onRetry={() => void load()} />
      </div>
    );
  }

  if (!credentials) {
    return (
      <div className="mx-auto max-w-3xl">
        <TableSkeleton />
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <p className="text-sm text-slate-500 dark:text-slate-400">
        Configure the credentials KSail uses to reach each cloud provider. Each credential resolves
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
          <h2 className="mb-3 text-sm font-semibold text-slate-900 dark:text-white">{provider}</h2>
          <div className="space-y-4">
            {group.map((credential) => {
              const draft = drafts[credential.key];
              return (
                <div
                  key={credential.key}
                  className="grid grid-cols-1 gap-3 sm:grid-cols-2 sm:items-end"
                >
                  <TextField
                    label={`${credential.label} — variable`}
                    value={draft?.envVar ?? ""}
                    spellCheck={false}
                    onChange={(event) => setDraft(credential.key, { envVar: event.target.value })}
                  />
                  <div>
                    <TextField
                      label={credential.secret ? `${credential.label} (secret)` : credential.label}
                      type={credential.secret ? "password" : "text"}
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
                    />
                    <div className="mt-1 flex items-center justify-between gap-2 text-xs text-slate-500 dark:text-slate-400">
                      <span>{sourceLabel(credential, secureStorage)}</span>
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
        <Button onClick={() => void handleSave()} loading={saving}>
          Save settings
        </Button>
      </div>
    </div>
  );
}
