import { Plus, RotateCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ApiError,
  createCluster,
  deleteCluster,
  getConfig,
  getMeta,
  listClusters,
  logout,
  updateCluster,
  type Cluster,
  type ClusterMeta,
  type ClusterSpec,
  type ProviderInfo,
  type User,
} from "./api.ts";
import { MetaContext } from "./lib/meta.ts";
import { AppShell, type View } from "./components/AppShell.tsx";
import { SettingsPage } from "./components/SettingsPage.tsx";
import { ClusterDetail } from "./components/ClusterDetail.tsx";
import {
  ClusterFormDialog,
  type ClusterFormValues,
  type FormMode,
} from "./components/ClusterFormDialog.tsx";
import { ClustersTable } from "./components/ClustersTable.tsx";
import { ConfirmDialog } from "./components/ConfirmDialog.tsx";
import { LoginScreen } from "./components/LoginScreen.tsx";
import { EmptyState, ErrorBanner, TableSkeleton } from "./components/states.tsx";
import { Button } from "./components/ui.tsx";
import { useTheme } from "./hooks/useTheme.ts";
import { useToast } from "./components/Toast.tsx";

const POLL_MS = 10000;

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

// specFromValues maps the form fields to a ClusterSpec. Node counts are parsed to numbers; the
// component enums are sent verbatim (default values serialize away server-side via omitzero).
function specFromValues(values: ClusterFormValues): ClusterSpec {
  const controlPlanes = Number.parseInt(values.controlPlanes, 10);
  const workers = Number.parseInt(values.workers, 10);

  // The form holds plain strings sourced from /api/v1/meta (the server's valid enum values); narrow
  // them to the generated enum unions on the way out.
  return {
    distribution: values.distribution as ClusterSpec["distribution"],
    provider: values.provider as ClusterSpec["provider"],
    controlPlanes: Number.isNaN(controlPlanes) ? undefined : controlPlanes,
    workers: Number.isNaN(workers) ? undefined : workers,
    cni: values.cni as ClusterSpec["cni"],
    csi: values.csi as ClusterSpec["csi"],
    cdi: values.cdi as ClusterSpec["cdi"],
    metricsServer: values.metricsServer as ClusterSpec["metricsServer"],
    loadBalancer: values.loadBalancer as ClusterSpec["loadBalancer"],
    certManager: values.certManager as ClusterSpec["certManager"],
    policyEngine: values.policyEngine as ClusterSpec["policyEngine"],
    gitOpsEngine: values.gitOpsEngine as ClusterSpec["gitOpsEngine"],
  };
}

export function App() {
  const { theme, toggle } = useTheme();
  const toast = useToast();

  const [readOnly, setReadOnly] = useState(false);
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

  const mounted = useRef(true);
  const pollRef = useRef<number | undefined>(undefined);

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
        if (pollRef.current) {
          window.clearInterval(pollRef.current);
          pollRef.current = undefined;
        }
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
  // credential settings flips provider availability), so the create form's gating stays current
  // without a full page reload.
  const reloadConfig = useCallback(async () => {
    try {
      const config = await getConfig();
      if (!mounted.current) {
        return;
      }
      setReadOnly(config.readOnly);
      setDistributions(config.distributions ?? DEFAULT_DISTRIBUTIONS);
      setProviderStatus(config.providers ?? null);
      setSettingsEnabled(config.settingsEnabled ?? false);
    } catch {
      // Non-fatal: keep the current config if the refresh fails.
    }
  }, []);

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

      setReadOnly(config.readOnly);
      setUser(config.user ?? null);
      setDistributions(config.distributions ?? DEFAULT_DISTRIBUTIONS);
      setProviderStatus(config.providers ?? null);
      setSettingsEnabled(config.settingsEnabled ?? false);

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
        // Only start polling if the initial load did not lose the session; otherwise the login
        // screen is shown and a poll would just fire another unauthorized request.
        if (authOK) {
          pollRef.current = window.setInterval(() => void refresh(true), POLL_MS);
        }
      }
    }

    void init();

    return () => {
      mounted.current = false;
      if (pollRef.current) {
        window.clearInterval(pollRef.current);
        pollRef.current = undefined;
      }
    };
  }, [refresh]);

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
      headerActions={headerActions}
    >
      {view === "settings" ? (
        <SettingsPage onSaved={() => void reloadConfig()} />
      ) : (
      <div className="mx-auto max-w-6xl space-y-4">
        {error && clusters.length > 0 ? (
          <ErrorBanner message={error} onRetry={() => void refresh()} />
        ) : null}

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
          readOnly={readOnly}
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
              <span className="font-medium text-slate-700 dark:text-slate-200">
                {deleteTarget.metadata.name}
              </span>{" "}
              and its underlying resources. This action cannot be undone.
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
