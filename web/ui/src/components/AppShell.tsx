import { Dialog, DialogPanel, Transition, TransitionChild } from "@headlessui/react";
import { Lock, LogOut, Menu as MenuIcon, Moon, Puzzle, Search, Sun } from "lucide-react";
import { Fragment, useState, type ReactNode } from "react";
import type { Cluster, User } from "../api.ts";
import type { Theme } from "../hooks/useTheme.ts";
import { clusterViews, globalViews, viewTitle, type RegisteredView, type View, type ViewGates } from "../lib/views.tsx";
import { ClusterSwitcher } from "./ClusterSwitcher.tsx";
import { KSailMark } from "./Logo.tsx";
import { IconButton } from "./ui.tsx";

// PluginNavEntry is a plugin-contributed sidebar item (rendered in the Plugins nav zone). It carries
// the route the entry navigates to and a stable id/label; the icon is uniform (a puzzle piece) so the
// zone reads as plugin-provided.
export interface PluginNavEntry {
  id: string;
  label: string;
  route: string;
}

// isMacLike picks the platform-appropriate shortcut hint for the search button (the handler accepts
// both ⌘K and Ctrl+K regardless; this only affects the displayed kbd).
const isMacLike = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

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

function SectionLabel({ children }: { children: ReactNode }) {
  return (
    <div className="px-3 pb-1 pt-1 text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500">
      {children}
    </div>
  );
}

function Brand() {
  return (
    <div className="flex h-14 items-center gap-2 border-b border-slate-200 px-5 dark:border-slate-800">
      <KSailMark className="size-6" />
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
  clusters,
  activeClusterKey,
  onSelectCluster,
  settingsEnabled,
  workloadEnabled,
  secretsEnabled,
  pluginsEnabled,
  surfaceLabel,
  onOpenCommandPalette,
  headerActions,
  pluginEntries,
  activePluginRoute,
  onSelectPlugin,
  children,
}: {
  theme: Theme;
  onToggleTheme: () => void;
  user: User | null;
  onLogout?: () => void;
  readOnly: boolean;
  view: View;
  onNavigate: (view: View) => void;
  clusters: Cluster[];
  // activeClusterKey is the cluster the workspace is drilled into (null = none; cluster zone hidden).
  activeClusterKey: string | null;
  onSelectCluster: (key: string) => void;
  settingsEnabled: boolean;
  workloadEnabled: boolean;
  secretsEnabled: boolean;
  pluginsEnabled: boolean;
  surfaceLabel: string;
  onOpenCommandPalette?: () => void;
  headerActions?: ReactNode;
  // pluginEntries are plugin-contributed sidebar items; activePluginRoute marks which one (if any) is
  // open so its content renders in place of a view; onSelectPlugin navigates to a plugin route.
  pluginEntries?: PluginNavEntry[];
  activePluginRoute?: string | null;
  onSelectPlugin?: (route: string) => void;
  children: ReactNode;
}) {
  const [drawerOpen, setDrawerOpen] = useState(false);

  // gates feed the registry's per-view availability predicates. AppShell only ever shows the cluster
  // zone when a cluster is active (see navContent), so the cluster-scope gate is always satisfied here.
  const gates: ViewGates = {
    activeCluster: true,
    workloadEnabled,
    secretsEnabled,
    settingsEnabled,
    pluginsEnabled,
  };

  // headerTitle shows the active plugin route's label when one is open, else the current view's title.
  const activePluginLabel = activePluginRoute
    ? pluginEntries?.find((entry) => entry.route === activePluginRoute)?.label
    : undefined;
  const headerTitle = activePluginLabel ?? viewTitle(view);

  // renderNav renders the enabled views from one registry partition (cluster or global), in registry
  // order. Overview is always enabled for an active cluster (its spec/status/conditions come from the
  // cluster object); Resources/Events/Secrets/Settings carry their own capability gates.
  const renderNav = (entries: readonly RegisteredView[], onPick: (next: View) => void) =>
    entries
      .filter((entry) => entry.enabled(gates))
      .map((entry) => {
        const Icon = entry.icon;

        return (
          <NavItem
            key={entry.id}
            icon={<Icon className="size-4" aria-hidden />}
            label={entry.title}
            active={view === entry.id}
            onClick={() => onPick(entry.id)}
          />
        );
      });

  // navContent renders the two zones — the cluster workspace (switcher + scoped nav, only when a
  // cluster is active) above the always-present global zone. onPick lets the drawer close on navigate.
  const navContent = (onPick: (next: View) => void, onPickPlugin: (route: string) => void) => (
    <nav className="flex flex-1 flex-col gap-1 overflow-y-auto overscroll-contain p-3">
      {activeClusterKey ? (
        <>
          <div className="pb-1">
            <ClusterSwitcher clusters={clusters} activeKey={activeClusterKey} onSelect={onSelectCluster} />
          </div>
          {renderNav(clusterViews, onPick)}
          <div className="my-2 border-t border-slate-200 dark:border-slate-800" />
          <SectionLabel>Manage</SectionLabel>
        </>
      ) : null}
      {renderNav(globalViews, onPick)}
      {pluginEntries && pluginEntries.length > 0 ? (
        <>
          <div className="my-2 border-t border-slate-200 dark:border-slate-800" />
          <SectionLabel>Plugins</SectionLabel>
          {pluginEntries.map((entry) => (
            <NavItem
              key={entry.id}
              icon={<Puzzle className="size-4" aria-hidden />}
              label={entry.label}
              active={activePluginRoute === entry.route}
              onClick={() => onPickPlugin(entry.route)}
            />
          ))}
        </>
      ) : null}
    </nav>
  );

  const footer = (
    <div className="border-t border-slate-200 p-3 text-xs text-slate-400 dark:border-slate-800">{surfaceLabel}</div>
  );

  return (
    <div className="flex h-full">
      {/* Keyboard users can jump straight past the chrome to the active view's content. */}
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:left-4 focus:top-4 focus:z-[80] focus:rounded-md focus:bg-blue-600 focus:px-3 focus:py-2 focus:text-sm focus:font-medium focus:text-white"
      >
        Skip to content
      </a>
      {/* Persistent sidebar at md+; replaced by the drawer below md. */}
      <aside className="hidden w-64 shrink-0 flex-col border-r border-slate-200 bg-white md:flex dark:border-slate-800 dark:bg-slate-900">
        <Brand />
        {navContent(onNavigate, (route) => onSelectPlugin?.(route))}
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
              <DialogPanel className="flex w-72 max-w-[85%] flex-col border-r border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
                <Brand />
                {navContent(
                  (next) => {
                    onNavigate(next);
                    setDrawerOpen(false);
                  },
                  (route) => {
                    onSelectPlugin?.(route);
                    setDrawerOpen(false);
                  },
                )}
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
              {headerTitle}
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
                  {isMacLike ? "⌘K" : "Ctrl K"}
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

        <main id="main-content" className="flex-1 overflow-y-auto p-4 md:p-6">{children}</main>
      </div>
    </div>
  );
}
