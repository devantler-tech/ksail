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
import { useEffect, useState } from "react";
import { downloadKubeconfig, errorMessage, type Cluster, type Condition } from "../api.ts";
import { cx } from "../lib/cx.ts";
import { formatTimestamp, relativeAge } from "../lib/format.ts";
import { clusterKey, isHostCluster, splitClusterKey } from "../lib/k8s.ts";
import { loadHealth, type LiveHealth, type PodSegment } from "../lib/health.ts";
import { COMPONENT_LABELS, useMeta } from "../lib/meta.ts";
import { Card, Field } from "./Card.tsx";
import { EventList } from "./EventList.tsx";
import { ResourceUsagePanel } from "./ResourceUsage.tsx";
import { HostBadge, StatusBadge, StatusDot } from "./StatusBadge.tsx";
import { EmptyState } from "./states.tsx";
import { Button } from "./ui.tsx";
import { useToast } from "./Toast.tsx";

// WORKLOAD_PLACEHOLDERS keeps the Workloads card's layout stable while live health is still loading
// (counts render as an em dash instead of a blank card).
const WORKLOAD_PLACEHOLDERS: { label: string; count?: number }[] = [
  { label: "Deployments" },
  { label: "StatefulSets" },
  { label: "DaemonSets" },
  { label: "Pods" },
];

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
    return <CircleAlert className="size-4 shrink-0 text-amber-500" aria-hidden />;
  }

  return <CircleHelp className="size-4 shrink-0 text-slate-400" aria-hidden />;
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
  // The host cluster's registration carries an empty spec on purpose (the operator does not manage
  // the hub's lifecycle), so the create-form defaults would mislabel it — show "—" instead.
  const hostCluster = isHostCluster(cluster);
  const distribution = spec?.distribution || (hostCluster ? "—" : meta.distributions[0] || "—");
  const provider = spec?.provider || (hostCluster ? "—" : meta.providers[distribution]?.[0] || "—");
  const secret = status?.kubeconfigSecretRef;
  const nodesHealthy = health ? health.nodesTotal > 0 && health.nodesReady === health.nodesTotal : false;

  // The local surface discovers clusters and only knows their distribution/provider; a spec carrying
  // node counts or component choices is a managed one (operator CR or form submission) whose
  // component defaults are meaningful. For discovered clusters, live node facts fill the gaps and
  // fabricated component defaults are not shown.
  const specManaged =
    spec !== undefined &&
    (spec.controlPlanes !== undefined || spec.workers !== undefined || meta.components.some((component) => spec[component.key]));
  const liveWorkers = health ? health.nodesTotal - health.controlPlanes : undefined;

  const crConditions = status?.conditions ?? [];
  const shownConditions = crConditions.length > 0 ? crConditions : (health?.derivedConditions ?? []);

  // The backend's creation timestamp when it tracks one (operator CRs), else the live cluster's own
  // age (kube-system's creation time).
  const createdAt = cluster.metadata.creationTimestamp ?? health?.createdAt;

  return (
    <div className="mx-auto max-w-6xl space-y-5">
      {/* Cluster header: identity, status, and lifecycle actions. */}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2.5">
            <h2 className="truncate text-xl font-semibold text-slate-900 dark:text-white">{cluster.metadata.name}</h2>
            <StatusBadge phase={status?.phase} />
            {hostCluster ? <HostBadge /> : null}
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
          {/* Lifecycle actions are hidden for the host cluster (the cluster the operator runs on);
              the API rejects them server-side anyway. */}
          {canEdit && !hostCluster ? (
            <Button variant="secondary" size="sm" onClick={() => onEdit(cluster)}>
              <Pencil className="size-3.5" aria-hidden />
              Edit
            </Button>
          ) : null}
          {canDelete && !hostCluster ? (
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

      {/* Resource usage: cluster-wide gauges, per-node utilisation, top consumers. */}
      {canBrowse ? (
        <ResourceUsagePanel usage={health?.usage ?? null} topPods={health?.topPods ?? null} loading={loading && !health} />
      ) : null}

      {/* Cluster spec, status, conditions, and (when available) recent warnings. */}
      <div className="grid gap-4 lg:grid-cols-3">
        <Card title="Spec" icon={<Server className="size-3.5" aria-hidden />}>
          <dl className="divide-y divide-slate-100 dark:divide-slate-800">
            <Field label="Distribution">{distribution}</Field>
            <Field label="Provider">{provider}</Field>
            <Field label="Control planes">{spec?.controlPlanes ?? health?.controlPlanes ?? "—"}</Field>
            <Field label="Workers">{spec?.workers ?? liveWorkers ?? "—"}</Field>
            {specManaged ? (
              meta.components.map((component) => (
                <Field key={component.key} label={COMPONENT_LABELS[component.key] ?? component.key}>
                  {spec?.[component.key] || component.default}
                </Field>
              ))
            ) : (
              <p className="pt-2 text-xs text-slate-400 dark:text-slate-500">
                Component configuration is not tracked for {hostCluster ? "the host cluster" : "discovered clusters"} —
                browse Resources to see what runs here.
              </p>
            )}
          </dl>
        </Card>

        <Card title="Status" icon={<Activity className="size-3.5" aria-hidden />}>
          <dl className="divide-y divide-slate-100 dark:divide-slate-800">
            <Field label="Phase">{status?.phase ?? "—"}</Field>
            <Field label="Endpoint">{status?.endpoint ? <CopyableEndpoint endpoint={status.endpoint} /> : "—"}</Field>
            <Field label="Kubernetes">{health?.kubernetesVersion || "—"}</Field>
            <Field label="OS">{health?.osImage || "—"}</Field>
            <Field label="Nodes">
              {status?.nodesTotal !== undefined
                ? `${status.nodesReady ?? 0} / ${status.nodesTotal} ready`
                : health
                  ? `${health.nodesReady} / ${health.nodesTotal} ready`
                  : "—"}
            </Field>
            <Field label="Created">
              <span title={formatTimestamp(createdAt)}>{relativeAge(createdAt)}</span>
            </Field>
            {status?.lastReconcileTime ? (
              <Field label="Last reconcile">
                <span title={formatTimestamp(status.lastReconcileTime)}>{relativeAge(status.lastReconcileTime)}</span>
              </Field>
            ) : null}
            {secret ? (
              <Field label="Kubeconfig">
                <span className="font-mono text-xs">{(secret.namespace ? `${secret.namespace}/` : "") + secret.name}</span>
              </Field>
            ) : null}
          </dl>
        </Card>

        <Card title="Conditions" icon={<CircleCheck className="size-3.5" aria-hidden />}>
          {shownConditions.length === 0 ? (
            <p className="text-sm text-slate-500 dark:text-slate-400">No conditions reported.</p>
          ) : (
            <>
              {crConditions.length === 0 ? (
                <p className="mb-2 text-[10px] font-semibold uppercase tracking-wide text-slate-400 dark:text-slate-500">
                  Live health checks
                </p>
              ) : null}
              <ul className="space-y-2">
                {shownConditions.map((condition) => (
                  <li key={condition.type} className="flex items-start gap-2">
                    {conditionIcon(condition.status)}
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-sm font-medium text-slate-800 dark:text-slate-100">{condition.type}</span>
                        {condition.lastTransitionTime ? (
                          <span className="shrink-0 text-xs tabular-nums text-slate-400">
                            {relativeAge(condition.lastTransitionTime)}
                          </span>
                        ) : null}
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
            </>
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
            <StatusDot tone={segment.dot} />
            <span className="flex-1">{segment.label}</span>
            <span className="tabular-nums text-slate-500 dark:text-slate-400">{segment.count}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
