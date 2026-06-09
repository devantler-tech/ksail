import { Activity, Boxes, Layers, RotateCw, Server, TriangleAlert } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { listResources, type Cluster, type K8sObject } from "../api.ts";
import { cx } from "../lib/cx.ts";
import { relativeAge } from "../lib/format.ts";
import {
  clusterKey,
  eventFields,
  eventLastSeenMs,
  nodeReady,
  podPhase,
  podReady,
  splitClusterKey,
  type EventFields,
} from "../lib/k8s.ts";
import { StatusBadge } from "./StatusBadge.tsx";
import { EmptyState, ErrorBanner } from "./states.tsx";
import { Button, SelectField } from "./ui.tsx";

// PodSegment is one slice of the pod-health bar: a label, a count, and the Tailwind colour classes
// for the bar fill and the legend dot.
type PodSegment = { key: string; label: string; count: number; bar: string; dot: string };

// OverviewSummary is the computed at-a-glance health of a cluster, derived entirely from the existing
// read-only resource endpoints (no dedicated backend endpoint).
interface OverviewSummary {
  nodesReady: number;
  nodesTotal: number;
  segments: PodSegment[];
  podsTotal: number;
  workloads: { label: string; count: number }[];
  warnings: EventFields[];
}

// MAX_WARNINGS caps the recent-warnings panel so a noisy cluster does not produce an unbounded list.
const MAX_WARNINGS = 8;

// categorizePods buckets pods into the health segments shown in the bar. A Running pod whose
// containers are not all ready is surfaced separately ("Not ready") since it is a common failure mode
// a plain phase count hides.
function categorizePods(pods: K8sObject[]): PodSegment[] {
  const counts = { running: 0, notReady: 0, pending: 0, succeeded: 0, failed: 0, other: 0 };

  for (const pod of pods) {
    const phase = podPhase(pod);
    if (phase === "Running") {
      podReady(pod) ? (counts.running += 1) : (counts.notReady += 1);
    } else if (phase === "Pending") {
      counts.pending += 1;
    } else if (phase === "Succeeded") {
      counts.succeeded += 1;
    } else if (phase === "Failed") {
      counts.failed += 1;
    } else {
      counts.other += 1;
    }
  }

  return [
    { key: "running", label: "Running", count: counts.running, bar: "bg-emerald-500", dot: "bg-emerald-500" },
    { key: "notReady", label: "Not ready", count: counts.notReady, bar: "bg-amber-500", dot: "bg-amber-500" },
    { key: "pending", label: "Pending", count: counts.pending, bar: "bg-blue-500", dot: "bg-blue-500" },
    { key: "succeeded", label: "Succeeded", count: counts.succeeded, bar: "bg-slate-400", dot: "bg-slate-400" },
    { key: "failed", label: "Failed", count: counts.failed, bar: "bg-red-500", dot: "bg-red-500" },
    { key: "other", label: "Other", count: counts.other, bar: "bg-slate-300", dot: "bg-slate-300" },
  ];
}

// listKind lists a resource kind for a cluster, returning [] on error so one missing/forbidden kind
// (e.g. a cluster without the Events API reachable) never blanks the whole dashboard.
async function listKind(namespace: string, name: string, kind: string): Promise<K8sObject[]> {
  try {
    const result = await listResources(namespace, name, kind);

    return result.items ?? [];
  } catch {
    return [];
  }
}

async function loadSummary(namespace: string, name: string): Promise<OverviewSummary> {
  const [nodes, pods, deployments, statefulSets, daemonSets, events] = await Promise.all([
    listKind(namespace, name, "Node"),
    listKind(namespace, name, "Pod"),
    listKind(namespace, name, "Deployment"),
    listKind(namespace, name, "StatefulSet"),
    listKind(namespace, name, "DaemonSet"),
    listKind(namespace, name, "Event"),
  ]);

  const warnings = events
    .map(eventFields)
    .filter((event) => event.type === "Warning")
    .sort((a, b) => eventLastSeenMs(b) - eventLastSeenMs(a))
    .slice(0, MAX_WARNINGS);

  return {
    nodesReady: nodes.filter(nodeReady).length,
    nodesTotal: nodes.length,
    segments: categorizePods(pods),
    podsTotal: pods.length,
    workloads: [
      { label: "Deployments", count: deployments.length },
      { label: "StatefulSets", count: statefulSets.length },
      { label: "DaemonSets", count: daemonSets.length },
      { label: "Pods", count: pods.length },
    ],
    warnings,
  };
}

function Card({ title, icon, children }: { title: string; icon: ReactNode; children: ReactNode }) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm dark:border-slate-800 dark:bg-slate-900">
      <div className="mb-3 flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400">
        {icon}
        {title}
      </div>
      {children}
    </div>
  );
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

// OverviewView is a per-cluster health dashboard composed from the read-only resource endpoints:
// node readiness, a pod-health breakdown, workload counts, and recent warning events. Shown only when
// the backend advertises capabilities.workloadRead (same gate as the Resources view).
export function OverviewView({ clusters }: { clusters: Cluster[] }) {
  const [selectedClusterKey, setSelectedClusterKey] = useState(clusters[0] ? clusterKey(clusters[0]) : "");
  const [summary, setSummary] = useState<OverviewSummary | null>(null);
  const [loading, setLoading] = useState(clusters.length > 0);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);

  // Keep the selection valid as the live list changes (snap to the first cluster when empty or removed).
  useEffect(() => {
    if (clusters.length === 0) {
      return;
    }

    if (!clusters.some((candidate) => clusterKey(candidate) === selectedClusterKey)) {
      setSelectedClusterKey(clusterKey(clusters[0]));
    }
  }, [clusters, selectedClusterKey]);

  // Refetch on cluster/refresh change. Deps are the primitive key only, so live status churn over SSE
  // does not trigger spurious refetches.
  useEffect(() => {
    if (selectedClusterKey === "") {
      return undefined;
    }

    const [namespace, name] = splitClusterKey(selectedClusterKey);
    let cancelled = false;
    setLoading(true);
    setError(null);

    loadSummary(namespace, name)
      .then((result) => {
        if (!cancelled) {
          setSummary(result);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(errorMessage(err));
          setSummary(null);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [selectedClusterKey, nonce]);

  const selectedCluster = useMemo(
    () => clusters.find((candidate) => clusterKey(candidate) === selectedClusterKey) ?? null,
    [clusters, selectedClusterKey],
  );

  if (clusters.length === 0) {
    return <EmptyState title="No clusters" description="Create or connect a cluster to see its overview." />;
  }

  const nodesHealthy = summary ? summary.nodesTotal > 0 && summary.nodesReady === summary.nodesTotal : false;

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
        <Button variant="secondary" onClick={() => setNonce((value) => value + 1)} loading={loading}>
          {loading ? null : <RotateCw className="size-4" aria-hidden />}
          Refresh
        </Button>
      </div>

      {error ? (
        <ErrorBanner message={error} onRetry={() => setNonce((value) => value + 1)} />
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <Card title="Cluster" icon={<Server className="size-3.5" aria-hidden />}>
            <div className="space-y-2">
              <StatusBadge phase={selectedCluster?.status?.phase} />
              {selectedCluster?.status?.endpoint ? (
                <p className="truncate text-xs text-slate-500 dark:text-slate-400" title={selectedCluster.status.endpoint}>
                  {selectedCluster.status.endpoint}
                </p>
              ) : null}
              <p className="text-sm text-slate-600 dark:text-slate-300">
                {selectedCluster?.spec?.cluster?.distribution ?? "—"}
                {selectedCluster?.spec?.cluster?.provider ? ` · ${selectedCluster.spec.cluster.provider}` : ""}
              </p>
            </div>
          </Card>

          <Card title="Nodes" icon={<Boxes className="size-3.5" aria-hidden />}>
            <div className="flex items-baseline gap-2">
              <span
                className={cx(
                  "text-2xl font-semibold tabular-nums",
                  nodesHealthy ? "text-slate-900 dark:text-white" : "text-amber-600 dark:text-amber-400",
                )}
              >
                {summary ? `${summary.nodesReady}/${summary.nodesTotal}` : "—"}
              </span>
              <span className="text-xs text-slate-500 dark:text-slate-400">ready</span>
            </div>
            <div className="mt-3 h-2 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
              <div
                className={cx("h-full rounded-full", nodesHealthy ? "bg-emerald-500" : "bg-amber-500")}
                style={{
                  width: summary && summary.nodesTotal > 0 ? `${(summary.nodesReady / summary.nodesTotal) * 100}%` : "0%",
                }}
              />
            </div>
          </Card>

          <Card title="Pod health" icon={<Activity className="size-3.5" aria-hidden />}>
            <PodHealth segments={summary?.segments ?? []} total={summary?.podsTotal ?? 0} />
          </Card>

          <Card title="Workloads" icon={<Layers className="size-3.5" aria-hidden />}>
            <dl className="grid grid-cols-2 gap-3">
              {(summary?.workloads ?? []).map((workload) => (
                <div key={workload.label}>
                  <dt className="text-xs text-slate-500 dark:text-slate-400">{workload.label}</dt>
                  <dd className="text-xl font-semibold tabular-nums text-slate-900 dark:text-white">{workload.count}</dd>
                </div>
              ))}
            </dl>
          </Card>

          <div className="sm:col-span-2 lg:col-span-2">
            <Card title="Recent warnings" icon={<TriangleAlert className="size-3.5" aria-hidden />}>
              <Warnings warnings={summary?.warnings ?? []} loading={loading} />
            </Card>
          </div>
        </div>
      )}
    </div>
  );
}

function PodHealth({ segments, total }: { segments: PodSegment[]; total: number }) {
  if (total === 0) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">No pods.</p>;
  }

  const visible = segments.filter((segment) => segment.count > 0);

  return (
    <div>
      <div className="flex h-2 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
        {visible.map((segment) => (
          <div key={segment.key} className={segment.bar} style={{ width: `${(segment.count / total) * 100}%` }} />
        ))}
      </div>
      <ul className="mt-3 grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
        {visible.map((segment) => (
          <li key={segment.key} className="flex items-center gap-1.5 text-slate-600 dark:text-slate-300">
            <span className={cx("size-1.5 rounded-full", segment.dot)} aria-hidden />
            <span className="flex-1">{segment.label}</span>
            <span className="tabular-nums text-slate-500 dark:text-slate-400">{segment.count}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function Warnings({ warnings, loading }: { warnings: EventFields[]; loading: boolean }) {
  if (loading && warnings.length === 0) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">Loading…</p>;
  }

  if (warnings.length === 0) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">No recent warnings.</p>;
  }

  return (
    <ul className="space-y-2">
      {warnings.map((warning, index) => (
        <li key={`${warning.objectName}-${warning.reason}-${index}`} className="flex items-start gap-2 text-sm">
          <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-amber-500" aria-hidden />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <span className="font-medium text-slate-700 dark:text-slate-200">{warning.reason || "Warning"}</span>
              {warning.objectName ? (
                <span className="truncate text-xs text-slate-500 dark:text-slate-400">
                  {warning.objectKind ? `${warning.objectKind}/` : ""}
                  {warning.objectName}
                </span>
              ) : null}
              <span className="ml-auto shrink-0 text-xs tabular-nums text-slate-400">{relativeAge(warning.lastSeen)}</span>
            </div>
            <p className="truncate text-xs text-slate-500 dark:text-slate-400" title={warning.message}>
              {warning.message}
            </p>
          </div>
        </li>
      ))}
    </ul>
  );
}
