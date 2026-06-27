import { useTheme } from "../../hooks/useTheme.ts";
import { SegmentedControl } from "../ui.tsx";
import { LabeledControl, SettingsSection } from "./SettingsSection.tsx";

const THEME_OPTIONS = [
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
  { value: "system", label: "System" },
] as const;

// AppearanceSettings is the Settings page's "Appearance" category. It delegates entirely to useTheme
// (the single theme authority that owns the `dark` class and persistence); it does not keep its own
// theme state. The header toggle and this control stay in sync because both read the same hook.
export function AppearanceSettings() {
  const { mode, setMode } = useTheme();

  return (
    <SettingsSection title="Appearance" description="Customize how the interface looks.">
      <LabeledControl label="Theme" help="“System” follows your operating system's light/dark setting.">
        <SegmentedControl options={THEME_OPTIONS} value={mode} onChange={setMode} />
      </LabeledControl>
    </SettingsSection>
  );
}
