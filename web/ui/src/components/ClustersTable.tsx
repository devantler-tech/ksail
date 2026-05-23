import { ChevronDown, ChevronRight, ChevronsUpDown, ChevronUp, Pencil, Trash2 } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import type { Cluster } from "../api.ts";
import { cx } from "../lib/cx.ts";
import { relativeAge } from "../lib/format.ts";
import { StatusBadge } from "./StatusBadge.tsx";

type SortKey = "name" | "namespace" | "distribution" | "provider" | "status" | "nodes" | "age";
type SortDir = "asc" | "desc";

function clusterKey(cluster: Cluster): string {
  return `${cluster.metadata.namespace ?? "default"}/${cluster.metadata.name}`;
}

function createdMs(cluster: Cluster): number {
  const value = cluster.metadata.creationTimestamp;
  const ms = value ? new Date(value).getTime() : 0;
  return Number.isNaN(ms) ? 0 : ms;
}

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
      return (a.status?.phase ?? "").localeCompare(b.status?.phase ?? "");
    case "nodes":
      return (a.status?.nodesReady ?? -1) - (b.status?.nodesReady ?? -1);
    case "age":
      return createdMs(a) - createdMs(b);
    default:
      return 0;
  }
}

const th = "px-4 py-2.5 text-left text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400";
const td = "px-4 py-3 align-middle";

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

function SortHeader({
  label,
  sortKey,
  active,
  dir,
  onSort,
  className,
}: {
  label: ReactNode;
  sortKey: SortKey;
  active: boolean;
  dir: SortDir;
  onSort: (key: SortKey) => void;
  className?: string;
}) {
  const Indicator = active ? (dir === "asc" ? ChevronUp : ChevronDown) : ChevronsUpDown;

  return (
    <th
      className={cx(th, className)}
      aria-sort={active ? (dir === "asc" ? "ascending" : "descending") : "none"}
    >
      <button
        type="button"
        onClick={() => onSort(sortKey)}
        className="group inline-flex items-center gap-1 uppercase tracking-wide hover:text-slate-700 dark:hover:text-slate-200"
      >
        {label}
        <Indicator
          className={cx("size-3.5", active ? "text-slate-500 dark:text-slate-300" : "text-slate-300 dark:text-slate-600")}
          aria-hidden
        />
      </button>
    </th>
  );
}

export function ClustersTable({
  clusters,
  readOnly,
  onSelect,
  onEdit,
  onDelete,
}: {
  clusters: Cluster[];
  readOnly: boolean;
  onSelect: (cluster: Cluster) => void;
  onEdit: (cluster: Cluster) => void;
  onDelete: (cluster: Cluster) => void;
}) {
  const [sortKey, setSortKey] = useState<SortKey>("name");
  const [sortDir, setSortDir] = useState<SortDir>("asc");

  function handleSort(key: SortKey) {
    if (key === sortKey) {
      setSortDir((current) => (current === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir("asc");
    }
  }

  // Sort a copy with a stable namespace/name tiebreaker so equal values keep a deterministic order
  // and the list never reorders between refreshes (the API list order is not guaranteed).
  const sorted = useMemo(() => {
    const factor = sortDir === "asc" ? 1 : -1;
    return [...clusters].sort((a, b) => {
      const primary = compareBySortKey(a, b, sortKey) * factor;
      return primary !== 0 ? primary : clusterKey(a).localeCompare(clusterKey(b));
    });
  }, [clusters, sortKey, sortDir]);

  const headerProps = { active: false, dir: sortDir, onSort: handleSort } as const;

  return (
    <div className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900">
      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-slate-200 dark:divide-slate-800">
          <thead className="bg-slate-50 dark:bg-slate-800/50">
            <tr>
              <SortHeader {...headerProps} label="Name" sortKey="name" active={sortKey === "name"} />
              <SortHeader {...headerProps} label="Namespace" sortKey="namespace" active={sortKey === "namespace"} />
              <SortHeader {...headerProps} label="Distribution" sortKey="distribution" active={sortKey === "distribution"} />
              <SortHeader
                {...headerProps}
                label="Provider"
                sortKey="provider"
                active={sortKey === "provider"}
                className="hidden sm:table-cell"
              />
              <SortHeader {...headerProps} label="Status" sortKey="status" active={sortKey === "status"} />
              <SortHeader {...headerProps} label="Nodes" sortKey="nodes" active={sortKey === "nodes"} />
              <SortHeader {...headerProps} label="Age" sortKey="age" active={sortKey === "age"} />
              <th className={cx(th, "w-10")}>
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
            {sorted.map((cluster) => (
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
                <td className={cx(td, "text-sm text-slate-500 tabular-nums dark:text-slate-400")}>
                  {relativeAge(cluster.metadata.creationTimestamp)}
                </td>
                <td className={cx(td, "text-right")}>
                  <div className="flex items-center justify-end gap-1">
                    {!readOnly ? (
                      <>
                        <button
                          type="button"
                          aria-label={`Edit ${cluster.metadata.name}`}
                          onClick={(event) => {
                            event.stopPropagation();
                            onEdit(cluster);
                          }}
                          className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700 dark:hover:bg-slate-700 dark:hover:text-slate-200"
                        >
                          <Pencil className="size-4" />
                        </button>
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
                      </>
                    ) : null}
                    <ChevronRight className="size-4 text-slate-300 dark:text-slate-600" aria-hidden />
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
