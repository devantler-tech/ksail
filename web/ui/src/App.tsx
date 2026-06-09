import {
  Activity,
  KeyRound,
  Layers,
  LayoutDashboard,
  Moon,
  Plus,
  RotateCw,
  Server,
  Settings,
  Sun,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  ApiError,
  createCluster,
  deleteCluster,
  fullCapabilities,
  getConfig,
  getMeta,
  listClusters,
  logout,
  updateCluster,
  type Cluster,
  type ClusterMeta,
  type Config,
  type ProviderInfo,
  type User,
} from "./api.ts";
import { MetaContext } from "./lib/meta.ts";
import { surfaceLabel } from "./lib/surface.ts";
import { AppShell, type View } from "./components/AppShell.tsx";
import { SettingsPage } from "./components/SettingsPage.tsx";
import { ResourcesView } from "./components/ResourcesView.tsx";
import { OverviewView } from "./components/OverviewView.tsx";
import { EventsView } from "./components/EventsView.tsx";
import { CommandPalette, type Command } from "./components/CommandPalette.tsx";
import { SecretsView } from "./components/SecretsView.tsx";
import { ClusterDetail } from "./components/ClusterDetail.tsx";
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
import { useClusterStream } from "./hooks/useClusterStream.ts";
import { useDeepLinks } from "./hooks/useDeepLinks.ts";
import { useDesktopCommands, type DesktopCommand } from "./hooks/useDesktopCommands.ts";
import type { DeepLinkTarget } from "./lib/deepLink.ts";
import { useToast } from "./components/Toast.tsx";

// DEFAULT_DISTRIBUTIONS is the create-form distribution list used when the backend does not advertise
// its supported set via config.distributions. The operator omits it (it only provisions VCluster
// in-cluster); the local `ksail ui` backend advertises everything it can create locally. The
// provider matrix and component options for whatever is selected still come from /api/v1/meta.
const DEFAULT_DISTRIBUTIONS = ["VCluster"];

function clusterKey(cluster: Cluster): string {
  return `${cluster.metadata.namespace ?? "default"}/${cluster.metadata.name}`;
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    return err.message;
  }

  return err instanceof Error ? err.message : String(err);
}

export function App() {
  const { theme, toggle } = useTheme();
  const toast = useToast();

  const [readOnly, setReadOnly] = useState(false);
  // canUpdate reflects the backend's clusterUpdate capability. The local UI/desktop backend cannot
  // update a cluster in place, so the edit affordance is hidden there; the operator can, so it shows.
  const [canUpdate, setCanUpdate] = useState(fullCapabilities.clusterUpdate);
  // canBrowse reflects the backend's workloadRead capability: whether the read-only resource browser
  // endpoints exist. The Resources view + nav item are shown only when true.
  const [canBrowse, setCanBrowse] = useState(fullCapabilities.workloadRead);
  // canManage reflects the backend's workloadWrite capability (scale/restart/delete). Combined with
  // !readOnly before the action UI is shown.
  const [canManage, setCanManage] = useState(fullCapabilities.workloadWrite);
  // canKubeconfig reflects the backend's kubeconfigDownload capability (the local backend).
  const [canKubeconfig, setCanKubeconfig] = useState(fullCapabilities.kubeconfigDownload);
  // canApply reflects the backend's applyManifests capability.
  const [canApply, setCanApply] = useState(fullCapabilities.applyManifests);
  // canCipher reflects the backend's secretsCipher capability (local SOPS).
  const [canCipher, setCanCipher] = useState(fullCapabilities.secretsCipher);
  // canLogs reflects the backend's workloadLogs capability (the in-browser log viewer).
  const [canLogs, setCanLogs] = useState(fullCapabilities.workloadLogs);
  // canExecCap reflects the backend's workloadExec capability (the in-browser terminal).
  const [canExecCap, setCanExecCap] = useState(fullCapabilities.workloadExec);
  const [user, setUser] = useState<User | null>(null);
  const [needsLogin, setNeedsLogin] = useState(false);
  const [meta, setMeta] = useState<ClusterMeta | null>(null);
  const [distributions, setDistributions] = useState<string[]>(DEFAULT_DISTRIBUTIONS);
  // providerStatus gates the create form's provider options. null = backend does not gate (operator).
  const [providerStatus, setProviderStatus] = useState<ProviderInfo[] | null>(null);
  const [settingsEnabled, setSettingsEnabled] = useState(false);
  const [mode, setMode] = useState<Config["mode"]>(undefined);
  const [view, setView] = useState<View>("clusters");
  // paletteOpen controls the ⌘K command palette overlay.
  const [paletteOpen, setPaletteOpen] = useState(false);

  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [formMode, setFormMode] = useState<FormMode>("create");
  const [formInitial, setFormInitial] = useState<Cluster | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Cluster | null>(null);
  // streamReady gates the live SSE subscription: open it only after the initial load succeeded
  // without losing the session (see init), so it never races the auth handshake.
  const [streamReady, setStreamReady] = useState(false);

  const mounted = useRef(true);

  const selected = useMemo(
    () => clusters.find((cluster) => clusterKey(cluster) === selectedKey) ?? null,
    [clusters, selectedKey],
  );

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

  // applyConfig maps the deployment config onto UI state: read-only, the capability gates, and the
  // create-form options. Shared by the initial load and reloadConfig so the (growing) capability list
  // lives in one place. The state setters are stable, so this is safe to memoize with no deps.
  const applyConfig = useCallback((config: Config) => {
    setReadOnly(config.readOnly);
    setCanUpdate(config.capabilities?.clusterUpdate ?? fullCapabilities.clusterUpdate);
    setCanBrowse(config.capabilities?.workloadRead ?? fullCapabilities.workloadRead);
    setCanManage(config.capabilities?.workloadWrite ?? fullCapabilities.workloadWrite);
    setCanKubeconfig(config.capabilities?.kubeconfigDownload ?? fullCapabilities.kubeconfigDownload);
    setCanApply(config.capabilities?.applyManifests ?? fullCapabilities.applyManifests);
    setCanCipher(config.capabilities?.secretsCipher ?? fullCapabilities.secretsCipher);
    setCanLogs(config.capabilities?.workloadLogs ?? fullCapabilities.workloadLogs);
    setCanExecCap(config.capabilities?.workloadExec ?? fullCapabilities.workloadExec);
    setDistributions(config.distributions ?? DEFAULT_DISTRIBUTIONS);
    setProviderStatus(config.providers ?? null);
    setSettingsEnabled(config.settingsEnabled ?? false);
    setMode(config.mode);
  }, []);

  // reloadConfig re-fetches deployment config after a change that can affect it (e.g. saving
  // credential settings flips provider availability), so the create form's gating stays current
  // without a full page reload.
  const reloadConfig = useCallback(async () => {
    try {
      const config = await getConfig();
      if (!mounted.current) {
        return;
      }
      applyConfig(config);
    } catch {
      // Non-fatal: keep the current config if the refresh fails.
    }
  }, [applyConfig]);

  // Navigate in response to a ksail:// deep link from the desktop shell (no-op in the browser). Stable
  // setters → empty deps.
  const navigateDeepLink = useCallback((target: DeepLinkTarget) => {
    if (target.view) {
      setView(target.view);
    }
    if (target.clusterKey) {
      setSelectedKey(target.clusterKey);
    }
  }, []);
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
      let config;
      try {
        config = await getConfig();
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

      applyConfig(config);
      setUser(config.user ?? null);

      if (config.authEnabled && !config.user) {
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

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) {
      return;
    }

    const name = deleteTarget.metadata.name;
    const namespace = deleteTarget.metadata.namespace ?? "default";
    try {
      await deleteCluster(namespace, name);
      toast.success(`Deleting cluster "${name}"`);
      await refresh(true);
    } catch (err) {
      toast.error(errorMessage(err));
      throw err;
    }
  }, [deleteTarget, refresh, toast]);

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

  // navTo builds a "Go to <view>" palette command. Centralizes the shared label/run/hint shape.
  const navTo = (target: View, label: string, icon: ReactNode): Command => ({
    id: `nav-${target}`,
    label: `Go to ${label}`,
    hint: "Navigate",
    icon,
    run: () => setView(target),
  });

  // Command palette entries: navigation, common actions, and jump-to-cluster. Built each render
  // (cheap) from the live clusters list and the capability gates, so it stays in sync with the nav.
  const commands: Command[] = [
    navTo("clusters", "Clusters", <Server className="size-4" aria-hidden />),
    ...(canBrowse
      ? [
          navTo("overview", "Overview", <LayoutDashboard className="size-4" aria-hidden />),
          navTo("resources", "Resources", <Layers className="size-4" aria-hidden />),
          navTo("events", "Events", <Activity className="size-4" aria-hidden />),
        ]
      : []),
    ...(canCipher ? [navTo("secrets", "Secrets", <KeyRound className="size-4" aria-hidden />)] : []),
    ...(settingsEnabled ? [navTo("settings", "Settings", <Settings className="size-4" aria-hidden />)] : []),
    {
      id: "action-refresh",
      label: "Refresh clusters",
      hint: "Action",
      icon: <RotateCw className="size-4" aria-hidden />,
      run: () => {
        setView("clusters");
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
        hint: "Cluster",
        icon: <Server className="size-4" aria-hidden />,
        keywords: key,
        run: () => {
          setView("clusters");
          setSelectedKey(key);
        },
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
        onNavigate={setView}
        settingsEnabled={settingsEnabled}
        workloadEnabled={canBrowse}
        secretsEnabled={canCipher}
        surfaceLabel={surfaceLabel(mode)}
        onOpenCommandPalette={() => setPaletteOpen(true)}
        headerActions={headerActions}
      >
        {view === "settings" ? (
          <SettingsPage onSaved={() => void reloadConfig()} />
        ) : view === "secrets" ? (
          <SecretsView />
        ) : view === "overview" ? (
          <OverviewView clusters={clusters} />
        ) : view === "events" ? (
          <EventsView clusters={clusters} />
        ) : view === "resources" ? (
          <ResourcesView
            clusters={clusters}
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
                onSelect={(cluster) => setSelectedKey(clusterKey(cluster))}
                onEdit={openEdit}
                onDelete={(cluster) => setDeleteTarget(cluster)}
              />
            )}
          </div>
        )}

        {meta ? (
          <ClusterDetail
            cluster={selected}
            open={selected !== null}
            canEdit={canEdit}
            canDownloadKubeconfig={canKubeconfig}
            onClose={() => setSelectedKey(null)}
            onEdit={openEdit}
          />
        ) : null}

        {meta ? (
          <ClusterFormDialog
            open={formOpen}
            mode={formMode}
            initial={formInitial}
            distributions={distributions}
            providerStatus={providerStatus}
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
