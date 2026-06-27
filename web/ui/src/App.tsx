import { Moon, Plus, RotateCw, Server, Sun } from "lucide-react";
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ApiError,
  createCluster,
  deleteCluster,
  errorMessage,
  fullCapabilities,
  getConfig,
  getMeta,
  listClusters,
  logout,
  updateCluster,
  type Capabilities,
  type Cluster,
  type ClusterMeta,
  type Config,
  type User,
} from "./api.ts";
import { MetaContext } from "./lib/meta.ts";
import { surfaceLabel } from "./lib/surface.ts";
import { clusterKey } from "./lib/k8s.ts";
import { CLUSTER_VIEW_IDS, isViewAvailable, VIEWS, type View, type ViewGates } from "./lib/views.tsx";
import { AppShell } from "./components/AppShell.tsx";
import { SettingsPage } from "./components/SettingsPage.tsx";
import { ResourcesView } from "./components/ResourcesView.tsx";
import { OverviewView } from "./components/OverviewView.tsx";
import { EventsView } from "./components/EventsView.tsx";
import { CommandPalette, type Command } from "./components/CommandPalette.tsx";
import { SecretsView } from "./components/SecretsView.tsx";
import { PluginsView } from "./components/PluginsView.tsx";
import { AIAssistant } from "./components/AIAssistant.tsx";
import { pluginNavigate } from "./lib/plugins/pluginNavigation.ts";
import { registry } from "./lib/plugins/registry.ts";
import { usePluginLoader, usePluginRegistry } from "./lib/plugins/usePlugins.ts";
import { setKubeWatchAvailable } from "./lib/plugins/watchStream.ts";
import { setWSMultiplexerAvailable } from "./lib/plugins/wsMultiplexer.ts";
import {
  ClusterFormDialog,
  specFromValues,
  type ClusterFormValues,
  type FormMode,
} from "./components/ClusterFormDialog.tsx";
import { ClustersTable } from "./components/ClustersTable.tsx";
import { ConfirmDialog } from "./components/ConfirmDialog.tsx";
import { LoginScreen } from "./components/LoginScreen.tsx";
import { EmptyState, ErrorBanner, TableSkeleton } from "./components/states.tsx";
import { Button } from "./components/ui.tsx";
import { useTheme } from "./hooks/useTheme.ts";
import { usePreferences } from "./hooks/usePreferences.tsx";
import { useClusterStream } from "./hooks/useClusterStream.ts";
import { useDeepLinks } from "./hooks/useDeepLinks.ts";
import { useDesktopCommands, type DesktopCommand } from "./hooks/useDesktopCommands.ts";
import type { DeepLinkTarget } from "./lib/deepLink.ts";
import { useToast } from "./components/Toast.tsx";

// DEFAULT_DISTRIBUTIONS is the create-form distribution list used when the backend does not advertise
// its supported set via config.distributions. The operator omits it (it only provisions VCluster
// in-cluster); the local `ksail open web` backend advertises everything it can create locally. The
// provider matrix and component options for whatever is selected still come from /api/v1/meta.
const DEFAULT_DISTRIBUTIONS = ["VCluster"];

// PluginRouterHost is lazy-loaded so react-router (and the plugin router machinery) only download when a
// plugin surface is opened — keeping KSail's main bundle free of react-router until a plugin needs it,
// matching the lazy-externals approach for MUI/Redux.
const PluginRouterHost = lazy(() =>
  import("./lib/plugins/PluginRouterHost.tsx").then((module) => ({ default: module.PluginRouterHost })),
);

// capability reads one capability flag from a (possibly null/partial) Config, defaulting to
// fullCapabilities — the value assumed for a backend that does not report capabilities (see api.ts).
// Centralizes the `config.capabilities?.x ?? fullCapabilities.x` projection so adding a capability is
// one Config/Capabilities field plus the one gate that consumes it, not a state slice + setter too.
function capability(config: Config | null, key: keyof Capabilities): boolean {
  return config?.capabilities?.[key] ?? fullCapabilities[key];
}

export function App() {
  const { theme, toggle } = useTheme();
  const { prefs } = usePreferences();
  const toast = useToast();

  // config is the single source for every capability/option projection below. null until the initial
  // /api/v1/config load resolves; the derived consts fall back to the no-config defaults until then.
  // The slices it replaced only ever changed together on (re)load, so one state is behavior-identical.
  const [config, setConfig] = useState<Config | null>(null);
  const [user, setUser] = useState<User | null>(null);
  const [needsLogin, setNeedsLogin] = useState(false);
  const [meta, setMeta] = useState<ClusterMeta | null>(null);
  const [view, setView] = useState<View>("clusters");
  // paletteOpen controls the ⌘K command palette overlay.
  const [paletteOpen, setPaletteOpen] = useState(false);
  // activePluginRoute is the plugin route currently open; its content replaces the view's. null = none.
  const [activePluginRoute, setActivePluginRoute] = useState<string | null>(null);

  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // activeClusterKey is the cluster the workspace is drilled into (the single source of "which
  // cluster" — there is no per-view selector). null = not in a cluster (the Clusters list is shown).
  const [activeClusterKey, setActiveClusterKey] = useState<string | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [formMode, setFormMode] = useState<FormMode>("create");
  const [formInitial, setFormInitial] = useState<Cluster | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Cluster | null>(null);
  // streamReady gates the live SSE subscription: open it only after the initial load succeeded
  // without losing the session (see init), so it never races the auth handshake.
  const [streamReady, setStreamReady] = useState(false);

  const mounted = useRef(true);

  const activeCluster = useMemo(
    () => clusters.find((cluster) => clusterKey(cluster) === activeClusterKey) ?? null,
    [clusters, activeClusterKey],
  );

  // Derived deployment consts: every capability gate and create-form option is a projection of the
  // single config state (no per-flag slice/setter). The fallbacks match the no-config defaults the
  // replaced useState slices used.
  const readOnly = config?.readOnly ?? false;
  // canUpdate reflects clusterUpdate: the local UI/desktop backend cannot update a cluster in place
  // (edit affordance hidden); the operator can. The rest mirror their capability flags 1:1.
  const canUpdate = capability(config, "clusterUpdate");
  const canBrowse = capability(config, "workloadRead");
  const canManage = capability(config, "workloadWrite");
  const canKubeconfig = capability(config, "kubeconfigDownload");
  const canApply = capability(config, "applyManifests");
  const canCipher = capability(config, "secretsCipher");
  const canLogs = capability(config, "workloadLogs");
  const canExecCap = capability(config, "workloadExec");
  // canInstallComponents gates the create form's component selectors: the operator installs the
  // declared components, the local backend does not yet (so the form hides them rather than offering
  // options that backend silently drops).
  const canInstallComponents = capability(config, "componentsInstall");
  const distributions = config?.distributions ?? DEFAULT_DISTRIBUTIONS;
  // providerStatus gates the create form's provider options. null = backend does not gate (operator).
  const providerStatus = config?.providers ?? null;
  // canPlugins gates the Plugins view and plugin loading on the backend serving UI plugins.
  const canPlugins = capability(config, "plugins");
  // canAIChat gates the Assistant view on the backend's AI chat capability (e.g. Copilot configured).
  const canAIChat = capability(config, "aiChat");
  // canKubeWatch reports whether the backend streams apiserver watches; the plugin K8s data layer reads
  // it through a module flag (set below) to keep its lists live via SSE instead of polling.
  const canKubeWatch = capability(config, "kubeWatch");
  // canWSMultiplexer reports whether the backend serves the Headlamp WebSocket multiplexer; when set, the
  // plugin K8s data layer prefers it (one socket) over the per-list SSE watch.
  const canWSMultiplexer = capability(config, "wsMultiplexer");
  const mode = config?.mode;

  // Mirror the kubeWatch / wsMultiplexer capabilities into the plugin watch modules so useAsyncList (which
  // has no access to the config state) opens a live watch only against a backend that serves it — the
  // multiplexer when available, else the SSE watch, else polling. Re-runs whenever config changes (e.g.
  // after a settings reload).
  useEffect(() => {
    setKubeWatchAvailable(canKubeWatch);
    setWSMultiplexerAvailable(canWSMultiplexer);
  }, [canKubeWatch, canWSMultiplexer]);

  // getCluster resolves the active cluster's namespace/name for plugins' Kubernetes data shim (read
  // live by the shim on each fetch), or null when no cluster is drilled into.
  const getCluster = useCallback(
    () =>
      activeCluster
        ? { namespace: activeCluster.metadata.namespace ?? "default", name: activeCluster.metadata.name }
        : null,
    [activeCluster],
  );
  // Subscribe to extension-registry changes so the sidebar reflects plugin-registered entries, and load
  // installed plugins once the backend advertises the capability.
  usePluginRegistry();
  const pluginLoader = usePluginLoader(canPlugins, getCluster);
  // The plugin sidebar is a parent→children tree (Headlamp plugins like Flux register a group + nested
  // entries); getSidebarTree applies any registered sidebar filters and nests children under their parent.
  const pluginEntries = registry.getSidebarTree();

  // enterCluster drills into a cluster's workspace, landing on its Overview. Used by the Clusters list,
  // deep links, and the command palette.
  const enterCluster = useCallback((key: string) => {
    setActivePluginRoute(null);
    setActiveClusterKey(key);
    setView("overview");
  }, []);

  // selectCluster switches the active cluster from the sidebar switcher, keeping the current
  // cluster-scoped view (so switching while on Resources stays on Resources); otherwise lands on
  // Overview.
  const selectCluster = useCallback((key: string) => {
    setActivePluginRoute(null);
    setActiveClusterKey(key);
    setView((current) => (CLUSTER_VIEW_IDS.includes(current) ? current : "overview"));
  }, []);

  // navigateView switches the top-level view, clearing any open plugin route so the view's content
  // shows (a plugin route otherwise renders in place of the view).
  const navigateView = useCallback((next: View) => {
    setActivePluginRoute(null);
    setView(next);
  }, []);

  // selectPlugin opens a plugin route; its content replaces the current view until a view is chosen.
  // Marking the surface active mounts PluginRouterHost (seeding this route as its initial path); when the
  // router is already mounted, pluginNavigate moves it there. The persistent router survives in-plugin
  // navigation; choosing a KSail view (setActivePluginRoute(null)) leaves the surface.
  const selectPlugin = useCallback((route: string) => {
    setActivePluginRoute(route);
    pluginNavigate(route);
  }, []);

  // Clear the cluster context when the active cluster disappears from the live list (e.g. deleted),
  // and bounce cluster-scoped views back to the Clusters list when no cluster is active.
  useEffect(() => {
    if (activeClusterKey && !clusters.some((cluster) => clusterKey(cluster) === activeClusterKey)) {
      setActiveClusterKey(null);
    }
  }, [clusters, activeClusterKey]);

  useEffect(() => {
    if (!activeClusterKey && CLUSTER_VIEW_IDS.includes(view)) {
      setView("clusters");
    }
  }, [activeClusterKey, view]);

  // refresh returns false when the session was lost (HTTP 401), so callers (e.g. init) can avoid
  // starting/continuing the poll while the login screen is shown.
  const refresh = useCallback(async (silent = false): Promise<boolean> => {
    if (!silent) {
      setRefreshing(true);
    }

    try {
      const list = await listClusters();
      if (!mounted.current) {
        return true;
      }
      setClusters(list.items ?? []);
      setError(null);
    } catch (err) {
      if (!mounted.current) {
        return true;
      }
      if (err instanceof ApiError && err.status === 401) {
        // Session lost: stop the live stream and show the login screen. Disabling the stream tears
        // down the EventSource (see useClusterStream) so it stops retrying against a 401.
        setStreamReady(false);
        setNeedsLogin(true);
        return false;
      }
      setError(errorMessage(err));
    } finally {
      if (mounted.current && !silent) {
        setRefreshing(false);
      }
    }

    return true;
  }, []);

  // reloadConfig re-fetches deployment config after a change that can affect it (e.g. saving
  // credential settings flips provider availability), so the create form's gating (all derived from
  // the single config state) stays current without a full page reload.
  const reloadConfig = useCallback(async () => {
    try {
      const next = await getConfig();
      if (!mounted.current) {
        return;
      }
      setConfig(next);
    } catch {
      // Non-fatal: keep the current config if the refresh fails.
    }
  }, []);

  // Navigate in response to a ksail:// deep link from the desktop shell (no-op in the browser). A
  // cluster target drills into that cluster (active + Overview); otherwise it switches the view.
  const navigateDeepLink = useCallback(
    (target: DeepLinkTarget) => {
      if (target.clusterKey) {
        enterCluster(target.clusterKey);

        return;
      }
      if (target.view) {
        setView(target.view);
      }
    },
    [enterCluster],
  );
  useDeepLinks(navigateDeepLink);

  // ⌘K / Ctrl+K toggles the command palette from anywhere in the app.
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setPaletteOpen((open) => !open);
      }
    }

    window.addEventListener("keydown", onKeyDown);

    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  // Native desktop menu commands (no-op in the browser). Mirrors the command palette's actions so the
  // menu and ⌘K stay in sync. The setters, refresh, and toggle are all stable.
  const handleDesktopCommand = useCallback(
    (command: DesktopCommand) => {
      if (command === "refresh") {
        void refresh();
      } else if (command === "toggle-theme") {
        toggle();
      } else if (command === "new-cluster" && !readOnly) {
        setFormMode("create");
        setFormInitial(null);
        setFormOpen(true);
      }
    },
    [refresh, toggle, readOnly],
  );
  useDesktopCommands(handleDesktopCommand);

  useEffect(() => {
    mounted.current = true;

    async function init() {
      let loaded;
      try {
        loaded = await getConfig();
      } catch (err) {
        if (mounted.current) {
          setError(errorMessage(err));
          setLoading(false);
        }
        return;
      }

      if (!mounted.current) {
        return;
      }

      setConfig(loaded);
      setUser(loaded.user ?? null);

      if (loaded.authEnabled && !loaded.user) {
        setNeedsLogin(true);
        setLoading(false);
        return;
      }

      try {
        const clusterMeta = await getMeta();
        if (mounted.current) {
          setMeta(clusterMeta);
        }
      } catch (err) {
        if (mounted.current) {
          setError(errorMessage(err));
          setLoading(false);
        }
        return;
      }

      const authOK = await refresh(true);
      if (mounted.current) {
        setLoading(false);
        // Live updates flow over the SSE stream (see useClusterStream). Enable it only if the initial
        // load did not lose the session; otherwise the login screen is shown and a stream would just
        // reconnect against a 401.
        if (authOK) {
          setStreamReady(true);
        }
      }
    }

    void init();

    return () => {
      mounted.current = false;
    };
  }, [refresh]);

  // Live cluster updates over SSE, replacing client-side polling. On a stream error we re-sync via
  // refresh(), which also detects a lost session (401) and surfaces the login screen.
  useClusterStream({
    enabled: streamReady && !needsLogin,
    onClusters: (items) => {
      if (!mounted.current) {
        return;
      }
      setClusters(items);
      setError(null);
    },
    onError: () => {
      void refresh(true);
    },
  });

  function openCreate() {
    setFormMode("create");
    setFormInitial(null);
    setFormOpen(true);
  }

  function openEdit(cluster: Cluster) {
    setFormMode("edit");
    setFormInitial(cluster);
    setFormOpen(true);
  }

  const handleSubmit = useCallback(
    async (values: ClusterFormValues) => {
      const spec = specFromValues(values);
      try {
        if (formMode === "edit" && formInitial) {
          // Preserve spec fields the form does not manage (provider options, workload, chat, OIDC…).
          const namespace = formInitial.metadata.namespace ?? "default";
          await updateCluster(namespace, formInitial.metadata.name, {
            metadata: { name: formInitial.metadata.name, namespace },
            spec: { ...formInitial.spec, cluster: { ...formInitial.spec?.cluster, ...spec } },
          });
          toast.success(`Cluster "${formInitial.metadata.name}" updated`);
        } else {
          const name = values.name.trim();
          await createCluster({
            metadata: { name, namespace: values.namespace.trim() || "default" },
            spec: { cluster: spec },
          });
          toast.success(`Cluster "${name}" created`);
        }
        await refresh(true);
      } catch (err) {
        toast.error(errorMessage(err));
        throw err;
      }
    },
    [formMode, formInitial, refresh, toast],
  );

  // handleSubmitRaw creates a cluster from a YAML-authored Cluster (the create dialog's YAML mode),
  // preserving every field the YAML carries (lossless full-spec) rather than projecting through the
  // form fields.
  const handleSubmitRaw = useCallback(
    async (cluster: Cluster) => {
      try {
        await createCluster(cluster);
        toast.success(`Cluster "${cluster.metadata.name}" created`);
        await refresh(true);
      } catch (err) {
        toast.error(errorMessage(err));
        throw err;
      }
    },
    [refresh, toast],
  );

  // performDelete deletes a specific cluster and refreshes the list. It rethrows so the confirm
  // dialog keeps its open/spinner state on failure; the immediate (unconfirmed) path swallows it.
  const performDelete = useCallback(
    async (target: Cluster) => {
      const name = target.metadata.name;
      const namespace = target.metadata.namespace ?? "default";
      try {
        await deleteCluster(namespace, name);
        toast.success(`Deleting cluster "${name}"`);
        await refresh(true);
      } catch (err) {
        toast.error(errorMessage(err));
        throw err;
      }
    },
    [refresh, toast],
  );

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) {
      return;
    }
    await performDelete(deleteTarget);
  }, [deleteTarget, performDelete]);

  // requestDeleteCluster is the per-row / overview delete trigger: it opens the confirm dialog, or —
  // when the confirm-destructive preference is off — deletes the cluster immediately.
  const requestDeleteCluster = useCallback(
    (cluster: Cluster) => {
      if (prefs.confirmDestructive) {
        setDeleteTarget(cluster);

        return;
      }
      void performDelete(cluster).catch(() => undefined);
    },
    [prefs.confirmDestructive, performDelete],
  );

  if (needsLogin) {
    return <LoginScreen />;
  }

  // canEdit gates the edit affordance: editing requires both a writable UI and a backend that can
  // apply spec changes in place. Delete stays gated on readOnly alone (the local backend supports it).
  const canEdit = !readOnly && canUpdate;

  // Header actions (Refresh / New cluster) belong to the Clusters view only.
  const headerActions =
    view === "clusters" ? (
      <>
        <Button variant="secondary" size="sm" onClick={() => void refresh()} loading={refreshing}>
          {refreshing ? null : <RotateCw className="size-4" aria-hidden />}
          Refresh
        </Button>
        {!readOnly ? (
          <Button size="sm" onClick={openCreate}>
            <Plus className="size-4" aria-hidden />
            New cluster
          </Button>
        ) : null}
      </>
    ) : null;

  // viewGates feeds the registry's availability predicates. The palette spans both scopes, so the
  // cluster-scope gate (activeCluster) decides whether the cluster-scoped views appear.
  const viewGates: ViewGates = {
    activeCluster: activeClusterKey !== null,
    workloadEnabled: canBrowse,
    secretsEnabled: canCipher,
    pluginsEnabled: canPlugins,
  };

  // navCommands derives the palette's "Go to <view>" entries from the same view registry the sidebar
  // nav uses, so labels, icons, and gates can never drift between the two. The cluster-scoped views
  // require an active cluster (isViewAvailable enforces it); choosing a cluster entry below drills in.
  const navCommands: Command[] = VIEWS.filter((entry) => isViewAvailable(entry, viewGates)).map((entry) => {
    const Icon = entry.icon;

    return {
      id: `nav-${entry.id}`,
      label: `Go to ${entry.title}`,
      hint: "Navigate",
      icon: <Icon className="size-4" aria-hidden />,
      run: () => navigateView(entry.id),
    };
  });

  // Command palette entries: navigation (from the registry), common actions, and jump-to-cluster.
  // Built each render (cheap) from the live clusters list and the capability gates, so it stays in
  // sync with the nav. Choosing a cluster entry drills into that cluster.
  const commands: Command[] = [
    ...navCommands,
    {
      id: "action-refresh",
      label: "Refresh clusters",
      hint: "Action",
      icon: <RotateCw className="size-4" aria-hidden />,
      run: () => {
        navigateView("clusters");
        void refresh();
      },
    },
    ...(!readOnly
      ? [{ id: "action-new", label: "New cluster", hint: "Action", icon: <Plus className="size-4" aria-hidden />, run: openCreate }]
      : []),
    {
      id: "action-theme",
      label: theme === "dark" ? "Switch to light theme" : "Switch to dark theme",
      hint: "Action",
      icon: theme === "dark" ? <Sun className="size-4" aria-hidden /> : <Moon className="size-4" aria-hidden />,
      run: toggle,
    },
    ...clusters.map((cluster) => {
      const key = clusterKey(cluster);

      return {
        id: `cluster-${key}`,
        label: cluster.metadata.name,
        hint: "Open cluster",
        icon: <Server className="size-4" aria-hidden />,
        keywords: key,
        run: () => enterCluster(key),
      };
    }),
  ];

  return (
    <MetaContext.Provider value={meta}>
      <AppShell
        theme={theme}
        onToggleTheme={toggle}
        user={user}
        onLogout={() => void logout().finally(() => setNeedsLogin(true))}
        readOnly={readOnly}
        view={view}
        onNavigate={navigateView}
        clusters={clusters}
        activeClusterKey={activeClusterKey}
        onSelectCluster={selectCluster}
        workloadEnabled={canBrowse}
        secretsEnabled={canCipher}
        pluginsEnabled={canPlugins}
        surfaceLabel={surfaceLabel(mode)}
        onOpenCommandPalette={() => setPaletteOpen(true)}
        headerActions={headerActions}
        pluginEntries={pluginEntries}
        activePluginRoute={activePluginRoute}
        onSelectPlugin={selectPlugin}
      >
        {activePluginRoute ? (
          <Suspense fallback={null}>
            <PluginRouterHost initialPath={activePluginRoute} clusterName={activeCluster?.metadata.name ?? null} />
          </Suspense>
        ) : view === "settings" ? (
          <SettingsPage config={config} onSaved={() => void reloadConfig()} onNavigate={navigateView} />
        ) : view === "plugins" ? (
          <PluginsView
            plugins={pluginLoader.plugins}
            loading={pluginLoader.loading}
            error={pluginLoader.error}
            onReload={pluginLoader.reload}
            canInstall={capability(config, "pluginInstall")}
            canBrowseCatalog={capability(config, "pluginCatalog")}
          />
        ) : view === "assistant" ? (
          <AIAssistant
            clusterName={activeCluster?.metadata.name ?? null}
            namespace={activeCluster?.metadata.namespace ?? null}
            available={canAIChat}
            allowWrite={capability(config, "aiChatWrite")}
          />
        ) : view === "secrets" ? (
          <SecretsView />
        ) : view === "overview" ? (
          <OverviewView
            cluster={activeCluster}
            canBrowse={canBrowse}
            canEdit={canEdit}
            canDelete={!readOnly}
            canDownloadKubeconfig={canKubeconfig}
            onEdit={openEdit}
            onDelete={requestDeleteCluster}
          />
        ) : view === "events" ? (
          <EventsView cluster={activeCluster} />
        ) : view === "resources" ? (
          <ResourcesView
            cluster={activeCluster}
            canWrite={!readOnly && canManage}
            canApply={!readOnly && canApply}
            canLogs={canLogs}
            canExec={!readOnly && canExecCap}
          />
        ) : (
          <div className="mx-auto max-w-6xl space-y-4">
            {error && clusters.length > 0 ? <ErrorBanner message={error} onRetry={() => void refresh()} /> : null}

            {loading ? (
              <TableSkeleton />
            ) : error && clusters.length === 0 ? (
              <ErrorBanner message={error} onRetry={() => void refresh()} />
            ) : clusters.length === 0 ? (
              <EmptyState
                title="No clusters yet"
                description={
                  readOnly
                    ? "Clusters are managed declaratively from Git (read-only mode)."
                    : "Create your first cluster to get started."
                }
                action={
                  !readOnly ? (
                    <Button onClick={openCreate}>
                      <Plus className="size-4" aria-hidden />
                      New cluster
                    </Button>
                  ) : undefined
                }
              />
            ) : (
              <ClustersTable
                clusters={clusters}
                readOnly={readOnly}
                canEdit={canEdit}
                onSelect={(cluster) => enterCluster(clusterKey(cluster))}
                onEdit={openEdit}
                onDelete={requestDeleteCluster}
              />
            )}
          </div>
        )}

        {meta ? (
          <ClusterFormDialog
            open={formOpen}
            mode={formMode}
            initial={formInitial}
            distributions={distributions}
            providerStatus={providerStatus}
            componentsInstall={canInstallComponents}
            isOperator={mode === "operator"}
            onSubmit={handleSubmit}
            onSubmitRaw={handleSubmitRaw}
            onClose={() => setFormOpen(false)}
          />
        ) : null}

        <ConfirmDialog
          open={deleteTarget !== null}
          title="Delete cluster"
          description={
            deleteTarget ? (
              <>
                This permanently deletes{" "}
                <span className="font-medium text-slate-700 dark:text-slate-200">{deleteTarget.metadata.name}</span> and
                its underlying resources. This action cannot be undone.
              </>
            ) : (
              ""
            )
          }
          confirmLabel="Delete"
          onConfirm={handleDelete}
          onClose={() => setDeleteTarget(null)}
        />

        <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} commands={commands} />
      </AppShell>
    </MetaContext.Provider>
  );
}
