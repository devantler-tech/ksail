import { ChevronDown, ChevronLeft, ChevronRight, ChevronsUpDown, ChevronUp } from "lucide-react";
import { useCallback, useEffect, useState, type ReactNode } from "react";
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

// Pagination is the slice + page metadata usePagination returns. rowsPerPage 0 means "show all".
export interface Pagination<T> {
  pageItems: T[];
  page: number;
  totalPages: number;
  total: number;
  // rangeStart/rangeEnd are the 1-based bounds of the visible slice (0/0 when empty), for the pager
  // summary ("11–20 of 57").
  rangeStart: number;
  rangeEnd: number;
  setPage: (page: number) => void;
}

// usePagination slices items into pages of rowsPerPage (0 = all on one page). It clamps the current
// page when the item count shrinks (e.g. a filter narrows the list) so the view never lands on an
// empty page past the end. Apply it AFTER sorting/filtering — it paginates whatever array it is given.
export function usePagination<T>(items: T[], rowsPerPage: number): Pagination<T> {
  const [page, setPage] = useState(1);

  const perPage = rowsPerPage > 0 ? rowsPerPage : Math.max(1, items.length);
  const totalPages = Math.max(1, Math.ceil(items.length / perPage));

  // Snap back into range whenever the total page count drops below the current page.
  useEffect(() => {
    setPage((current) => Math.min(current, totalPages));
  }, [totalPages]);

  const current = Math.min(page, totalPages);
  const start = rowsPerPage > 0 ? (current - 1) * rowsPerPage : 0;
  const pageItems = rowsPerPage > 0 ? items.slice(start, start + rowsPerPage) : items;

  return {
    pageItems,
    page: current,
    totalPages,
    total: items.length,
    rangeStart: items.length === 0 ? 0 : start + 1,
    rangeEnd: start + pageItems.length,
    setPage,
  };
}

// TablePager renders the prev/next page controls and a range summary under a table. It renders
// nothing when there is only a single page, so callers can include it unconditionally.
export function TablePager<T>({ pagination, className }: { pagination: Pagination<T>; className?: string }) {
  const { page, totalPages, total, rangeStart, rangeEnd, setPage } = pagination;
  if (totalPages <= 1) {
    return null;
  }

  const pageButton =
    "inline-flex size-8 items-center justify-center rounded-md text-slate-500 ring-1 ring-inset ring-slate-200 transition-colors hover:bg-slate-50 hover:text-slate-700 disabled:cursor-not-allowed disabled:opacity-40 dark:text-slate-400 dark:ring-slate-700 dark:hover:bg-slate-800 dark:hover:text-slate-200";

  return (
    <div
      className={cx(
        "flex items-center justify-between gap-3 text-xs text-slate-500 dark:text-slate-400",
        className,
      )}
    >
      <span className="tabular-nums">
        {rangeStart}–{rangeEnd} of {total}
      </span>
      <div className="flex items-center gap-2">
        <span className="tabular-nums">
          Page {page} of {totalPages}
        </span>
        <button
          type="button"
          aria-label="Previous page"
          disabled={page <= 1}
          onClick={() => setPage(page - 1)}
          className={pageButton}
        >
          <ChevronLeft className="size-4" aria-hidden />
        </button>
        <button
          type="button"
          aria-label="Next page"
          disabled={page >= totalPages}
          onClick={() => setPage(page + 1)}
          className={pageButton}
        >
          <ChevronRight className="size-4" aria-hidden />
        </button>
      </div>
    </div>
  );
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
