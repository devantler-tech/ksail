import { FileCode, Layers, RotateCw } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { deleteResource, errorMessage, listResources, type Cluster, type K8sObject } from "../api.ts";
import { cx } from "../lib/cx.ts";
import { useResourceKinds } from "../lib/meta.ts";
import { usePreferences, useTimeFormatters } from "../hooks/usePreferences.tsx";
import { clusterKey, recentEvents, splitClusterKey, type EventFields } from "../lib/k8s.ts";
import {
  buildResourceTarget,
  compareResources,
  currentReplicas,
  objectKey,
  podContainers,
  resourceStatus,
  type SortKey,
} from "../lib/resources.ts";
import { ApplyManifestsDialog } from "./ApplyManifestsDialog.tsx";
import { ConfirmDialog } from "./ConfirmDialog.tsx";
import { ResourceDetailPanel } from "./ResourceDetailPanel.tsx";
import { PodExec, PodLogs } from "./PodSession.tsx";
import { useResourceList } from "../hooks/useResourceList.ts";
import { EmptyState } from "./states.tsx";
import { DataStates, SortHeader, TableCard, TablePager, td, usePagination, useSort } from "./table.tsx";
import { StatusDot } from "./StatusBadge.tsx";
import { useToast } from "./Toast.tsx";
import { Button, SelectField, TextField } from "./ui.tsx";

// ResourceStatusBadge renders a resource's derived status with a colour dot (green = ok, amber =
// not). Distinct from StatusBadge.tsx's cluster-phase pill, hence the longer name.
function ResourceStatusBadge({ obj }: { obj: K8sObject }) {
  const status = resourceStatus(obj);
  if (!status) {
    return <span className="text-slate-400">—</span>;
  }

  return (
    <span className="inline-flex items-center gap-1.5">
      <StatusDot tone={status.ok ? "ok" : "warn"} />
      <span className={status.ok ? "text-slate-600 dark:text-slate-300" : "text-amber-700 dark:text-amber-400"}>
        {status.label}
      </span>
    </span>
  );
}

// ResourcesView is the read-only workload browser: pick a cluster + resource kind, optionally filter
// by namespace (client-side), and inspect any object's raw manifest. Backed by the ResourceService
// endpoints; shown only when the backend advertises capabilities.workloadRead. The detail slide-over,
// pod logs/exec panels, and the per-action wiring live in ResourceDetailPanel / PodSession; this
// component owns the kind/filter/list state and the write-action orchestration (runAction).
export function ResourcesView({
  cluster,
  canWrite,
  canApply,
  canLogs,
  canExec,
}: {
  // cluster is the active cluster from the workspace context (the single fetch target; no selector).
  cluster: Cluster | null;
  // canWrite gates the write actions (scale/restart/delete) — true only when the backend advertises
  // workloadWrite AND the UI is not read-only.
  canWrite: boolean;
  // canApply gates the Apply YAML action (applyManifests && !readOnly).
  canApply: boolean;
  // canLogs gates the Logs action on Pods. Logs are read-only, so this is just workloadLogs (no
  // !readOnly), letting log viewing work even in read-only/GitOps mode.
  canLogs: boolean;
  // canExec gates the Exec terminal action on Pods (workloadExec && !readOnly).
  canExec: boolean;
}) {
  const toast = useToast();
  const { prefs, setPreference } = usePreferences();
  const { format } = useTimeFormatters();
  // kindLists drives the kind selector and the action affordances: served by /api/v1/meta's
  // resourceKinds on current backends, with the api.ts constants as the older-backend fallback.
  const kindLists = useResourceKinds();
  // clusterId is the active cluster's "namespace/name" — the fetch target shared by every request.
  const clusterId = cluster ? clusterKey(cluster) : "";
  const [kind, setKind] = useState<string>("Pod");
  // Seed the namespace filter from the default-namespace preference (empty = all namespaces); it
  // stays freely editable — this is a starting value, not a lock.
  const [namespaceFilter, setNamespaceFilter] = useState(prefs.defaultNamespace);
  const [nameFilter, setNameFilter] = useState("");
  const { sortKey, sortDir, toggleSort } = useSort<SortKey>("name");
  // The list, its load/error state, and refresh() come from the shared hook (the Events view uses it
  // too). hasFetched keeps the skeleton showing until the first list resolves.
  const { items, loading, hasFetched, error, refresh } = useResourceList(clusterId, kind);
  const [selected, setSelected] = useState<K8sObject | null>(null);
  // Action state for the selected resource (scale/restart/delete).
  const [scaleValue, setScaleValue] = useState("0");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [actionBusy, setActionBusy] = useState(false);
  const [applyOpen, setApplyOpen] = useState(false);
  // Logs target: the Pod being viewed (null = closed) + the chosen container.
  const [logPod, setLogPod] = useState<K8sObject | null>(null);
  const [logContainer, setLogContainer] = useState("");
  // Exec target: the Pod being exec'd (null = closed) + the chosen container.
  const [execPod, setExecPod] = useState<K8sObject | null>(null);
  const [execContainer, setExecContainer] = useState("");
  // detailFormat is the manifest rendering of the selected resource (kubectl-familiar YAML by
  // default). It is a persisted preference (kept across selections and sessions), sourced from the
  // preferences store rather than local state.
  const detailFormat = prefs.detailFormat;
  // relatedEvents holds the recent events targeting the selected resource (its namespace, matched by
  // involvedObject), shown in the detail panel.
  const [relatedEvents, setRelatedEvents] = useState<EventFields[]>([]);

  // Seed the scale input from the selected object's current replica count whenever it changes.
  useEffect(() => {
    if (selected) {
      setScaleValue(String(currentReplicas(selected)));
    }
  }, [selected]);

  // Switching the active cluster (sidebar switcher) keeps this view mounted, so close anything still
  // targeting the previous cluster — a detail panel, log stream, exec session, or confirm dialog —
  // rather than letting it operate on (or stream from) the wrong cluster.
  useEffect(() => {
    setSelected(null);
    setLogPod(null);
    setExecPod(null);
    setDeleteOpen(false);
    setApplyOpen(false);
  }, [clusterId]);

  // Fetch the events targeting the selected resource (its own namespace, matched by involvedObject),
  // for the detail panel's "Related events" section. Skipped for Events themselves and unnamed
  // objects. Best-effort: a failure just yields no related events.
  useEffect(() => {
    const name = selected?.metadata?.name ?? "";
    if (!selected || name === "" || kind === "Event") {
      setRelatedEvents([]);

      return undefined;
    }

    const [clusterNamespace, clusterName] = splitClusterKey(clusterId);
    let cancelled = false;

    listResources(clusterNamespace, clusterName, "Event", selected.metadata?.namespace)
      .then((list) => {
        if (cancelled) {
          return;
        }
        setRelatedEvents(
          recentEvents(list.items ?? [], {
            matches: (event) => event.objectName === name && (event.objectKind === "" || event.objectKind === kind),
            limit: 10,
          }),
        );
      })
      .catch(() => {
        if (!cancelled) {
          setRelatedEvents([]);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [selected, kind, clusterId]);

  // runAction performs a write action on the selected resource, toasts the outcome, closes the detail
  // panel + delete dialog, and refetches the list. It rethrows on failure so an awaiting caller (the
  // delete ConfirmDialog) keeps its dialog open and clears its spinner; the fire-and-forget callers
  // (the action-bar buttons) go through runActionFireAndForget below, which swallows the rejection.
  function runAction(verb: string, perform: () => Promise<void>): Promise<void> {
    const targetName = selected?.metadata?.name ?? "resource";

    setActionBusy(true);

    return perform()
      .then(() => {
        toast.success(`${verb} ${targetName}`);
        setSelected(null);
        setDeleteOpen(false);
        refresh();
      })
      .catch((err: unknown) => {
        toast.error(errorMessage(err));
        throw err;
      })
      .finally(() => setActionBusy(false));
  }

  // runActionFireAndForget runs a write action without awaiting it (the action-bar buttons), letting
  // runAction toast both outcomes; the trailing catch only prevents an unhandled-rejection warning
  // from the rethrow runAction does for the delete path.
  function runActionFireAndForget(verb: string, perform: () => Promise<void>) {
    void runAction(verb, perform).catch(() => undefined);
  }

  // requestDelete is the detail panel's delete trigger: it opens the confirm dialog, or — when the
  // confirm-destructive preference is off — deletes the selected resource immediately (the same
  // action the dialog's confirm runs).
  function requestDelete() {
    if (prefs.confirmDestructive) {
      setDeleteOpen(true);

      return;
    }
    if (selected) {
      runActionFireAndForget("Deleted", () => deleteResource(buildResourceTarget(clusterId, kind, selected)));
    }
  }

  // Apply the namespace + name filters (client-side) and sort by the active column with a stable
  // namespace/name tiebreak so equal values keep a deterministic order across refreshes.
  const filtered = useMemo(() => {
    const nsQuery = namespaceFilter.trim().toLowerCase();
    const nameQuery = nameFilter.trim().toLowerCase();

    const matched = items.filter((item) => {
      const ns = (item.metadata?.namespace ?? "").toLowerCase();
      const nm = (item.metadata?.name ?? "").toLowerCase();

      return (nsQuery === "" || ns.includes(nsQuery)) && (nameQuery === "" || nm.includes(nameQuery));
    });

    const factor = sortDir === "asc" ? 1 : -1;

    return [...matched].sort((a, b) => {
      const primary = compareResources(a, b, sortKey) * factor;

      return primary !== 0 ? primary : objectKey(a, 0).localeCompare(objectKey(b, 0));
    });
  }, [items, namespaceFilter, nameFilter, sortKey, sortDir]);

  // Paginate the sorted+filtered rows per the rows-per-page preference (0 = show all).
  const pagination = usePagination(filtered, prefs.rowsPerPage);

  if (!cluster) {
    return <EmptyState title="No cluster selected" description="Choose a cluster to browse its resources." />;
  }

  const [selectedNamespace, selectedName] = splitClusterKey(clusterId);

  // openLogs/openExec switch the detail panel for the chosen pod's logs/exec session, seeding the
  // container picker with the pod's first container.
  function openLogs(pod: K8sObject) {
    setLogContainer(podContainers(pod)[0] ?? "");
    setLogPod(pod);
    setSelected(null);
  }

  function openExec(pod: K8sObject) {
    setExecContainer(podContainers(pod)[0] ?? "");
    setExecPod(pod);
    setSelected(null);
  }

  return (
    <div className="mx-auto max-w-6xl space-y-4">
      <div className="flex flex-wrap items-end gap-3">
        <SelectField label="Kind" value={kind} onChange={(event) => setKind(event.target.value)} className="min-w-40">
          {kindLists.kinds.map((name) => (
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
        <TextField
          label="Name filter"
          placeholder="all names"
          value={nameFilter}
          onChange={(event) => setNameFilter(event.target.value)}
          className="min-w-44"
        />
        <Button variant="secondary" onClick={() => refresh()} loading={loading}>
          {loading ? null : <RotateCw className="size-4" aria-hidden />}
          Refresh
        </Button>
        {canApply ? (
          <Button variant="secondary" onClick={() => setApplyOpen(true)}>
            <FileCode className="size-4" aria-hidden />
            Apply YAML
          </Button>
        ) : null}
      </div>

      <DataStates
        error={error}
        loading={loading || !hasFetched}
        empty={filtered.length === 0}
        emptyTitle={`No ${kind} resources`}
        emptyDescription="Nothing to show for this selection."
        emptyIcon={<Layers className="size-6" aria-hidden />}
        onRetry={refresh}
      >
        <TableCard>
          <thead className="bg-slate-50 dark:bg-slate-800/50">
            <tr>
              <SortHeader label="Name" sortKey="name" activeKey={sortKey} dir={sortDir} onSort={toggleSort} />
              <SortHeader
                label="Namespace"
                sortKey="namespace"
                activeKey={sortKey}
                dir={sortDir}
                onSort={toggleSort}
                className="hidden sm:table-cell"
              />
              <SortHeader label="Status" sortKey="status" activeKey={sortKey} dir={sortDir} onSort={toggleSort} />
              <SortHeader label="Age" sortKey="age" activeKey={sortKey} dir={sortDir} onSort={toggleSort} />
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
            {pagination.pageItems.map((item, index) => (
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
                <td className={cx(td, "font-medium text-slate-900 dark:text-white")}>{item.metadata?.name ?? "—"}</td>
                <td className={cx(td, "hidden text-sm text-slate-600 sm:table-cell dark:text-slate-300")}>
                  {item.metadata?.namespace ?? "—"}
                </td>
                <td className={cx(td, "text-sm")}>
                  <ResourceStatusBadge obj={item} />
                </td>
                <td className={cx(td, "text-sm text-slate-500 tabular-nums dark:text-slate-400")}>
                  {format(item.metadata?.creationTimestamp)}
                </td>
              </tr>
            ))}
          </tbody>
        </TableCard>
        <TablePager pagination={pagination} className="px-1 pt-3" />
      </DataStates>

      <ResourceDetailPanel
        selected={selected}
        kind={kind}
        clusterId={clusterId}
        kindLists={kindLists}
        canWrite={canWrite}
        canLogs={canLogs}
        canExec={canExec}
        actionBusy={actionBusy}
        runAction={runActionFireAndForget}
        scaleValue={scaleValue}
        onScaleChange={setScaleValue}
        detailFormat={detailFormat}
        onDetailFormatChange={(value) => setPreference("detailFormat", value)}
        relatedEvents={relatedEvents}
        onClose={() => setSelected(null)}
        onOpenLogs={openLogs}
        onOpenExec={openExec}
        onRequestDelete={requestDelete}
      />

      <ConfirmDialog
        open={deleteOpen}
        title={`Delete ${kind}`}
        description={
          selected ? (
            <>
              This permanently deletes{" "}
              <span className="font-medium text-slate-700 dark:text-slate-200">{selected.metadata?.name}</span>
              {selected.metadata?.namespace ? ` in namespace ${selected.metadata.namespace}` : ""}. This action cannot
              be undone.
            </>
          ) : (
            ""
          )
        }
        confirmLabel="Delete"
        onConfirm={() =>
          selected
            ? runAction("Deleted", () => deleteResource(buildResourceTarget(clusterId, kind, selected)))
            : Promise.resolve()
        }
        onClose={() => setDeleteOpen(false)}
      />

      <ApplyManifestsDialog
        open={applyOpen}
        onClose={() => setApplyOpen(false)}
        clusterNamespace={selectedNamespace}
        clusterName={selectedName}
        onApplied={() => refresh()}
      />

      <PodLogs
        pod={logPod}
        container={logContainer}
        onContainerChange={setLogContainer}
        onClose={() => setLogPod(null)}
        target={{ clusterNamespace: selectedNamespace, clusterName: selectedName }}
      />

      <PodExec
        pod={execPod}
        container={execContainer}
        onContainerChange={setExecContainer}
        onClose={() => setExecPod(null)}
        target={{ clusterNamespace: selectedNamespace, clusterName: selectedName }}
      />
    </div>
  );
}
