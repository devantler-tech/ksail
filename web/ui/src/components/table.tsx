import { ChevronDown, ChevronsUpDown, ChevronUp } from "lucide-react";
import { useCallback, useState, type ReactNode } from "react";
import { cx } from "../lib/cx.ts";
import { EmptyState, ErrorBanner, TableSkeleton } from "./states.tsx";

// th/td are the shared table cell classes used across the data tables (clusters, resources, events).
export const th = "px-4 py-2.5 text-left text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400";
export const td = "px-4 py-3 align-middle";

export type SortDir = "asc" | "desc";

// useSort tracks the active sort column and direction: clicking the active column flips direction, a
// different column sorts ascending. Generic over the table's column-key union.
export function useSort<K extends string>(initialKey: K, initialDir: SortDir = "asc") {
  const [sortKey, setSortKey] = useState<K>(initialKey);
  const [sortDir, setSortDir] = useState<SortDir>(initialDir);

  const toggleSort = useCallback(
    (key: K) => {
      if (key === sortKey) {
        setSortDir((dir) => (dir === "asc" ? "desc" : "asc"));
      } else {
        setSortKey(key);
        setSortDir("asc");
      }
    },
    [sortKey],
  );

  return { sortKey, sortDir, toggleSort };
}

// SortHeader renders a clickable, accessible column header with an asc/desc/idle sort indicator. The
// component derives `active` from activeKey === sortKey so callers don't repeat that comparison.
export function SortHeader<K extends string>({
  label,
  sortKey,
  activeKey,
  dir,
  onSort,
  className,
}: {
  label: ReactNode;
  sortKey: K;
  activeKey: K;
  dir: SortDir;
  onSort: (key: K) => void;
  className?: string;
}) {
  const active = activeKey === sortKey;
  const Indicator = active ? (dir === "asc" ? ChevronUp : ChevronDown) : ChevronsUpDown;

  return (
    <th className={cx(th, className)} aria-sort={active ? (dir === "asc" ? "ascending" : "descending") : "none"}>
      <button
        type="button"
        onClick={() => onSort(sortKey)}
        className="group inline-flex items-center gap-1 rounded uppercase tracking-wide hover:text-slate-700 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-600 dark:hover:text-slate-200"
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

// TableCard wraps table markup (thead/tbody) in the standard bordered, horizontally-scrollable card.
export function TableCard({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900">
      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-slate-200 dark:divide-slate-800">{children}</table>
      </div>
    </div>
  );
}

// DataStates renders the standard error / loading-skeleton / empty gate for a data table, showing its
// children only when data is present. Shared by the Resources and Events views.
export function DataStates({
  error,
  loading,
  empty,
  emptyTitle,
  emptyDescription,
  emptyIcon,
  onRetry,
  children,
}: {
  error: string | null;
  loading: boolean;
  empty: boolean;
  emptyTitle: string;
  emptyDescription: string;
  // emptyIcon lets the view match the empty state to its subject (events, resources, …).
  emptyIcon?: ReactNode;
  onRetry: () => void;
  children: ReactNode;
}) {
  if (error) {
    return <ErrorBanner message={error} onRetry={onRetry} />;
  }
  if (loading) {
    return <TableSkeleton />;
  }
  if (empty) {
    return <EmptyState title={emptyTitle} description={emptyDescription} icon={emptyIcon} />;
  }

  return <>{children}</>;
}
