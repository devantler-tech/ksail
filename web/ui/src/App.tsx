import { Plus, RotateCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
  type ClusterSpec,
  type Config,
  type ProviderInfo,
  type User,
} from "./api.ts";
import { MetaContext } from "./lib/meta.ts";
import { AppShell, type View } from "./components/AppShell.tsx";
import { SettingsPage } from "./components/SettingsPage.tsx";
import { ResourcesView } from "./components/ResourcesView.tsx";
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
  // canExecCap reflects the backend's workloadExec capability (the in-browser terminal).
  const [canExecCap, setCanExecCap] = useState(fullCapabilities.workloadExec);
  const [user, setUser] = useState<User | null>(null);
  const [needsLogin, setNeedsLogin] = useState(false);
  const [meta, setMeta] = useState<ClusterMeta | null>(null);
  const [distributions, setDistributions] = useState<string[]>(DEFAULT_DISTRIBUTIONS);
  // providerStatus gates the create form's provider options. null = backend does not gate (operator).
  const [providerStatus, setProviderStatus] = useState<ProviderInfo[] | null>(null);
  const [settingsEnabled, setSettingsEnabled] = useState(false);
  const [view, setView] = useState<View>("clusters");

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
    setCanExecCap(config.capabilities?.workloadExec ?? fullCapabilities.workloadExec);
    setDistributions(config.distributions ?? DEFAULT_DISTRIBUTIONS);
    setProviderStatus(config.providers ?? null);
    setSettingsEnabled(config.settingsEnabled ?? false);
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
        headerActions={headerActions}
      >
        {view === "settings" ? (
          <SettingsPage onSaved={() => void reloadConfig()} />
        ) : view === "secrets" ? (
          <SecretsView />
        ) : view === "resources" ? (
          <ResourcesView
            clusters={clusters}
            canWrite={!readOnly && canManage}
            canApply={!readOnly && canApply}
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
      </AppShell>
    </MetaContext.Provider>
  );
}
