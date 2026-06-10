import {
  Activity,
  Boxes,
  CircleAlert,
  CircleCheck,
  CircleHelp,
  Copy,
  Download,
  Layers,
  Pencil,
  RotateCw,
  Server,
  Trash2,
  TriangleAlert,
} from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { downloadKubeconfig, errorMessage, listResources, type Cluster, type Condition, type K8sObject } from "../api.ts";
import { cx } from "../lib/cx.ts";
import { formatTimestamp, relativeAge } from "../lib/format.ts";
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
import { COMPONENT_LABELS, useMeta } from "../lib/meta.ts";
import { EventList } from "./EventList.tsx";
import { StatusBadge } from "./StatusBadge.tsx";
import { EmptyState } from "./states.tsx";
import { Button } from "./ui.tsx";
import { useToast } from "./Toast.tsx";

// PodSegment is one slice of the pod-health bar: a label, a count, and its bar/dot colour classes.
type PodSegment = { key: string; label: string; count: number; bar: string; dot: string };

// LiveHealth is the at-a-glance cluster health derived from the read-only resource endpoints.
interface LiveHealth {
  nodesReady: number;
  nodesTotal: number;
  segments: PodSegment[];
  podsTotal: number;
  workloads: { label: string; count: number }[];
  warnings: EventFields[];
}

const MAX_WARNINGS = 8;

// WORKLOAD_PLACEHOLDERS keeps the Workloads card's layout stable while live health is still loading
// (counts render as an em dash instead of a blank card).
const WORKLOAD_PLACEHOLDERS: { label: string; count?: number }[] = [
  { label: "Deployments" },
  { label: "StatefulSets" },
  { label: "DaemonSets" },
  { label: "Pods" },
];

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

// listKind lists a kind for a cluster, returning [] on error so one missing/forbidden kind never blanks
// the whole dashboard.
async function listKind(namespace: string, name: string, kind: string): Promise<K8sObject[]> {
  try {
    const result = await listResources(namespace, name, kind);

    return result.items ?? [];
  } catch {
    return [];
  }
}

async function loadHealth(namespace: string, name: string): Promise<LiveHealth> {
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

function Card({
  title,
  icon,
  children,
  className,
}: {
  title: string;
  icon: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cx(
        "rounded-xl border border-slate-200 bg-white p-4 shadow-sm dark:border-slate-800 dark:bg-slate-900",
        className,
      )}
    >
      <div className="mb-3 flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400">
        {icon}
        {title}
      </div>
      {children}
    </div>
  );
}

// Field renders a label/value row in the Spec and Status cards.
function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-1.5">
      <dt className="shrink-0 text-xs text-slate-500 dark:text-slate-400">{label}</dt>
      <dd className="min-w-0 truncate text-right text-sm text-slate-700 dark:text-slate-200">{children}</dd>
    </div>
  );
}

function CopyableEndpoint({ endpoint }: { endpoint: string }) {
  const toast = useToast();

  return (
    <button
      type="button"
      title="Copy endpoint"
      onClick={() => {
        navigator.clipboard
          ?.writeText(endpoint)
          .then(() => toast.success("Endpoint copied"))
          .catch(() => toast.error("Copy failed"));
      }}
      className="group inline-flex max-w-full items-center gap-1.5"
    >
      <span className="truncate font-mono text-xs text-slate-700 dark:text-slate-300">{endpoint}</span>
      <Copy className="size-3.5 shrink-0 text-slate-400 group-hover:text-slate-600 dark:group-hover:text-slate-200" aria-hidden />
    </button>
  );
}

function conditionIcon(status: Condition["status"]) {
  if (status === "True") {
    return <CircleCheck className="size-4 shrink-0 text-emerald-500" aria-hidden />;
  }
  if (status === "False") {
    return <CircleAlert className="size-4 shrink-0 text-slate-400" aria-hidden />;
  }

  return <CircleHelp className="size-4 shrink-0 text-amber-500" aria-hidden />;
}

// OverviewView is the cluster home: the cluster's spec, status, and conditions (formerly the cluster
// detail panel) alongside live health composed from the read-only resource endpoints. It operates on
// the active cluster from the workspace context — there is no cluster selector here.
export function OverviewView({
  cluster,
  canBrowse,
  canEdit,
  canDelete,
  canDownloadKubeconfig,
  onEdit,
  onDelete,
}: {
  cluster: Cluster | null;
  // canBrowse gates the live-health cards (node/pod/workload counts + warnings) on the workload-read
  // API; the cluster's own spec/status/conditions render regardless.
  canBrowse: boolean;
  canEdit: boolean;
  canDelete: boolean;
  canDownloadKubeconfig: boolean;
  onEdit: (cluster: Cluster) => void;
  onDelete: (cluster: Cluster) => void;
}) {
  const meta = useMeta();
  const toast = useToast();
  const [health, setHealth] = useState<LiveHealth | null>(null);
  const [loading, setLoading] = useState(false);
  const [nonce, setNonce] = useState(0);
  const [downloading, setDownloading] = useState(false);

  const key = cluster ? clusterKey(cluster) : "";

  useEffect(() => {
    if (key === "" || !canBrowse) {
      setHealth(null);

      return undefined;
    }

    const [namespace, name] = splitClusterKey(key);
    let cancelled = false;
    setLoading(true);

    // loadHealth never rejects — each per-kind fetch swallows its own error (see listKind) so one
    // missing/forbidden kind degrades to empty cards rather than failing the whole dashboard.
    loadHealth(namespace, name)
      .then((result) => {
        if (!cancelled) {
          setHealth(result);
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
  }, [key, canBrowse, nonce]);

  if (!cluster) {
    return <EmptyState title="No cluster selected" description="Choose a cluster to see its overview." />;
  }

  const status = cluster.status;
  const spec = cluster.spec?.cluster;
  const namespace = cluster.metadata.namespace ?? "default";
  const distribution = spec?.distribution || meta.distributions[0] || "—";
  const provider = spec?.provider || meta.providers[distribution]?.[0] || "—";
  const secret = status?.kubeconfigSecretRef;
  const conditions = status?.conditions ?? [];
  const nodesHealthy = health ? health.nodesTotal > 0 && health.nodesReady === health.nodesTotal : false;

  return (
    <div className="mx-auto max-w-6xl space-y-5">
      {/* Cluster header: identity, status, and lifecycle actions. */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2.5">
            <h2 className="truncate text-xl font-semibold text-slate-900 dark:text-white">{cluster.metadata.name}</h2>
            <StatusBadge phase={status?.phase} />
          </div>
          <p className="mt-0.5 text-sm text-slate-500 dark:text-slate-400">
            {distribution} · {provider} · namespace {namespace}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {canBrowse ? (
            <Button variant="secondary" size="sm" onClick={() => setNonce((value) => value + 1)} loading={loading}>
              {loading ? null : <RotateCw className="size-3.5" aria-hidden />}
              Refresh
            </Button>
          ) : null}
          {canDownloadKubeconfig ? (
            <Button
              variant="secondary"
              size="sm"
              loading={downloading}
              onClick={() => {
                setDownloading(true);
                downloadKubeconfig(namespace, cluster.metadata.name)
                  .catch((err: unknown) => toast.error(errorMessage(err)))
                  .finally(() => setDownloading(false));
              }}
            >
              {downloading ? null : <Download className="size-3.5" aria-hidden />}
              Kubeconfig
            </Button>
          ) : null}
          {canEdit ? (
            <Button variant="secondary" size="sm" onClick={() => onEdit(cluster)}>
              <Pencil className="size-3.5" aria-hidden />
              Edit
            </Button>
          ) : null}
          {canDelete ? (
            <Button variant="danger" size="sm" onClick={() => onDelete(cluster)}>
              <Trash2 className="size-3.5" aria-hidden />
              Delete
            </Button>
          ) : null}
        </div>
      </div>

      {/* Live health (workload-read only). */}
      {canBrowse ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <Card title="Nodes" icon={<Boxes className="size-3.5" aria-hidden />}>
            <div className="flex items-baseline gap-2">
              <span
                className={cx(
                  "text-2xl font-semibold tabular-nums",
                  nodesHealthy ? "text-slate-900 dark:text-white" : "text-amber-600 dark:text-amber-400",
                )}
              >
                {health ? `${health.nodesReady}/${health.nodesTotal}` : "—"}
              </span>
              <span className="text-xs text-slate-500 dark:text-slate-400">ready</span>
            </div>
            <div className="mt-3 h-2 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
              <div
                className={cx(
                  "h-full rounded-full transition-[width] duration-500",
                  nodesHealthy ? "bg-emerald-500" : "bg-amber-500",
                )}
                style={{
                  width: health && health.nodesTotal > 0 ? `${(health.nodesReady / health.nodesTotal) * 100}%` : "0%",
                }}
              />
            </div>
          </Card>

          <Card title="Pod health" icon={<Activity className="size-3.5" aria-hidden />}>
            {health ? (
              <PodHealth segments={health.segments} total={health.podsTotal} />
            ) : (
              <p className="text-sm text-slate-500 dark:text-slate-400">Loading…</p>
            )}
          </Card>

          <Card title="Workloads" icon={<Layers className="size-3.5" aria-hidden />}>
            <dl className="grid grid-cols-2 gap-3">
              {(health?.workloads ?? WORKLOAD_PLACEHOLDERS).map((workload) => (
                <div key={workload.label}>
                  <dt className="text-xs text-slate-500 dark:text-slate-400">{workload.label}</dt>
                  <dd className="text-xl font-semibold tabular-nums text-slate-900 dark:text-white">
                    {workload.count ?? "—"}
                  </dd>
                </div>
              ))}
            </dl>
          </Card>
        </div>
      ) : null}

      {/* Cluster spec, status, conditions, and (when available) recent warnings. */}
      <div className="grid gap-4 lg:grid-cols-3">
        <Card title="Spec" icon={<Server className="size-3.5" aria-hidden />}>
          <dl className="divide-y divide-slate-100 dark:divide-slate-800">
            <Field label="Distribution">{distribution}</Field>
            <Field label="Provider">{provider}</Field>
            <Field label="Control planes">{spec?.controlPlanes ?? 1}</Field>
            <Field label="Workers">{spec?.workers ?? 0}</Field>
            {meta.components.map((component) => (
              <Field key={component.key} label={COMPONENT_LABELS[component.key] ?? component.key}>
                {spec?.[component.key] || component.default}
              </Field>
            ))}
          </dl>
        </Card>

        <Card title="Status" icon={<Activity className="size-3.5" aria-hidden />}>
          <dl className="divide-y divide-slate-100 dark:divide-slate-800">
            <Field label="Phase">{status?.phase ?? "—"}</Field>
            <Field label="Endpoint">{status?.endpoint ? <CopyableEndpoint endpoint={status.endpoint} /> : "—"}</Field>
            <Field label="Nodes">
              {status?.nodesTotal === undefined ? "—" : `${status.nodesReady ?? 0} / ${status.nodesTotal} ready`}
            </Field>
            <Field label="Kubeconfig">
              {secret ? (
                <span className="font-mono text-xs">{(secret.namespace ? `${secret.namespace}/` : "") + secret.name}</span>
              ) : (
                "—"
              )}
            </Field>
            <Field label="Created">
              <span title={formatTimestamp(cluster.metadata.creationTimestamp)}>
                {relativeAge(cluster.metadata.creationTimestamp)}
              </span>
            </Field>
            <Field label="Last reconcile">
              <span title={formatTimestamp(status?.lastReconcileTime)}>
                {status?.lastReconcileTime ? relativeAge(status.lastReconcileTime) : "—"}
              </span>
            </Field>
          </dl>
        </Card>

        <Card title="Conditions" icon={<CircleCheck className="size-3.5" aria-hidden />}>
          {conditions.length === 0 ? (
            <p className="text-sm text-slate-500 dark:text-slate-400">No conditions reported.</p>
          ) : (
            <ul className="space-y-2">
              {conditions.map((condition) => (
                <li key={condition.type} className="flex items-start gap-2">
                  {conditionIcon(condition.status)}
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-sm font-medium text-slate-800 dark:text-slate-100">{condition.type}</span>
                      <span className="shrink-0 text-xs tabular-nums text-slate-400">
                        {relativeAge(condition.lastTransitionTime)}
                      </span>
                    </div>
                    {condition.reason ? (
                      <p className="text-xs text-slate-500 dark:text-slate-400">{condition.reason}</p>
                    ) : null}
                    {condition.message ? (
                      <p className="text-xs text-slate-500 dark:text-slate-400">{condition.message}</p>
                    ) : null}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </Card>

        {canBrowse ? (
          <Card title="Recent warnings" icon={<TriangleAlert className="size-3.5" aria-hidden />} className="lg:col-span-3">
            {loading && !health ? (
              <p className="text-sm text-slate-500 dark:text-slate-400">Loading…</p>
            ) : (health?.warnings.length ?? 0) === 0 ? (
              <p className="text-sm text-slate-500 dark:text-slate-400">No recent warnings.</p>
            ) : (
              <EventList events={health?.warnings ?? []} />
            )}
          </Card>
        ) : null}
      </div>
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
          <div
            key={segment.key}
            className={cx(segment.bar, "transition-[width] duration-500")}
            style={{ width: `${(segment.count / total) * 100}%` }}
          />
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
