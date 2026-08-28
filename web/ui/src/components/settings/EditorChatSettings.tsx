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

const PROVIDER_OPTIONS = [
  { value: "", label: "GitHub Copilot (default)" },
  { value: "openai", label: "OpenAI" },
  { value: "anthropic", label: "Anthropic" },
  { value: "gemini", label: "Google Gemini" },
  { value: "azure-openai", label: "Azure OpenAI" },
  { value: "openrouter", label: "OpenRouter" },
  { value: "ollama", label: "Ollama" },
  { value: "openai-compatible", label: "Other OpenAI-compatible API" },
];

const WIRE_API_OPTIONS = [
  { value: "", label: "Chat completions (default)" },
  { value: "completions", label: "Chat completions" },
  { value: "responses", label: "Responses API" },
];

const EMPTY_APP_SETTINGS: AppSettings = {
  editor: "",
  chat: {
    provider: "",
    model: "",
    reasoningEffort: "",
    baseUrl: "",
    apiKeyEnvVar: "",
    wireApi: "",
    azureApiVersion: "",
  },
};

// EditorChatSettings is the Settings page's "Editor & AI" category: the editor command used for
// interactive flows and the provider-neutral AI assistant settings. Secrets remain in Credentials;
// this section persists only provider metadata and an optional environment-variable name.
export function EditorChatSettings() {
  const toast = useToast();
  const [loaded, setLoaded] = useState<AppSettings | null>(null);
  const [draft, setDraft] = useState<AppSettings>(EMPTY_APP_SETTINGS);
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
  const provider = draft.chat.provider || "copilot";
  const usesAPIProvider = provider !== "copilot";
  const usesOpenAIWire = usesAPIProvider && provider !== "anthropic";

  return (
    <SettingsSection
      title="Editor & AI"
      description="The editor used for interactive flows and the API provider backing the AI assistant."
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

        <SelectField
          label="AI provider"
          value={draft.chat.provider}
          onChange={(event) =>
            setDraft({
              ...draft,
              chat: {
                ...draft.chat,
                provider: event.target.value,
                model: "",
                baseUrl: "",
                apiKeyEnvVar: "",
                wireApi: "",
                azureApiVersion: "",
              },
            })
          }
        >
          {PROVIDER_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </SelectField>

        <div>
          <TextField
            label="AI model"
            placeholder={usesAPIProvider ? "required" : "default"}
            autoComplete="off"
            spellCheck={false}
            value={draft.chat.model}
            onChange={(event) => setDraft({ ...draft, chat: { ...draft.chat, model: event.target.value } })}
          />
          <FieldHelp>
            {usesAPIProvider
              ? "The provider model or Azure deployment name. Required for API providers."
              : "Optional Copilot model override. Leave blank for the default."}
          </FieldHelp>
        </div>

        {usesAPIProvider && (
          <>
            <div>
              <TextField
                label="Base URL"
                placeholder={
                  provider === "azure-openai" || provider === "openai-compatible"
                    ? "required"
                    : "provider default"
                }
                autoComplete="off"
                spellCheck={false}
                value={draft.chat.baseUrl}
                onChange={(event) =>
                  setDraft({ ...draft, chat: { ...draft.chat, baseUrl: event.target.value } })
                }
              />
              <FieldHelp>
                Required for Azure and custom endpoints; otherwise leave blank for the provider default.
                Azure expects only the resource host, without <code>/openai/v1</code>.
              </FieldHelp>
            </div>

            <div>
              <TextField
                label="API key environment variable"
                placeholder="provider default"
                autoComplete="off"
                spellCheck={false}
                value={draft.chat.apiKeyEnvVar}
                onChange={(event) =>
                  setDraft({ ...draft, chat: { ...draft.chat, apiKeyEnvVar: event.target.value } })
                }
              />
              <FieldHelp>
                Leave blank to use the secure <strong>AI providers</strong> key under Credentials, then the
                provider's conventional variable. This stores only the variable name, never the key.
              </FieldHelp>
            </div>
          </>
        )}

        {usesOpenAIWire && (
          <SelectField
            label="API format"
            value={draft.chat.wireApi}
            onChange={(event) =>
              setDraft({ ...draft, chat: { ...draft.chat, wireApi: event.target.value } })
            }
          >
            {WIRE_API_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </SelectField>
        )}

        {provider === "azure-openai" && (
          <TextField
            label="Azure API version"
            placeholder="runtime default"
            autoComplete="off"
            spellCheck={false}
            value={draft.chat.azureApiVersion}
            onChange={(event) =>
              setDraft({ ...draft, chat: { ...draft.chat, azureApiVersion: event.target.value } })
            }
          />
        )}

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
