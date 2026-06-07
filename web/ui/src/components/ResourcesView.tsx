import { RotateCw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import {
  ApiError,
  listResources,
  RESOURCE_KINDS,
  type Cluster,
  type K8sObject,
} from "../api.ts";
import { cx } from "../lib/cx.ts";
import { relativeAge } from "../lib/format.ts";
import { EmptyState, ErrorBanner, TableSkeleton } from "./states.tsx";
import { Button, SelectField, SlideOver, TextField } from "./ui.tsx";

const th =
  "px-4 py-2.5 text-left text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400";
const td = "px-4 py-3 align-middle";

function clusterKey(cluster: Cluster): string {
  return `${cluster.metadata.namespace ?? "default"}/${cluster.metadata.name}`;
}

function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    return err.message;
  }

  return err instanceof Error ? err.message : String(err);
}

function objectKey(obj: K8sObject, index: number): string {
  const meta = obj.metadata;

  return `${meta?.namespace ?? ""}/${meta?.name ?? index}`;
}

// ResourcesView is the read-only workload browser: pick a cluster + resource kind, optionally filter
// by namespace (client-side), and inspect any object's raw manifest. Backed by the ResourceService
// endpoints; shown only when the backend advertises capabilities.workloadRead.
export function ResourcesView({ clusters }: { clusters: Cluster[] }) {
  const [selectedClusterKey, setSelectedClusterKey] = useState(
    clusters[0] ? clusterKey(clusters[0]) : "",
  );
  const [kind, setKind] = useState<string>("Pod");
  const [namespaceFilter, setNamespaceFilter] = useState("");
  const [items, setItems] = useState<K8sObject[]>([]);
  const [loading, setLoading] = useState(clusters.length > 0);
  // hasFetched gates the empty state so the skeleton (not "No <kind>") shows until the first list
  // resolves — including the window where clusters arrive over SSE after mount.
  const [hasFetched, setHasFetched] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<K8sObject | null>(null);
  const [nonce, setNonce] = useState(0);

  // Keep the selection in sync with the live list: snap to the first cluster when the selection is
  // empty (no clusters at mount) or its cluster was removed (e.g. deleted while this view is open),
  // so the dropdown's value and the cluster actually fetched never diverge.
  useEffect(() => {
    if (clusters.length === 0) {
      return;
    }

    if (!clusters.some((candidate) => clusterKey(candidate) === selectedClusterKey)) {
      setSelectedClusterKey(clusterKey(clusters[0]));
    }
  }, [clusters, selectedClusterKey]);

  // Re-fetch when the selected cluster, kind, or an explicit refresh (nonce) changes. Deps are
  // PRIMITIVES only (selectedClusterKey is "namespace/name") — not the cluster object — so live
  // cluster-list status churn pushed over SSE does not trigger spurious resource refetches. The
  // namespace filter is applied client-side (below), so editing it issues no request per keystroke.
  useEffect(() => {
    if (selectedClusterKey === "") {
      return undefined;
    }

    // selectedClusterKey is `${namespace}/${name}`; both segments are DNS labels (no "/"), so the
    // first slash separates them.
    const slash = selectedClusterKey.indexOf("/");
    const clusterNamespace = selectedClusterKey.slice(0, slash);
    const clusterName = selectedClusterKey.slice(slash + 1);

    let cancelled = false;
    setLoading(true);
    setError(null);

    listResources(clusterNamespace, clusterName, kind)
      .then((list) => {
        if (!cancelled) {
          setItems(list.items ?? []);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(errorMessage(err));
          setItems([]);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
          setHasFetched(true);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [selectedClusterKey, kind, nonce]);

  const filtered = useMemo(() => {
    const query = namespaceFilter.trim().toLowerCase();
    if (query === "") {
      return items;
    }

    return items.filter((item) => (item.metadata?.namespace ?? "").toLowerCase().includes(query));
  }, [items, namespaceFilter]);

  if (clusters.length === 0) {
    return (
      <EmptyState
        title="No clusters"
        description="Create or connect a cluster to browse its resources."
      />
    );
  }

  return (
    <div className="mx-auto max-w-6xl space-y-4">
      <div className="flex flex-wrap items-end gap-3">
        <SelectField
          label="Cluster"
          value={selectedClusterKey}
          onChange={(event) => setSelectedClusterKey(event.target.value)}
          className="min-w-44"
        >
          {clusters.map((candidate) => (
            <option key={clusterKey(candidate)} value={clusterKey(candidate)}>
              {candidate.metadata.name}
            </option>
          ))}
        </SelectField>
        <SelectField
          label="Kind"
          value={kind}
          onChange={(event) => setKind(event.target.value)}
          className="min-w-40"
        >
          {RESOURCE_KINDS.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </SelectField>
        <TextField
          label="Namespace filter"
          placeholder="all namespaces"
          value={namespaceFilter}
          onChange={(event) => setNamespaceFilter(event.target.value)}
          className="min-w-44"
        />
        <Button variant="secondary" onClick={() => setNonce((value) => value + 1)} loading={loading}>
          {loading ? null : <RotateCw className="size-4" aria-hidden />}
          Refresh
        </Button>
      </div>

      {error ? (
        <ErrorBanner message={error} onRetry={() => setNonce((value) => value + 1)} />
      ) : loading || !hasFetched ? (
        <TableSkeleton />
      ) : filtered.length === 0 ? (
        <EmptyState title={`No ${kind} resources`} description="Nothing to show for this selection." />
      ) : (
        <div className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900">
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-slate-200 dark:divide-slate-800">
              <thead className="bg-slate-50 dark:bg-slate-800/50">
                <tr>
                  <th className={th}>Name</th>
                  <th className={cx(th, "hidden sm:table-cell")}>Namespace</th>
                  <th className={th}>Age</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                {filtered.map((item, index) => (
                  <tr
                    key={objectKey(item, index)}
                    role="button"
                    tabIndex={0}
                    onClick={() => setSelected(item)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        setSelected(item);
                      }
                    }}
                    className="cursor-pointer transition-colors hover:bg-slate-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-blue-500 dark:hover:bg-slate-800/50"
                  >
                    <td className={cx(td, "font-medium text-slate-900 dark:text-white")}>
                      {item.metadata?.name ?? "—"}
                    </td>
                    <td
                      className={cx(td, "hidden text-sm text-slate-600 sm:table-cell dark:text-slate-300")}
                    >
                      {item.metadata?.namespace ?? "—"}
                    </td>
                    <td className={cx(td, "text-sm text-slate-500 tabular-nums dark:text-slate-400")}>
                      {relativeAge(item.metadata?.creationTimestamp)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      <SlideOver
        open={selected !== null}
        onClose={() => setSelected(null)}
        title={selected?.metadata?.name ?? ""}
        subtitle={`${kind}${selected?.metadata?.namespace ? ` · ${selected.metadata.namespace}` : ""}`}
      >
        {selected ? (
          <pre className="overflow-x-auto rounded-lg bg-slate-50 p-3 text-xs leading-relaxed text-slate-800 dark:bg-slate-800/50 dark:text-slate-200">
            {JSON.stringify(selected, null, 2)}
          </pre>
        ) : null}
      </SlideOver>
    </div>
  );
}
