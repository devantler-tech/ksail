import { ChevronRight, Pencil, Trash2 } from "lucide-react";
import { useMemo } from "react";
import type { Cluster } from "../api.ts";
import { cx } from "../lib/cx.ts";
import { epochMs } from "../lib/format.ts";
import { usePreferences, useTimeFormatters } from "../hooks/usePreferences.tsx";
import { clusterKey, clusterPhase, isHostCluster } from "../lib/k8s.ts";
import { HostBadge, StatusBadge } from "./StatusBadge.tsx";
import { SortHeader, TablePager, td, th, usePagination, useSort } from "./table.tsx";

type SortKey = "name" | "namespace" | "distribution" | "provider" | "status" | "nodes" | "age";

// compareBySortKey returns the ordering of two clusters for the active sort column.
function compareBySortKey(a: Cluster, b: Cluster, key: SortKey): number {
  switch (key) {
    case "name":
      return a.metadata.name.localeCompare(b.metadata.name);
    case "namespace":
      return (a.metadata.namespace ?? "default").localeCompare(b.metadata.namespace ?? "default");
    case "distribution":
      return (a.spec?.cluster?.distribution ?? "").localeCompare(b.spec?.cluster?.distribution ?? "");
    case "provider":
      return (a.spec?.cluster?.provider ?? "").localeCompare(b.spec?.cluster?.provider ?? "");
    case "status":
      return clusterPhase(a).localeCompare(clusterPhase(b));
    case "nodes":
      return (a.status?.nodesReady ?? -1) - (b.status?.nodesReady ?? -1);
    case "age":
      return epochMs(a.metadata.creationTimestamp) - epochMs(b.metadata.creationTimestamp);
    default:
      return 0;
  }
}

function NodeCount({ cluster }: { cluster: Cluster }) {
  const total = cluster.status?.nodesTotal;
  if (total === undefined) {
    return <span className="text-slate-400">—</span>;
  }

  const ready = cluster.status?.nodesReady ?? 0;
  const healthy = total > 0 && ready === total;

  return (
    <span
      className={cx(
        "tabular-nums",
        healthy ? "text-slate-700 dark:text-slate-200" : "text-amber-600 dark:text-amber-400",
      )}
    >
      {ready}/{total}
    </span>
  );
}

export function ClustersTable({
  clusters,
  readOnly,
  canEdit,
  onSelect,
  onEdit,
  onDelete,
}: {
  clusters: Cluster[];
  readOnly: boolean;
  // canEdit gates the per-row edit button independently of delete: a backend may allow delete but
  // not in-place update (the local `ksail open web`/desktop backend), so edit is hidden while delete stays.
  canEdit: boolean;
  onSelect: (cluster: Cluster) => void;
  onEdit: (cluster: Cluster) => void;
  onDelete: (cluster: Cluster) => void;
}) {
  const { format } = useTimeFormatters();
  const { prefs } = usePreferences();
  const { sortKey, sortDir, toggleSort } = useSort<SortKey>("name");

  // Sort a copy with a stable namespace/name tiebreaker so equal values keep a deterministic order
  // and the list never reorders between refreshes (the API list order is not guaranteed).
  const sorted = useMemo(() => {
    const factor = sortDir === "asc" ? 1 : -1;
    return [...clusters].sort((a, b) => {
      const primary = compareBySortKey(a, b, sortKey) * factor;
      return primary !== 0 ? primary : clusterKey(a).localeCompare(clusterKey(b));
    });
  }, [clusters, sortKey, sortDir]);

  const headerProps = { activeKey: sortKey, dir: sortDir, onSort: toggleSort } as const;

  // Paginate the sorted clusters per the rows-per-page preference (0 = show all).
  const pagination = usePagination(sorted, prefs.rowsPerPage);

  return (
    <div className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900">
      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-slate-200 dark:divide-slate-800">
          <thead className="bg-slate-50 dark:bg-slate-800/50">
            <tr>
              <SortHeader {...headerProps} label="Name" sortKey="name" />
              <SortHeader {...headerProps} label="Namespace" sortKey="namespace" />
              <SortHeader {...headerProps} label="Distribution" sortKey="distribution" />
              <SortHeader {...headerProps} label="Provider" sortKey="provider" className="hidden sm:table-cell" />
              <SortHeader {...headerProps} label="Status" sortKey="status" />
              <SortHeader {...headerProps} label="Nodes" sortKey="nodes" />
              <SortHeader {...headerProps} label="Age" sortKey="age" />
              <th className={cx(th, "w-10")}>
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
            {pagination.pageItems.map((cluster) => (
              <tr
                key={clusterKey(cluster)}
                role="button"
                tabIndex={0}
                aria-label={`View ${cluster.metadata.name}`}
                onClick={() => onSelect(cluster)}
                onKeyDown={(event) => {
                  // Make the row selectable by keyboard (Enter/Space), not just mouse. Ignore keys
                  // that bubble up from the nested Edit/Delete buttons (event.target is the button,
                  // not the row) so activating them does not also select the row.
                  if (event.target !== event.currentTarget) {
                    return;
                  }
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    onSelect(cluster);
                  }
                }}
                className="cursor-pointer transition-colors hover:bg-slate-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-blue-500 dark:hover:bg-slate-800/50"
              >
                <td className={cx(td, "font-medium text-slate-900 dark:text-white")}>
                  <span className="inline-flex items-center gap-2">
                    {cluster.metadata.name}
                    {isHostCluster(cluster) ? <HostBadge /> : null}
                  </span>
                </td>
                <td className={cx(td, "text-sm text-slate-600 dark:text-slate-300")}>
                  {cluster.metadata.namespace ?? "default"}
                </td>
                <td className={cx(td, "text-sm text-slate-600 dark:text-slate-300")}>
                  {cluster.spec?.cluster?.distribution ?? "—"}
                </td>
                <td className={cx(td, "hidden text-sm text-slate-600 sm:table-cell dark:text-slate-300")}>
                  {cluster.spec?.cluster?.provider ?? "—"}
                </td>
                <td className={td}>
                  <StatusBadge phase={clusterPhase(cluster)} />
                </td>
                <td className={cx(td, "text-sm")}>
                  <NodeCount cluster={cluster} />
                </td>
                <td className={cx(td, "text-sm text-slate-500 tabular-nums dark:text-slate-400")}>
                  {format(cluster.metadata.creationTimestamp)}
                </td>
                <td className={cx(td, "text-right")}>
                  {/* Lifecycle actions are hidden for the host cluster (the cluster the operator
                      runs on); the API rejects them server-side anyway. */}
                  <div className="flex items-center justify-end gap-1">
                    {canEdit && !isHostCluster(cluster) ? (
                      <button
                        type="button"
                        aria-label={`Edit ${cluster.metadata.name}`}
                        onClick={(event) => {
                          event.stopPropagation();
                          onEdit(cluster);
                        }}
                        className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-blue-600 dark:hover:bg-slate-700 dark:hover:text-slate-200"
                      >
                        <Pencil className="size-4" />
                      </button>
                    ) : null}
                    {!readOnly && !isHostCluster(cluster) ? (
                      <button
                        type="button"
                        aria-label={`Delete ${cluster.metadata.name}`}
                        onClick={(event) => {
                          event.stopPropagation();
                          onDelete(cluster);
                        }}
                        className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-red-50 hover:text-red-600 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-red-600 dark:hover:bg-red-500/10 dark:hover:text-red-400"
                      >
                        <Trash2 className="size-4" />
                      </button>
                    ) : null}
                    <ChevronRight className="size-4 text-slate-300 dark:text-slate-600" aria-hidden />
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <TablePager
        pagination={pagination}
        className="border-t border-slate-200 px-4 py-3 dark:border-slate-800"
      />
    </div>
  );
}
