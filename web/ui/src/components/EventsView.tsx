import { RotateCw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { listResources, type Cluster, type K8sObject } from "../api.ts";
import { cx } from "../lib/cx.ts";
import { relativeAge } from "../lib/format.ts";
import { clusterKey, eventFields, eventLastSeenMs, splitClusterKey } from "../lib/k8s.ts";
import { EmptyState, ErrorBanner, TableSkeleton } from "./states.tsx";
import { Button, SelectField, TextField } from "./ui.tsx";

const th = "px-4 py-2.5 text-left text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400";
const td = "px-4 py-3 align-top";

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

// EventTypeBadge colours an event by its type: Warning stands out (amber), Normal is muted.
export function EventTypeBadge({ type }: { type: string }) {
  const warning = type === "Warning";

  return (
    <span
      className={cx(
        "inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset",
        warning
          ? "bg-amber-50 text-amber-700 ring-amber-600/20 dark:bg-amber-500/10 dark:text-amber-400 dark:ring-amber-500/30"
          : "bg-slate-100 text-slate-600 ring-slate-500/20 dark:bg-slate-700/40 dark:text-slate-300 dark:ring-slate-600/40",
      )}
    >
      {type || "Normal"}
    </span>
  );
}

type TypeFilter = "all" | "Warning" | "Normal";

// EventsView lists a cluster's Kubernetes Events with event-specific columns, a Type filter, and a
// free-text search — sorted newest-first. Built on the existing listResources(kind="Event") endpoint,
// so it needs no dedicated backend route; gated on capabilities.workloadRead (same as Resources).
export function EventsView({ clusters }: { clusters: Cluster[] }) {
  const [selectedClusterKey, setSelectedClusterKey] = useState(clusters[0] ? clusterKey(clusters[0]) : "");
  const [items, setItems] = useState<K8sObject[]>([]);
  const [loading, setLoading] = useState(clusters.length > 0);
  const [hasFetched, setHasFetched] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);
  const [typeFilter, setTypeFilter] = useState<TypeFilter>("all");
  const [search, setSearch] = useState("");

  useEffect(() => {
    if (clusters.length === 0) {
      return;
    }

    if (!clusters.some((candidate) => clusterKey(candidate) === selectedClusterKey)) {
      setSelectedClusterKey(clusterKey(clusters[0]));
    }
  }, [clusters, selectedClusterKey]);

  useEffect(() => {
    if (selectedClusterKey === "") {
      return undefined;
    }

    const [namespace, name] = splitClusterKey(selectedClusterKey);
    let cancelled = false;
    setLoading(true);
    setError(null);

    listResources(namespace, name, "Event")
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
  }, [selectedClusterKey, nonce]);

  // Normalize, filter (type + search), and sort newest-first. Recomputed only when inputs change.
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

  if (clusters.length === 0) {
    return <EmptyState title="No clusters" description="Create or connect a cluster to view its events." />;
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
          placeholder="reason, object, message"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          className="min-w-52"
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
      ) : rows.length === 0 ? (
        <EmptyState title="No events" description="Nothing to show for this selection." />
      ) : (
        <div className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-800 dark:bg-slate-900">
          <div className="overflow-x-auto">
            <table className="min-w-full divide-y divide-slate-200 dark:divide-slate-800">
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
            </table>
          </div>
        </div>
      )}
    </div>
  );
}
