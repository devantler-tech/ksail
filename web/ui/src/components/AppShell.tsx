import { Dialog, DialogPanel, Transition, TransitionChild } from "@headlessui/react";
import {
  Activity,
  Boxes,
  KeyRound,
  Layers,
  LayoutDashboard,
  Lock,
  LogOut,
  Menu as MenuIcon,
  Moon,
  Search,
  Server,
  Settings,
  Sun,
} from "lucide-react";
import { Fragment, useState, type ReactNode } from "react";
import type { Theme } from "../hooks/useTheme.ts";
import type { User } from "../api.ts";
import { IconButton } from "./ui.tsx";

// View is the top-level SPA section. Routing is view-state (no router dependency), matching the
// existing single-page architecture.
export type View = "clusters" | "overview" | "resources" | "events" | "secrets" | "settings";

const VIEW_TITLES: Record<View, string> = {
  clusters: "Clusters",
  overview: "Overview",
  resources: "Resources",
  events: "Events",
  secrets: "Secrets",
  settings: "Settings",
};

// NavEntry describes one sidebar/drawer destination and whether the serving backend enables it.
type NavEntry = { view: View; label: string; icon: ReactNode; enabled: boolean };

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
      aria-current={active ? "page" : undefined}
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

// Brand is the logo lockup shown at the top of the sidebar and the mobile drawer.
function Brand() {
  return (
    <div className="flex h-14 items-center gap-2 border-b border-slate-200 px-5 dark:border-slate-800">
      <Boxes className="size-6 text-blue-600 dark:text-blue-500" aria-hidden />
      <span className="text-lg font-semibold tracking-tight text-slate-900 dark:text-white">KSail</span>
    </div>
  );
}

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
  surfaceLabel,
  onOpenCommandPalette,
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
  // surfaceLabel names the running surface (e.g. "Operator", "Local", "Desktop") for the sidebar
  // footer, so the same SPA brands itself correctly across the operator, `ksail ui`, and desktop.
  surfaceLabel: string;
  // onOpenCommandPalette opens the ⌘K command palette; the header renders a trigger when provided.
  onOpenCommandPalette?: () => void;
  headerActions?: ReactNode;
  children: ReactNode;
}) {
  // drawerOpen controls the mobile nav drawer (the sidebar is hidden below the md breakpoint).
  const [drawerOpen, setDrawerOpen] = useState(false);

  const navEntries: NavEntry[] = [
    { view: "clusters", label: "Clusters", icon: <Server className="size-4" aria-hidden />, enabled: true },
    {
      view: "overview",
      label: "Overview",
      icon: <LayoutDashboard className="size-4" aria-hidden />,
      enabled: workloadEnabled,
    },
    { view: "resources", label: "Resources", icon: <Layers className="size-4" aria-hidden />, enabled: workloadEnabled },
    { view: "events", label: "Events", icon: <Activity className="size-4" aria-hidden />, enabled: workloadEnabled },
    { view: "secrets", label: "Secrets", icon: <KeyRound className="size-4" aria-hidden />, enabled: secretsEnabled },
    { view: "settings", label: "Settings", icon: <Settings className="size-4" aria-hidden />, enabled: settingsEnabled },
  ];

  // navList renders the gated destinations. onPick wraps onNavigate so the mobile drawer can also
  // close itself on selection; the desktop sidebar passes onNavigate directly.
  const navList = (onPick: (next: View) => void) => (
    <nav className="flex-1 space-y-1 p-3">
      {navEntries
        .filter((entry) => entry.enabled)
        .map((entry) => (
          <NavItem
            key={entry.view}
            icon={entry.icon}
            label={entry.label}
            active={view === entry.view}
            onClick={() => onPick(entry.view)}
          />
        ))}
    </nav>
  );

  const footer = (
    <div className="border-t border-slate-200 p-3 text-xs text-slate-400 dark:border-slate-800">{surfaceLabel}</div>
  );

  return (
    <div className="flex h-full">
      {/* Persistent sidebar at md+; replaced by the drawer below md. */}
      <aside className="hidden w-60 shrink-0 flex-col border-r border-slate-200 bg-white md:flex dark:border-slate-800 dark:bg-slate-900">
        <Brand />
        {navList(onNavigate)}
        {footer}
      </aside>

      {/* Mobile nav drawer (md:hidden). Left-anchored, mirrors the SlideOver transition idiom. */}
      <Transition show={drawerOpen} as={Fragment}>
        <Dialog onClose={setDrawerOpen} className="relative z-50 md:hidden">
          <TransitionChild
            as={Fragment}
            enter="ease-out duration-200"
            enterFrom="opacity-0"
            enterTo="opacity-100"
            leave="ease-in duration-150"
            leaveFrom="opacity-100"
            leaveTo="opacity-0"
          >
            <div className="fixed inset-0 bg-slate-900/40 backdrop-blur-sm dark:bg-black/60" />
          </TransitionChild>
          <div className="fixed inset-0 flex">
            <TransitionChild
              as={Fragment}
              enter="transform ease-out duration-250"
              enterFrom="-translate-x-full"
              enterTo="translate-x-0"
              leave="transform ease-in duration-200"
              leaveFrom="translate-x-0"
              leaveTo="-translate-x-full"
            >
              <DialogPanel className="flex w-64 max-w-[80%] flex-col border-r border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
                <Brand />
                {navList((next) => {
                  onNavigate(next);
                  setDrawerOpen(false);
                })}
                {footer}
              </DialogPanel>
            </TransitionChild>
          </div>
        </Dialog>
      </Transition>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 shrink-0 items-center justify-between gap-3 border-b border-slate-200 bg-white/80 px-4 backdrop-blur md:px-6 dark:border-slate-800 dark:bg-slate-900/80">
          <div className="flex min-w-0 items-center gap-2 md:gap-3">
            <IconButton label="Open navigation" onClick={() => setDrawerOpen(true)} className="md:hidden">
              <MenuIcon className="size-5" />
            </IconButton>
            <h1 className="truncate text-sm font-semibold text-slate-900 md:text-base dark:text-white">
              {VIEW_TITLES[view]}
            </h1>
            {readOnly ? (
              <span className="inline-flex items-center gap-1 rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700 ring-1 ring-inset ring-amber-600/20 dark:bg-amber-500/10 dark:text-amber-400 dark:ring-amber-500/30">
                <Lock className="size-3" aria-hidden />
                <span className="hidden sm:inline">Read-only</span>
              </span>
            ) : null}
          </div>
          <div className="flex items-center gap-1.5">
            {onOpenCommandPalette ? (
              <button
                type="button"
                onClick={onOpenCommandPalette}
                className="inline-flex items-center gap-2 rounded-md px-2.5 py-1.5 text-sm text-slate-500 ring-1 ring-inset ring-slate-300 transition-colors hover:bg-slate-50 hover:text-slate-700 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-600 dark:text-slate-400 dark:ring-slate-700 dark:hover:bg-slate-800 dark:hover:text-slate-200"
                aria-label="Open command palette"
              >
                <Search className="size-4" aria-hidden />
                <span className="hidden lg:inline">Search</span>
                <kbd className="hidden rounded border border-slate-300 px-1 font-sans text-[10px] text-slate-400 sm:inline dark:border-slate-600">
                  ⌘K
                </kbd>
              </button>
            ) : null}
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
