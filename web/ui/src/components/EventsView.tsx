import { Activity, RotateCw } from "lucide-react";
import { useMemo, useState } from "react";
import type { Cluster } from "../api.ts";
import { useResourceList } from "../hooks/useResourceList.ts";
import { usePreferences, useTimeFormatters } from "../hooks/usePreferences.tsx";
import { cx } from "../lib/cx.ts";
import { clusterKey, recentEvents } from "../lib/k8s.ts";
import { EventTypeBadge } from "./EventList.tsx";
import { EmptyState } from "./states.tsx";
import { DataStates, TableCard, TablePager, td, th, usePagination } from "./table.tsx";
import { Button, SelectField, TextField } from "./ui.tsx";

type TypeFilter = "all" | "Warning" | "Normal";

// EventsView lists the active cluster's Kubernetes Events with event-specific columns, a Type filter,
// and free-text search — newest-first. Built on the shared useResourceList(kind="Event"); the cluster
// comes from the workspace context (no selector here).
export function EventsView({ cluster }: { cluster: Cluster | null }) {
  const { format } = useTimeFormatters();
  const { prefs } = usePreferences();
  const [typeFilter, setTypeFilter] = useState<TypeFilter>("all");
  const [search, setSearch] = useState("");

  const key = cluster ? clusterKey(cluster) : "";
  const { items, loading, hasFetched, error, refresh } = useResourceList(key, "Event");

  // Normalize, filter (type + search), and sort newest-first.
  const rows = useMemo(() => {
    const needle = search.trim().toLowerCase();

    return recentEvents(items, {
      type: typeFilter === "all" ? undefined : typeFilter,
      matches: (event) =>
        needle === ""
          ? true
          : `${event.reason} ${event.objectKind} ${event.objectName} ${event.message}`.toLowerCase().includes(needle),
    });
  }, [items, typeFilter, search]);

  // Paginate the filtered events per the rows-per-page preference (0 = show all).
  const pagination = usePagination(rows, prefs.rowsPerPage);

  if (!cluster) {
    return <EmptyState title="No cluster selected" description="Choose a cluster to view its events." />;
  }

  return (
    <div className="mx-auto max-w-6xl space-y-4">
      <div className="flex flex-wrap items-end gap-3">
        <SelectField
          label="Type"
          value={typeFilter}
          onChange={(event) => setTypeFilter(event.target.value as TypeFilter)}
          className="min-w-32"
        >
          <option value="all">All</option>
          <option value="Warning">Warning</option>
          <option value="Normal">Normal</option>
        </SelectField>
        <TextField
          label="Search"
          type="search"
          placeholder="reason, object, message…"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          className="min-w-52"
        />
        <Button variant="secondary" onClick={refresh} loading={loading}>
          {loading ? null : <RotateCw className="size-4" aria-hidden />}
          Refresh
        </Button>
      </div>

      <DataStates
        error={error}
        loading={loading || !hasFetched}
        empty={rows.length === 0}
        emptyTitle="No events"
        emptyDescription="Nothing to show for this selection."
        emptyIcon={<Activity className="size-6" aria-hidden />}
        onRetry={refresh}
      >
        <TableCard tableClassName="w-full table-fixed sm:table-auto">
          <thead className="bg-slate-50 dark:bg-slate-800/50">
            <tr>
              <th className={cx(th, "w-24")}>Type</th>
              <th className={th}>
                <span className="sm:hidden">Event</span>
                <span className="hidden sm:inline">Reason</span>
              </th>
              <th className={cx(th, "hidden sm:table-cell")}>Object</th>
              <th className={cx(th, "hidden sm:table-cell")}>Message</th>
              <th className={cx(th, "hidden md:table-cell")}>Count</th>
              <th className={cx(th, "hidden sm:table-cell")}>Last seen</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
            {pagination.pageItems.map((event, index) => (
              <tr key={`${event.objectName}-${event.reason}-${index}`}>
                <td className={td}>
                  <EventTypeBadge type={event.type} />
                </td>
                <td className={cx(td, "min-w-0 text-slate-900 dark:text-white")}>
                  <span className="block break-words font-medium sm:truncate" title={event.reason}>
                    {event.reason || "—"}
                  </span>
                  {event.objectName ? (
                    <span
                      className="mt-1 block truncate text-xs text-slate-500 sm:hidden dark:text-slate-400"
                      title={`${event.objectKind ? `${event.objectKind}/` : ""}${event.objectName}`}
                    >
                      {event.objectKind ? `${event.objectKind}/` : ""}
                      {event.objectName}
                    </span>
                  ) : null}
                  {event.message ? (
                    <p
                      className="mt-1 line-clamp-3 break-words text-xs font-normal text-slate-500 sm:hidden dark:text-slate-400"
                      title={event.message}
                    >
                      {event.message}
                    </p>
                  ) : null}
                  <span className="mt-1 block text-xs font-normal tabular-nums text-slate-400 sm:hidden">
                    {format(event.lastSeen)}
                  </span>
                </td>
                <td className={cx(td, "hidden text-sm text-slate-600 sm:table-cell dark:text-slate-300")}>
                  {event.objectName ? (
                    <span className="break-words">
                      {event.objectKind ? `${event.objectKind}/` : ""}
                      {event.objectName}
                    </span>
                  ) : (
                    "—"
                  )}
                </td>
                <td className={cx(td, "hidden max-w-md text-sm text-slate-600 sm:table-cell dark:text-slate-300")}>
                  <span className="line-clamp-2 break-words" title={event.message}>
                    {event.message || "—"}
                  </span>
                </td>
                <td className={cx(td, "hidden text-sm tabular-nums text-slate-500 md:table-cell dark:text-slate-400")}>
                  {event.count}
                </td>
                <td className={cx(td, "hidden text-sm tabular-nums text-slate-500 sm:table-cell dark:text-slate-400")}>
                  {format(event.lastSeen)}
                </td>
              </tr>
            ))}
          </tbody>
        </TableCard>
        <TablePager pagination={pagination} className="px-1 pt-3" />
      </DataStates>
    </div>
  );
}
