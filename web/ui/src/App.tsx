import { Plus, RotateCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ApiError,
  createCluster,
  deleteCluster,
  getConfig,
  listClusters,
  logout,
  type Cluster,
  type User,
} from "./api.ts";
import { AppShell } from "./components/AppShell.tsx";
import { ClusterDetail } from "./components/ClusterDetail.tsx";
import { ClustersTable } from "./components/ClustersTable.tsx";
import { ConfirmDialog } from "./components/ConfirmDialog.tsx";
import { CreateClusterDialog, type CreateClusterInput } from "./components/CreateClusterDialog.tsx";
import { LoginScreen } from "./components/LoginScreen.tsx";
import { EmptyState, ErrorBanner, TableSkeleton } from "./components/states.tsx";
import { Button } from "./components/ui.tsx";
import { useTheme } from "./hooks/useTheme.ts";
import { useToast } from "./components/Toast.tsx";

// Distributions the operator can currently provision in-cluster (see pkg/operator/provisioner.go).
const DISTRIBUTIONS = ["VCluster"];

const POLL_MS = 10000;

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
  const [user, setUser] = useState<User | null>(null);
  const [needsLogin, setNeedsLogin] = useState(false);

  const [clusters, setClusters] = useState<Cluster[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Cluster | null>(null);

  const mounted = useRef(true);
  const pollRef = useRef<number | undefined>(undefined);

  const selected = useMemo(
    () => clusters.find((cluster) => clusterKey(cluster) === selectedKey) ?? null,
    [clusters, selectedKey],
  );

  const refresh = useCallback(async (silent = false) => {
    if (!silent) {
      setRefreshing(true);
    }

    try {
      const list = await listClusters();
      if (!mounted.current) {
        return;
      }
      setClusters(list.items ?? []);
      setError(null);
    } catch (err) {
      if (!mounted.current) {
        return;
      }
      if (err instanceof ApiError && err.status === 401) {
        if (pollRef.current) {
          window.clearInterval(pollRef.current);
          pollRef.current = undefined;
        }
        setNeedsLogin(true);
        return;
      }
      setError(errorMessage(err));
    } finally {
      if (mounted.current && !silent) {
        setRefreshing(false);
      }
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

      if (config.authEnabled && !config.user) {
        setNeedsLogin(true);
        setLoading(false);
        return;
      }

      await refresh(true);
      if (mounted.current) {
        setLoading(false);
        pollRef.current = window.setInterval(() => void refresh(true), POLL_MS);
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

  const handleCreate = useCallback(
    async (input: CreateClusterInput) => {
      try {
        await createCluster({
          metadata: { name: input.name, namespace: input.namespace },
          spec: { cluster: { distribution: input.distribution } },
        });
        toast.success(`Cluster "${input.name}" created`);
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

  const headerActions = (
    <>
      <Button variant="secondary" size="sm" onClick={() => void refresh()} loading={refreshing}>
        {refreshing ? null : <RotateCw className="size-4" aria-hidden />}
        Refresh
      </Button>
      {!readOnly ? (
        <Button size="sm" onClick={() => setCreateOpen(true)}>
          <Plus className="size-4" aria-hidden />
          New cluster
        </Button>
      ) : null}
    </>
  );

  return (
    <AppShell
      theme={theme}
      onToggleTheme={toggle}
      user={user}
      onLogout={() => void logout().finally(() => setNeedsLogin(true))}
      readOnly={readOnly}
      headerActions={headerActions}
    >
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
                <Button onClick={() => setCreateOpen(true)}>
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
            onDelete={(cluster) => setDeleteTarget(cluster)}
          />
        )}
      </div>

      <ClusterDetail cluster={selected} open={selected !== null} onClose={() => setSelectedKey(null)} />

      <CreateClusterDialog
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreate={handleCreate}
        distributions={DISTRIBUTIONS}
      />

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
  );
}
