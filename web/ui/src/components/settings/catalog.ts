import { Bot, Info, KeyRound, Palette, SlidersHorizontal, type LucideIcon } from "lucide-react";

// The settings catalog is the single source of truth for the Settings page's categories: their
// title, rail icon, blurb, and availability gate. SettingsNav (the rail) and SettingsPage (the
// detail switch) both derive from it, so adding a category is one entry here. Mirrors the views.tsx
// registry idiom.

// SettingsCategoryId is the set of category ids, derived from the catalog below (see
// RegisteredSettingsCategory / the SettingsPage switch) so the two can never drift.
export type SettingsCategoryId =
  | "general"
  | "appearance"
  | "credentials"
  | "editorChat"
  | "about";

// SettingsCategoryGates carries the runtime flags a category's availability predicate reads.
export interface SettingsCategoryGates {
  // settingsEnabled is the backend's credential-settings capability — served by the local
  // `ksail open web` / desktop backend, not by the in-cluster operator. Backend-backed categories
  // (Credentials) gate on it; the pure client-side categories (General, Appearance, About) are
  // always available, so the Settings page is useful on every backend.
  settingsEnabled: boolean;
}

export interface SettingsCategory {
  id: SettingsCategoryId;
  title: string;
  description: string;
  icon: LucideIcon;
  enabled: (gates: SettingsCategoryGates) => boolean;
}

export const SETTINGS_CATEGORIES = [
  {
    id: "general",
    title: "General",
    description: "Table density, timestamp display, default namespace, and confirmations.",
    icon: SlidersHorizontal,
    enabled: () => true,
  },
  {
    id: "appearance",
    title: "Appearance",
    description: "Theme and visual preferences.",
    icon: Palette,
    enabled: () => true,
  },
  {
    id: "credentials",
    title: "Credentials",
    description: "Cloud provider credentials KSail uses to reach each provider.",
    icon: KeyRound,
    enabled: (gates) => gates.settingsEnabled,
  },
  {
    id: "editorChat",
    title: "Editor & AI",
    description: "Default editor command and AI assistant model.",
    icon: Bot,
    enabled: (gates) => gates.settingsEnabled,
  },
  {
    id: "about",
    title: "About",
    description: "Version and deployment information.",
    icon: Info,
    enabled: () => true,
  },
] as const satisfies readonly SettingsCategory[];

// RegisteredSettingsCategory is one concrete catalog entry (id narrowed to the literal union).
export type RegisteredSettingsCategory = (typeof SETTINGS_CATEGORIES)[number];

// availableCategories returns the catalog entries enabled for the current gates, in catalog order.
export function availableCategories(gates: SettingsCategoryGates): RegisteredSettingsCategory[] {
  return SETTINGS_CATEGORIES.filter((category) => category.enabled(gates));
}
