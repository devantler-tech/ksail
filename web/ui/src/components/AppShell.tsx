import { Boxes, KeyRound, Layers, Lock, LogOut, Moon, Server, Settings, Sun } from "lucide-react";
import type { ReactNode } from "react";
import type { Theme } from "../hooks/useTheme.ts";
import type { User } from "../api.ts";
import { IconButton } from "./ui.tsx";

// View is the top-level SPA section. Routing is view-state (no router dependency), matching the
// existing single-page architecture.
export type View = "clusters" | "resources" | "secrets" | "settings";

function NavItem({
  icon,
  label,
  active,
  onClick,
}: {
  icon: ReactNode;
  label: string;
  active?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={
        active
          ? "flex w-full items-center gap-2.5 rounded-md bg-blue-50 px-3 py-2 text-sm font-medium text-blue-700 dark:bg-blue-500/10 dark:text-blue-400"
          : "flex w-full items-center gap-2.5 rounded-md px-3 py-2 text-sm font-medium text-slate-600 hover:bg-slate-50 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800/60 dark:hover:text-white"
      }
    >
      {icon}
      {label}
    </button>
  );
}

const VIEW_TITLES: Record<View, string> = {
  clusters: "Clusters",
  resources: "Resources",
  secrets: "Secrets",
  settings: "Settings",
};

export function AppShell({
  theme,
  onToggleTheme,
  user,
  onLogout,
  readOnly,
  view,
  onNavigate,
  settingsEnabled,
  workloadEnabled,
  secretsEnabled,
  headerActions,
  children,
}: {
  theme: Theme;
  onToggleTheme: () => void;
  user: User | null;
  onLogout?: () => void;
  readOnly: boolean;
  view: View;
  onNavigate: (view: View) => void;
  settingsEnabled: boolean;
  workloadEnabled: boolean;
  secretsEnabled: boolean;
  headerActions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="flex h-full">
      <aside className="hidden w-60 shrink-0 flex-col border-r border-slate-200 bg-white md:flex dark:border-slate-800 dark:bg-slate-900">
        <div className="flex h-14 items-center gap-2 border-b border-slate-200 px-5 dark:border-slate-800">
          <Boxes className="size-6 text-blue-600 dark:text-blue-500" aria-hidden />
          <span className="text-lg font-semibold tracking-tight text-slate-900 dark:text-white">
            KSail
          </span>
        </div>
        <nav className="flex-1 space-y-1 p-3">
          <NavItem
            icon={<Server className="size-4" aria-hidden />}
            label="Clusters"
            active={view === "clusters"}
            onClick={() => onNavigate("clusters")}
          />
          {workloadEnabled ? (
            <NavItem
              icon={<Layers className="size-4" aria-hidden />}
              label="Resources"
              active={view === "resources"}
              onClick={() => onNavigate("resources")}
            />
          ) : null}
          {secretsEnabled ? (
            <NavItem
              icon={<KeyRound className="size-4" aria-hidden />}
              label="Secrets"
              active={view === "secrets"}
              onClick={() => onNavigate("secrets")}
            />
          ) : null}
          {settingsEnabled ? (
            <NavItem
              icon={<Settings className="size-4" aria-hidden />}
              label="Settings"
              active={view === "settings"}
              onClick={() => onNavigate("settings")}
            />
          ) : null}
        </nav>
        <div className="border-t border-slate-200 p-3 text-xs text-slate-400 dark:border-slate-800">
          Kubernetes Operator
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 shrink-0 items-center justify-between gap-3 border-b border-slate-200 bg-white/80 px-4 backdrop-blur md:px-6 dark:border-slate-800 dark:bg-slate-900/80">
          <div className="flex items-center gap-3">
            <h1 className="text-sm font-semibold text-slate-900 md:text-base dark:text-white">
              {VIEW_TITLES[view]}
            </h1>
            {readOnly ? (
              <span className="inline-flex items-center gap-1 rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700 ring-1 ring-inset ring-amber-600/20 dark:bg-amber-500/10 dark:text-amber-400 dark:ring-amber-500/30">
                <Lock className="size-3" aria-hidden />
                Read-only
              </span>
            ) : null}
          </div>
          <div className="flex items-center gap-1.5">
            {headerActions}
            <IconButton
              label={theme === "dark" ? "Switch to light theme" : "Switch to dark theme"}
              onClick={onToggleTheme}
            >
              {theme === "dark" ? <Sun className="size-5" /> : <Moon className="size-5" />}
            </IconButton>
            {user ? (
              <div className="ml-1 flex items-center gap-2 border-l border-slate-200 pl-2 dark:border-slate-800">
                <span className="hidden max-w-[12rem] truncate text-sm text-slate-500 sm:block dark:text-slate-400">
                  {user.email ?? user.name ?? user.subject}
                </span>
                <IconButton label="Sign out" onClick={onLogout}>
                  <LogOut className="size-5" />
                </IconButton>
              </div>
            ) : null}
          </div>
        </header>

        <main className="flex-1 overflow-y-auto p-4 md:p-6">{children}</main>
      </div>
    </div>
  );
}
