import { useCallback, useEffect, useState } from "react";
import { errorMessage, getAppSettings, updateAppSettings, type AppSettings } from "../../api.ts";
import { Button, SelectField, TextField } from "../ui.tsx";
import { ErrorBanner, TableSkeleton } from "../states.tsx";
import { useToast } from "../Toast.tsx";
import { FieldHelp, SettingsSection } from "./SettingsSection.tsx";

const REASONING_OPTIONS = [
  { value: "", label: "Default" },
  { value: "low", label: "Low" },
  { value: "medium", label: "Medium" },
  { value: "high", label: "High" },
];

// EditorChatSettings is the Settings page's "Editor & AI" category: the editor command used for
// interactive flows (overlaid onto EDITOR by the backend) and the AI assistant's model + reasoning
// effort. Backend-persisted (ui-settings.json), so it uses a draft/save flow like Credentials.
export function EditorChatSettings() {
  const toast = useToast();
  const [loaded, setLoaded] = useState<AppSettings | null>(null);
  const [draft, setDraft] = useState<AppSettings>({ editor: "", chat: { model: "", reasoningEffort: "" } });
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    try {
      const response = await getAppSettings();
      setLoaded(response);
      setDraft(response);
      setError(null);
    } catch (err) {
      setError(errorMessage(err));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function handleSave() {
    setSaving(true);
    try {
      const response = await updateAppSettings(draft);
      setLoaded(response);
      setDraft(response);
      toast.success("Settings saved");
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setSaving(false);
    }
  }

  if (error && !loaded) {
    return <ErrorBanner message={error} onRetry={() => void load()} />;
  }

  if (!loaded) {
    return <TableSkeleton />;
  }

  const dirty = JSON.stringify(draft) !== JSON.stringify(loaded);

  return (
    <SettingsSection
      title="Editor & AI"
      description="The editor used for interactive flows and the AI assistant's model."
      footer={
        <Button onClick={() => void handleSave()} loading={saving} disabled={!dirty}>
          Save settings
        </Button>
      }
    >
      <div className="space-y-5">
        <div>
          <TextField
            label="Editor command"
            placeholder="e.g. code --wait"
            autoComplete="off"
            spellCheck={false}
            value={draft.editor}
            onChange={(event) => setDraft({ ...draft, editor: event.target.value })}
          />
          <FieldHelp>
            Used for interactive flows like editing SOPS secrets. Sets the <code>EDITOR</code> variable
            for KSail. Leave blank to use your shell's editor.
          </FieldHelp>
        </div>

        <div>
          <TextField
            label="AI model"
            placeholder="default"
            autoComplete="off"
            spellCheck={false}
            value={draft.chat.model}
            onChange={(event) => setDraft({ ...draft, chat: { ...draft.chat, model: event.target.value } })}
          />
          <FieldHelp>The model the AI assistant uses (GitHub Copilot). Leave blank for the default.</FieldHelp>
        </div>

        <SelectField
          label="Reasoning effort"
          value={draft.chat.reasoningEffort}
          onChange={(event) =>
            setDraft({ ...draft, chat: { ...draft.chat, reasoningEffort: event.target.value } })
          }
        >
          {REASONING_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </SelectField>
      </div>
    </SettingsSection>
  );
}
