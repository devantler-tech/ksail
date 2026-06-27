import { NavButton } from "../ui.tsx";
import type { RegisteredSettingsCategory, SettingsCategoryId } from "./catalog.ts";

// SettingsNav is the category rail for the Settings page: a vertical list on md+ screens, a
// horizontally-scrolling row on narrow ones. Reuses the shared NavButton for the active/idle state.
export function SettingsNav({
  categories,
  active,
  onSelect,
}: {
  categories: readonly RegisteredSettingsCategory[];
  active: SettingsCategoryId;
  onSelect: (id: SettingsCategoryId) => void;
}) {
  return (
    <nav
      aria-label="Settings categories"
      className="flex gap-1 overflow-x-auto pb-1 md:flex-col md:overflow-visible md:pb-0"
    >
      {categories.map((category) => {
        const Icon = category.icon;

        return (
          <NavButton
            key={category.id}
            icon={<Icon className="size-4 shrink-0" aria-hidden />}
            label={category.title}
            active={category.id === active}
            onClick={() => onSelect(category.id)}
            className="w-auto shrink-0 md:w-full md:shrink"
          />
        );
      })}
    </nav>
  );
}
