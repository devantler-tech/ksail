import { ChevronRight, Trash2 } from "lucide-react";
import type { Cluster } from "../api.ts";
import { cx } from "../lib/cx.ts";
import { relativeAge } from "../lib/format.ts";
import { StatusBadge } from "./StatusBadge.tsx";

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

const th = "px-4 py-2.5 text-left text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400";
const td = "px-4 py-3 align-middle";

export function ClustersTable({
  clusters,
  readOnly,
  onSelect,
  onDelete,
}: {
  clusters: Cluster[];
  readOnly: boolean;
  onSelect: (cluster: Cluster) => void;
  onDelete: (cluster: Cluster) => void;
}) {
  return (
    <div className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900">
      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-slate-200 dark:divide-slate-800">
          <thead className="bg-slate-50 dark:bg-slate-800/50">
            <tr>
              <th className={th}>Name</th>
              <th className={th}>Namespace</th>
              <th className={th}>Distribution</th>
              <th className={cx(th, "hidden sm:table-cell")}>Provider</th>
              <th className={th}>Status</th>
              <th className={th}>Nodes</th>
              <th className={cx(th, "hidden lg:table-cell")}>Endpoint</th>
              <th className={th}>Age</th>
              <th className={cx(th, "w-10")}>
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
            {clusters.map((cluster) => {
              const key = `${cluster.metadata.namespace ?? "default"}/${cluster.metadata.name}`;
              return (
                <tr
                  key={key}
                  onClick={() => onSelect(cluster)}
                  className="cursor-pointer transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/50"
                >
                  <td className={cx(td, "font-medium text-slate-900 dark:text-white")}>
                    {cluster.metadata.name}
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
                    <StatusBadge phase={cluster.status?.phase} />
                  </td>
                  <td className={cx(td, "text-sm")}>
                    <NodeCount cluster={cluster} />
                  </td>
                  <td className={cx(td, "hidden max-w-xs lg:table-cell")}>
                    <span
                      className="block truncate font-mono text-xs text-slate-500 dark:text-slate-400"
                      title={cluster.status?.endpoint ?? ""}
                    >
                      {cluster.status?.endpoint ?? "—"}
                    </span>
                  </td>
                  <td className={cx(td, "text-sm text-slate-500 tabular-nums dark:text-slate-400")}>
                    {relativeAge(cluster.metadata.creationTimestamp)}
                  </td>
                  <td className={cx(td, "text-right")}>
                    <div className="flex items-center justify-end gap-1">
                      {!readOnly ? (
                        <button
                          type="button"
                          aria-label={`Delete ${cluster.metadata.name}`}
                          onClick={(event) => {
                            event.stopPropagation();
                            onDelete(cluster);
                          }}
                          className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-500/10 dark:hover:text-red-400"
                        >
                          <Trash2 className="size-4" />
                        </button>
                      ) : null}
                      <ChevronRight className="size-4 text-slate-300 dark:text-slate-600" aria-hidden />
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
