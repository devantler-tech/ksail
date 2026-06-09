import { FileCode, RotateCw, ScrollText, SquareTerminal } from "lucide-react";
import { lazy, Suspense, useEffect, useMemo, useState } from "react";
import {
  ApiError,
  CLUSTER_SCOPED_KINDS,
  deleteResource,
  listResources,
  RESOURCE_KINDS,
  RESTARTABLE_KINDS,
  restartResource,
  scaleResource,
  SCALABLE_KINDS,
  type Cluster,
  type K8sObject,
} from "../api.ts";
import { cx } from "../lib/cx.ts";
import { relativeAge } from "../lib/format.ts";
import { ApplyManifestsDialog } from "./ApplyManifestsDialog.tsx";
import { LogViewer } from "./LogViewer.tsx";
import { ConfirmDialog } from "./ConfirmDialog.tsx";

// ExecTerminal pulls in xterm.js (~250 kB), so it is code-split: the chunk loads only when a terminal
// is actually opened, keeping it out of the initial bundle.
const ExecTerminal = lazy(() =>
  import("./ExecTerminal.tsx").then((module) => ({ default: module.ExecTerminal })),
);
import { EmptyState, ErrorBanner, TableSkeleton } from "./states.tsx";
import { useToast } from "./Toast.tsx";
import { Button, SelectField, SlideOver, TextField } from "./ui.tsx";

const th = "px-4 py-2.5 text-left text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400";
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

// splitClusterKey parses "namespace/name" into its segments (both are DNS labels, no "/").
function splitClusterKey(key: string): [string, string] {
  const slash = key.indexOf("/");

  return [key.slice(0, slash), key.slice(slash + 1)];
}

// currentReplicas reads spec.replicas from an unstructured object, defaulting to 0.
function currentReplicas(obj: K8sObject): number {
  const spec = obj.spec as { replicas?: number } | undefined;

  return spec?.replicas ?? 0;
}

// podContainers reads spec.containers[].name from a Pod object (for the logs/exec container picker).
function podContainers(obj: K8sObject): string[] {
  const spec = obj.spec as { containers?: { name?: string }[] } | undefined;

  return (spec?.containers ?? []).map((container) => container.name ?? "").filter((name) => name !== "");
}

// ContainerPicker renders a container selector for a multi-container Pod (nothing for single-container
// Pods). Shared by the Logs and Exec slide-overs so the picker markup lives in one place.
function ContainerPicker({
  pod,
  value,
  onChange,
}: {
  pod: K8sObject;
  value: string;
  onChange: (value: string) => void;
}) {
  const containers = podContainers(pod);
  if (containers.length <= 1) {
    return null;
  }

  return (
    <SelectField label="Container" value={value} onChange={(event) => onChange(event.target.value)}>
      {containers.map((name) => (
        <option key={name} value={name}>
          {name}
        </option>
      ))}
    </SelectField>
  );
}

// resourceStatus derives an at-a-glance health/reconcile status for the table: a Ready condition
// (Flux/ArgoCD GitOps CRs and many others expose one), else a phase (Pods, PVCs, …). Returns null when
// the object carries no recognizable status.
function resourceStatus(obj: K8sObject): { label: string; ok: boolean } | null {
  const status = obj.status as
    | { conditions?: { type?: string; status?: string; reason?: string }[]; phase?: string }
    | undefined;
  if (!status) {
    return null;
  }

  const ready = status.conditions?.find((condition) => condition.type === "Ready");
  if (ready) {
    if (ready.status === "True") {
      return { label: "Ready", ok: true };
    }

    return { label: ready.reason ? `Not Ready: ${ready.reason}` : "Not Ready", ok: false };
  }

  if (typeof status.phase === "string" && status.phase !== "") {
    const healthy = ["Running", "Active", "Succeeded", "Bound", "Available"].includes(status.phase);

    return { label: status.phase, ok: healthy };
  }

  return null;
}

// StatusBadge renders a resource's derived status with a colour dot (green = ok, amber = not).
function StatusBadge({ obj }: { obj: K8sObject }) {
  const status = resourceStatus(obj);
  if (!status) {
    return <span className="text-slate-400">—</span>;
  }

  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={cx("size-1.5 rounded-full", status.ok ? "bg-emerald-500" : "bg-amber-500")} aria-hidden />
      <span className={status.ok ? "text-slate-600 dark:text-slate-300" : "text-amber-700 dark:text-amber-400"}>
        {status.label}
      </span>
    </span>
  );
}

// ResourcesView is the read-only workload browser: pick a cluster + resource kind, optionally filter
// by namespace (client-side), and inspect any object's raw manifest. Backed by the ResourceService
// endpoints; shown only when the backend advertises capabilities.workloadRead.
export function ResourcesView({
  clusters,
  canWrite,
  canApply,
  canLogs,
  canExec,
}: {
  clusters: Cluster[];
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
  const [selectedClusterKey, setSelectedClusterKey] = useState(clusters[0] ? clusterKey(clusters[0]) : "");
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

  // Seed the scale input from the selected object's current replica count whenever it changes.
  useEffect(() => {
    if (selected) {
      setScaleValue(String(currentReplicas(selected)));
    }
  }, [selected]);

  // runAction performs a write action on the selected resource, then toasts the outcome, closes the
  // detail panel, and refetches the list so it reflects the change.
  function runAction(verb: string, perform: () => Promise<void>) {
    const targetName = selected?.metadata?.name ?? "resource";

    setActionBusy(true);
    perform()
      .then(() => {
        toast.success(`${verb} ${targetName}`);
        setSelected(null);
        setDeleteOpen(false);
        setNonce((value) => value + 1);
      })
      .catch((err: unknown) => toast.error(errorMessage(err)))
      .finally(() => setActionBusy(false));
  }

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

    const [clusterNamespace, clusterName] = splitClusterKey(selectedClusterKey);

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
    return <EmptyState title="No clusters" description="Create or connect a cluster to browse its resources." />;
  }

  const [selectedNamespace, selectedName] = splitClusterKey(selectedClusterKey);

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
        <SelectField label="Kind" value={kind} onChange={(event) => setKind(event.target.value)} className="min-w-40">
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
        {canApply && selectedClusterKey !== "" ? (
          <Button variant="secondary" onClick={() => setApplyOpen(true)}>
            <FileCode className="size-4" aria-hidden />
            Apply YAML
          </Button>
        ) : null}
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
                  <th className={th}>Status</th>
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
                    <td className={cx(td, "hidden text-sm text-slate-600 sm:table-cell dark:text-slate-300")}>
                      {item.metadata?.namespace ?? "—"}
                    </td>
                    <td className={cx(td, "text-sm")}>
                      <StatusBadge obj={item} />
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
          <div className="space-y-3">
            {canWrite || ((canLogs || canExec) && kind === "Pod") ? (
              <div className="flex flex-wrap items-center gap-2 border-b border-slate-200 pb-3 dark:border-slate-800">
                {canWrite && SCALABLE_KINDS.includes(kind) ? (
                  <form
                    className="flex items-center gap-1.5"
                    onSubmit={(event) => {
                      event.preventDefault();
                      const replicas = scaleValue.trim() === "" ? Number.NaN : Number(scaleValue);
                      if (!Number.isInteger(replicas) || replicas < 0) {
                        toast.error("Enter a non-negative whole number of replicas");

                        return;
                      }
                      const [namespace, clusterName] = splitClusterKey(selectedClusterKey);
                      runAction("Scaled", () =>
                        scaleResource(
                          {
                            namespace,
                            name: clusterName,
                            kind,
                            resourceName: selected.metadata?.name ?? "",
                            resourceNamespace: selected.metadata?.namespace,
                          },
                          replicas,
                        ),
                      );
                    }}
                  >
                    <span className="text-xs text-slate-500 dark:text-slate-400">Replicas</span>
                    <input
                      type="number"
                      min={0}
                      value={scaleValue}
                      onChange={(event) => setScaleValue(event.target.value)}
                      className="w-20 rounded-md border border-slate-300 bg-white px-2 py-1 text-sm dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
                    />
                    <Button type="submit" size="sm" variant="secondary" loading={actionBusy}>
                      Scale
                    </Button>
                  </form>
                ) : null}
                {canWrite && RESTARTABLE_KINDS.includes(kind) ? (
                  <Button
                    size="sm"
                    variant="secondary"
                    loading={actionBusy}
                    onClick={() => {
                      const [namespace, clusterName] = splitClusterKey(selectedClusterKey);
                      runAction("Restarted", () =>
                        restartResource({
                          namespace,
                          name: clusterName,
                          kind,
                          resourceName: selected.metadata?.name ?? "",
                          resourceNamespace: selected.metadata?.namespace,
                        }),
                      );
                    }}
                  >
                    Restart
                  </Button>
                ) : null}
                {canLogs && kind === "Pod" ? (
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => {
                      const containers = podContainers(selected);
                      setLogContainer(containers[0] ?? "");
                      setLogPod(selected);
                      setSelected(null);
                    }}
                  >
                    <ScrollText className="size-3.5" aria-hidden />
                    Logs
                  </Button>
                ) : null}
                {canExec && kind === "Pod" ? (
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => {
                      const containers = podContainers(selected);
                      setExecContainer(containers[0] ?? "");
                      setExecPod(selected);
                      setSelected(null);
                    }}
                  >
                    <SquareTerminal className="size-3.5" aria-hidden />
                    Exec
                  </Button>
                ) : null}
                {canWrite && !CLUSTER_SCOPED_KINDS.includes(kind) ? (
                  <Button size="sm" variant="danger" disabled={actionBusy} onClick={() => setDeleteOpen(true)}>
                    Delete
                  </Button>
                ) : null}
              </div>
            ) : null}
            <pre className="overflow-x-auto rounded-lg bg-slate-50 p-3 text-xs leading-relaxed text-slate-800 dark:bg-slate-800/50 dark:text-slate-200">
              {JSON.stringify(selected, null, 2)}
            </pre>
          </div>
        ) : null}
      </SlideOver>

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
        onConfirm={async () => {
          const [namespace, clusterName] = splitClusterKey(selectedClusterKey);
          const targetName = selected?.metadata?.name ?? "";
          try {
            await deleteResource({
              namespace,
              name: clusterName,
              kind,
              resourceName: targetName,
              resourceNamespace: selected?.metadata?.namespace,
            });
            toast.success(`Deleted ${targetName}`);
            setSelected(null);
            setNonce((value) => value + 1);
          } catch (err) {
            toast.error(errorMessage(err));
            throw err;
          }
        }}
        onClose={() => setDeleteOpen(false)}
      />

      <ApplyManifestsDialog
        open={applyOpen}
        onClose={() => setApplyOpen(false)}
        clusterNamespace={selectedNamespace}
        clusterName={selectedName}
        onApplied={() => setNonce((value) => value + 1)}
      />

      <SlideOver
        open={logPod !== null}
        onClose={() => setLogPod(null)}
        title={`Logs · ${logPod?.metadata?.name ?? ""}`}
        subtitle={logPod?.metadata?.namespace ? `namespace: ${logPod.metadata.namespace}` : ""}
      >
        {logPod ? (
          <div className="space-y-3">
            <ContainerPicker pod={logPod} value={logContainer} onChange={setLogContainer} />
            <LogViewer
              clusterNamespace={selectedNamespace}
              clusterName={selectedName}
              podNamespace={logPod.metadata?.namespace ?? ""}
              pod={logPod.metadata?.name ?? ""}
              container={logContainer}
            />
          </div>
        ) : null}
      </SlideOver>

      <SlideOver
        open={execPod !== null}
        onClose={() => setExecPod(null)}
        title={`Exec · ${execPod?.metadata?.name ?? ""}`}
        subtitle={execPod?.metadata?.namespace ? `namespace: ${execPod.metadata.namespace}` : ""}
      >
        {execPod ? (
          <div className="space-y-3">
            <ContainerPicker pod={execPod} value={execContainer} onChange={setExecContainer} />
            <Suspense
              fallback={<div className="text-sm text-slate-500 dark:text-slate-400">Loading terminal…</div>}
            >
              <ExecTerminal
                clusterNamespace={selectedNamespace}
                clusterName={selectedName}
                podNamespace={execPod.metadata?.namespace ?? ""}
                pod={execPod.metadata?.name ?? ""}
                container={execContainer}
              />
            </Suspense>
          </div>
        ) : null}
      </SlideOver>
    </div>
  );
}
