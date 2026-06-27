import { Puzzle } from "lucide-react";
import {
  usePreferences,
  type DateStyle,
  type TimeZonePref,
} from "../../hooks/usePreferences.tsx";
import { Button, SegmentedControl, SelectField, TextField, Toggle } from "../ui.tsx";
import { FieldHelp, LabeledControl, SettingsSection } from "./SettingsSection.tsx";

const TIME_FORMAT_OPTIONS = [
  { value: "relative", label: "Relative" },
  { value: "absolute", label: "Absolute" },
] as const;

const ROWS_PER_PAGE_OPTIONS = [25, 50, 100, 0] as const;

// GeneralSettings is the Settings page's "General" category: client-side UI preferences (table
// density, timestamp display, default namespace, confirmations) backed by the preferences store.
// They apply live on change — there is no save step. onOpenPlugins cross-links to the Plugins view
// (plugins are managed there, not duplicated here).
export function GeneralSettings({ onOpenPlugins }: { onOpenPlugins?: () => void }) {
  const { prefs, setPreference } = usePreferences();

  return (
    <SettingsSection
      title="General"
      description="Preferences applied across the interface. Changes save automatically."
    >
      <div className="space-y-5">
        <div>
          <SelectField
            label="Rows per page"
            value={String(prefs.rowsPerPage)}
            onChange={(event) => setPreference("rowsPerPage", Number(event.target.value))}
          >
            {ROWS_PER_PAGE_OPTIONS.map((rows) => (
              <option key={rows} value={rows}>
                {rows === 0 ? "Show all" : rows}
              </option>
            ))}
          </SelectField>
          <FieldHelp>How many rows resource, event, and cluster tables show per page.</FieldHelp>
        </div>

        <LabeledControl
          label="Timestamps"
          help="Show ages as a relative duration (5m, 3h, 2d) or as a full date-time."
        >
          <SegmentedControl
            options={TIME_FORMAT_OPTIONS}
            value={prefs.timeFormat}
            onChange={(value) => setPreference("timeFormat", value)}
          />
        </LabeledControl>

        <div>
          <SelectField
            label="Date style"
            value={prefs.dateStyle}
            onChange={(event) => setPreference("dateStyle", event.target.value as DateStyle)}
          >
            <option value="locale">Locale</option>
            <option value="iso">ISO (YYYY-MM-DD)</option>
          </SelectField>
          <FieldHelp>Applies to absolute date-times (including the relative-age tooltips).</FieldHelp>
        </div>

        <SelectField
          label="Time zone"
          value={prefs.timeZone}
          onChange={(event) => setPreference("timeZone", event.target.value as TimeZonePref)}
        >
          <option value="local">Local time</option>
          <option value="utc">UTC</option>
        </SelectField>

        <div>
          <TextField
            label="Default namespace"
            placeholder="all namespaces"
            spellCheck={false}
            value={prefs.defaultNamespace}
            onChange={(event) => setPreference("defaultNamespace", event.target.value)}
          />
          <FieldHelp>Seeds the namespace filter when browsing resources. Leave blank for all.</FieldHelp>
        </div>

        <div className="border-t border-slate-100 pt-4 dark:border-slate-800">
          <Toggle
            label="Confirm destructive actions"
            description="Ask before deleting clusters and resources."
            checked={prefs.confirmDestructive}
            onChange={(checked) => setPreference("confirmDestructive", checked)}
          />
        </div>

        {onOpenPlugins ? (
          <div className="flex items-center justify-between gap-4 border-t border-slate-100 pt-4 dark:border-slate-800">
            <div>
              <span className="block text-sm font-medium text-slate-700 dark:text-slate-200">Plugins</span>
              <span className="text-xs text-slate-500 dark:text-slate-400">
                Install and manage UI plugins in the Plugins section.
              </span>
            </div>
            <Button variant="secondary" size="sm" onClick={onOpenPlugins}>
              <Puzzle className="size-4" aria-hidden />
              Manage plugins
            </Button>
          </div>
        ) : null}
      </div>
    </SettingsSection>
  );
}
