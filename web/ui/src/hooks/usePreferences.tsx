import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { formatAbsolute, relativeAge } from "../lib/format.ts";

// TimeFormat chooses how timestamps render across the app: "relative" is the compact "5m/3h/2d"
// age (kubectl-familiar, the default); "absolute" shows the full date-time.
export type TimeFormat = "relative" | "absolute";
// DateStyle controls the absolute rendering: "locale" uses the viewer's locale string, "iso" uses
// an ISO-ordered "YYYY-MM-DD HH:MM:SS".
export type DateStyle = "locale" | "iso";
// TimeZonePref renders absolute timestamps in the viewer's local zone or in UTC.
export type TimeZonePref = "local" | "utc";
// DetailFormat is the resource manifest rendering in the detail panel (kubectl-familiar YAML by
// default, or JSON).
export type DetailFormat = "yaml" | "json";

// Preferences are pure client-side UI settings — no secrets, no cluster identity. They persist to
// localStorage and apply live (no save step). Backend-persisted settings (credentials, editor,
// chat) live elsewhere; do not add anything sensitive here.
export interface Preferences {
  // rowsPerPage caps resource/event/cluster tables (0 = show all). Consumed once table pagination
  // lands; stored now so the General settings UI is complete.
  rowsPerPage: number;
  // timeFormat / dateStyle / timeZone drive every timestamp via useTimeFormatters().
  timeFormat: TimeFormat;
  dateStyle: DateStyle;
  timeZone: TimeZonePref;
  // defaultNamespace seeds the Resources namespace filter (empty = all namespaces).
  defaultNamespace: string;
  // confirmDestructive gates the confirm dialog on destructive actions (delete). false runs them
  // immediately.
  confirmDestructive: boolean;
  // detailFormat is the resource detail manifest rendering, persisted across sessions.
  detailFormat: DetailFormat;
}

export const DEFAULT_PREFERENCES: Preferences = {
  rowsPerPage: 50,
  timeFormat: "relative",
  dateStyle: "locale",
  timeZone: "local",
  defaultNamespace: "",
  confirmDestructive: true,
  detailFormat: "yaml",
};

const STORAGE_KEY = "ksail-preferences";

// readStored loads persisted preferences, merged over defaults so a newly-added field (or a
// partially-written blob) degrades gracefully. localStorage access can throw in privacy mode or
// sandboxed frames, and the value can be malformed — both are treated as "no stored preferences".
function readStored(): Preferences {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) {
      return { ...DEFAULT_PREFERENCES };
    }

    const parsed = JSON.parse(raw) as Partial<Preferences>;
    if (typeof parsed !== "object" || parsed === null) {
      return { ...DEFAULT_PREFERENCES };
    }

    return { ...DEFAULT_PREFERENCES, ...parsed };
  } catch {
    return { ...DEFAULT_PREFERENCES };
  }
}

function writeStored(prefs: Preferences): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(prefs));
  } catch {
    /* ignore: storage unavailable (preferences still apply for this session) */
  }
}

export interface PreferencesApi {
  prefs: Preferences;
  setPreference: <K extends keyof Preferences>(key: K, value: Preferences[K]) => void;
  reset: () => void;
}

const PreferencesContext = createContext<PreferencesApi | null>(null);

// PreferencesProvider holds the live preferences and persists every change. Mount it near the app
// root (alongside ToastProvider) so the whole tree can read/update preferences.
export function PreferencesProvider({ children }: { children: ReactNode }) {
  const [prefs, setPrefs] = useState<Preferences>(() => readStored());

  const setPreference = useCallback<PreferencesApi["setPreference"]>((key, value) => {
    setPrefs((current) => {
      const next = { ...current, [key]: value };
      writeStored(next);

      return next;
    });
  }, []);

  const reset = useCallback(() => {
    setPrefs({ ...DEFAULT_PREFERENCES });
    writeStored(DEFAULT_PREFERENCES);
  }, []);

  const api = useMemo<PreferencesApi>(() => ({ prefs, setPreference, reset }), [prefs, setPreference, reset]);

  return <PreferencesContext.Provider value={api}>{children}</PreferencesContext.Provider>;
}

export function usePreferences(): PreferencesApi {
  const context = useContext(PreferencesContext);
  if (!context) {
    throw new Error("usePreferences must be used within a PreferencesProvider");
  }

  return context;
}

// TimestampFormatters bundles the preference-aware primary formatter with an always-absolute one
// (for tooltips that complement a relative label).
export interface TimestampFormatters {
  // format renders per the timeFormat preference (relative age or absolute date-time).
  format: (iso?: string) => string;
  // formatAbsolute always renders the full date-time (honours dateStyle/timeZone), e.g. for a
  // tooltip alongside a relative label.
  formatAbsolute: (iso?: string) => string;
}

// useTimeFormatters returns timestamp formatters bound to the current time preferences. Call it at
// a component's top level and use the returned functions in render (including inside list maps).
export function useTimeFormatters(): TimestampFormatters {
  const { prefs } = usePreferences();

  return useMemo<TimestampFormatters>(() => {
    const absolute = (iso?: string) =>
      formatAbsolute(iso, { dateStyle: prefs.dateStyle, timeZone: prefs.timeZone });

    return {
      format: (iso?: string) => (prefs.timeFormat === "absolute" ? absolute(iso) : relativeAge(iso)),
      formatAbsolute: absolute,
    };
  }, [prefs.timeFormat, prefs.dateStyle, prefs.timeZone]);
}
