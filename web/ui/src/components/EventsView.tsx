import { Activity, RotateCw } from "lucide-react";
import { useMemo, useState } from "react";
import type { Cluster } from "../api.ts";
import { useResourceList } from "../hooks/useResourceList.ts";
import { cx } from "../lib/cx.ts";
import { relativeAge } from "../lib/format.ts";
import { clusterKey, eventFields, eventLastSeenMs } from "../lib/k8s.ts";
import { EventTypeBadge } from "./EventList.tsx";
import { EmptyState } from "./states.tsx";
import { DataStates, TableCard, td, th } from "./table.tsx";
import { Button, SelectField, TextField } from "./ui.tsx";

type TypeFilter = "all" | "Warning" | "Normal";

// EventsView lists the active cluster's Kubernetes Events with event-specific columns, a Type filter,
// and free-text search — newest-first. Built on the shared useResourceList(kind="Event"); the cluster
// comes from the workspace context (no selector here).
export function EventsView({ cluster }: { cluster: Cluster | null }) {
  const [typeFilter, setTypeFilter] = useState<TypeFilter>("all");
  const [search, setSearch] = useState("");

  const key = cluster ? clusterKey(cluster) : "";
  const { items, loading, hasFetched, error, refresh } = useResourceList(key, "Event");

  // Normalize, filter (type + search), and sort newest-first.
  const rows = useMemo(() => {
    const needle = search.trim().toLowerCase();

    return items
      .map(eventFields)
      .filter((event) => (typeFilter === "all" ? true : event.type === typeFilter))
      .filter((event) =>
        needle === ""
          ? true
          : `${event.reason} ${event.objectKind} ${event.objectName} ${event.message}`.toLowerCase().includes(needle),
      )
      .sort((a, b) => eventLastSeenMs(b) - eventLastSeenMs(a));
  }, [items, typeFilter, search]);

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
        <TableCard>
          <thead className="bg-slate-50 dark:bg-slate-800/50">
            <tr>
              <th className={th}>Type</th>
              <th className={th}>Reason</th>
              <th className={cx(th, "hidden sm:table-cell")}>Object</th>
              <th className={th}>Message</th>
              <th className={cx(th, "hidden md:table-cell")}>Count</th>
              <th className={th}>Last seen</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
            {rows.map((event, index) => (
              <tr key={`${event.objectName}-${event.reason}-${index}`}>
                <td className={td}>
                  <EventTypeBadge type={event.type} />
                </td>
                <td className={cx(td, "font-medium text-slate-900 dark:text-white")}>{event.reason || "—"}</td>
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
                <td className={cx(td, "max-w-md text-sm text-slate-600 dark:text-slate-300")}>
                  <span className="line-clamp-2 break-words" title={event.message}>
                    {event.message || "—"}
                  </span>
                </td>
                <td className={cx(td, "hidden text-sm tabular-nums text-slate-500 md:table-cell dark:text-slate-400")}>
                  {event.count}
                </td>
                <td className={cx(td, "text-sm tabular-nums text-slate-500 dark:text-slate-400")}>
                  {relativeAge(event.lastSeen)}
                </td>
              </tr>
            ))}
          </tbody>
        </TableCard>
      </DataStates>
    </div>
  );
}
