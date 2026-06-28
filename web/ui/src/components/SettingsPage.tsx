import { useState } from "react";
import type { Config } from "../api.ts";
import type { View } from "../lib/views.tsx";
import { availableCategories, type SettingsCategoryId } from "./settings/catalog.ts";
import { SettingsNav } from "./settings/SettingsNav.tsx";
import { GeneralSettings } from "./settings/GeneralSettings.tsx";
import { AppearanceSettings } from "./settings/AppearanceSettings.tsx";
import { CredentialsSettings } from "./settings/CredentialsSettings.tsx";
import { EditorChatSettings } from "./settings/EditorChatSettings.tsx";
import { AboutSettings } from "./settings/AboutSettings.tsx";

// SettingsPage is the categorized settings shell: a category rail (master) beside the active
// category's panel (detail). Categories come from the catalog, gated by backend capability — the
// pure client-side ones (General/Appearance/About) show on every backend, while Credentials is
// gated on the local UI's settings endpoints. onSaved refreshes deployment config after a
// credential save; onNavigate cross-links out to other views (e.g. Plugins).
export function SettingsPage({
  config,
  onSaved,
  onNavigate,
}: {
  config: Config | null;
  onSaved?: () => void;
  onNavigate?: (view: View) => void;
}) {
  const settingsEnabled = config?.settingsEnabled ?? false;
  // Gate the Plugins cross-link on the same capability App uses to show the Plugins view, so Settings
  // can't navigate to a surface the backend doesn't serve.
  const canPlugins = config?.capabilities?.plugins ?? false;
  const categories = availableCategories({ settingsEnabled });
  const [active, setActive] = useState<SettingsCategoryId>("general");

  // Fall back to the first available category when the selected one is gated off (e.g. Credentials
  // on the operator backend), so the detail pane never renders empty.
  const current = categories.some((category) => category.id === active)
    ? active
    : (categories[0]?.id ?? "general");

  return (
    <div className="mx-auto max-w-5xl">
      <div className="grid grid-cols-1 gap-6 md:grid-cols-[12rem_1fr]">
        <div className="md:sticky md:top-0 md:self-start">
          <SettingsNav categories={categories} active={current} onSelect={setActive} />
        </div>
        <div className="min-w-0">
          {current === "general" ? (
            <GeneralSettings
              onOpenPlugins={onNavigate && canPlugins ? () => onNavigate("plugins") : undefined}
            />
          ) : current === "appearance" ? (
            <AppearanceSettings />
          ) : current === "credentials" ? (
            <CredentialsSettings onSaved={onSaved} />
          ) : current === "editorChat" ? (
            <EditorChatSettings />
          ) : (
            <AboutSettings config={config} />
          )}
        </div>
      </div>
    </div>
  );
}
