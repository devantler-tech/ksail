import { Gauge } from "lucide-react";
import { cx } from "../lib/cx.ts";
import { formatBytes, formatCores, percentOf } from "../lib/quantity.ts";
import {
  topConsumers,
  type ClusterUsage,
  type NodeUsage,
  type PodConsumption,
  type ResourceTotals,
} from "../lib/usage.ts";
import { Card } from "./Card.tsx";
import { StatusDot } from "./StatusBadge.tsx";

// Dimension describes one monitored resource axis, so the cluster gauges, per-node bars, and
// top-consumer lists all render from the same two descriptors instead of duplicated CPU/memory markup.
interface Dimension {
  key: "cpu" | "memory";
  label: string;
  format: (value: number) => string;
}

const DIMENSIONS: Dimension[] = [
  { key: "cpu", label: "CPU", format: formatCores },
  { key: "memory", label: "Memory", format: formatBytes },
];

const TOP_CONSUMERS = 5;

// Utilisation traffic-light tones. Tailwind extracts class names statically, so the full literal
// strings live here rather than being assembled from a colour fragment.
const STROKE_TONES = {
  ok: "stroke-emerald-500",
  warn: "stroke-amber-500",
  hot: "stroke-red-500",
  none: "stroke-slate-300 dark:stroke-slate-600",
} as const;

const BAR_TONES = {
  ok: "bg-emerald-500",
  warn: "bg-amber-500",
  hot: "bg-red-500",
  none: "bg-slate-300 dark:bg-slate-600",
} as const;

function toneKey(percent: number | undefined): keyof typeof STROKE_TONES {
  if (percent === undefined) {
    return "none";
  }
  if (percent >= 90) {
    return "hot";
  }

  return percent >= 75 ? "warn" : "ok";
}

// percentLabel renders a percentage for display ("—" when unknown).
function percentLabel(percent: number | undefined): string {
  return percent === undefined ? "—" : `${Math.round(percent)}%`;
}

// totalsTitle is the hover tooltip carrying the precise numbers behind a gauge or bar.
function totalsTitle(dimension: Dimension, totals: ResourceTotals): string {
  const parts = [];
  if (totals.usage !== undefined) {
    parts.push(`used ${dimension.format(totals.usage)}`);
  }

  parts.push(`requested ${dimension.format(totals.requests)}`);

  if (totals.limits > 0) {
    parts.push(`limits ${dimension.format(totals.limits)}`);
  }

  parts.push(`allocatable ${dimension.format(totals.allocatable)}`);

  return `${dimension.label}: ${parts.join(" · ")}`;
}

// Donut is an SVG ring gauge with the percentage in its centre and explanatory lines beside it.
function Donut({ percent, label, lines }: { percent: number | undefined; label: string; lines: string[] }) {
  const radius = 40;
  const circumference = 2 * Math.PI * radius;
  const filled = ((percent ?? 0) / 100) * circumference;

  return (
    <div
      className="flex items-center gap-3"
      role="meter"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={percent === undefined ? undefined : Math.round(percent)}
    >
      <div className="relative size-20 shrink-0">
        <svg viewBox="0 0 96 96" className="size-full -rotate-90" aria-hidden>
          <circle cx={48} cy={48} r={radius} fill="none" strokeWidth={9} className="stroke-slate-100 dark:stroke-slate-800" />
          <circle
            cx={48}
            cy={48}
            r={radius}
            fill="none"
            strokeWidth={9}
            strokeLinecap="round"
            strokeDasharray={`${filled} ${circumference - filled}`}
            className={cx("transition-[stroke-dasharray] duration-500", STROKE_TONES[toneKey(percent)])}
          />
        </svg>
        <span className="absolute inset-0 flex items-center justify-center text-sm font-semibold tabular-nums text-slate-900 dark:text-white">
          {percentLabel(percent)}
        </span>
      </div>
      <div className="min-w-0">
        <p className="text-sm font-medium text-slate-800 dark:text-slate-100">{label}</p>
        {lines.map((line) => (
          <p key={line} className="text-xs text-slate-500 dark:text-slate-400">
            {line}
          </p>
        ))}
      </div>
    </div>
  );
}

// ClusterGauge summarizes one dimension cluster-wide: live usage when the metrics API serves it,
// otherwise scheduled requests, always as a share of what is allocatable across all nodes.
function ClusterGauge({ dimension, totals }: { dimension: Dimension; totals: ResourceTotals }) {
  const usagePercent = percentOf(totals.usage, totals.allocatable);
  const requestsPercent = percentOf(totals.requests, totals.allocatable);

  const lines = [
    totals.usage !== undefined
      ? `${dimension.format(totals.usage)} of ${dimension.format(totals.allocatable)} used`
      : `${dimension.format(totals.allocatable)} allocatable`,
    `${dimension.format(totals.requests)} requested${requestsPercent === undefined ? "" : ` (${Math.round(requestsPercent)}%)`}`,
  ];

  return (
    <div title={totalsTitle(dimension, totals)}>
      <Donut percent={usagePercent ?? requestsPercent} label={dimension.label} lines={lines} />
    </div>
  );
}

// UsageBar is one node's utilisation of a dimension: fill = live usage (or requests without a
// metrics API), tick marker = requests, both as a share of the node's allocatable.
function UsageBar({ dimension, totals }: { dimension: Dimension; totals: ResourceTotals }) {
  const usagePercent = percentOf(totals.usage, totals.allocatable);
  const requestsPercent = percentOf(totals.requests, totals.allocatable);
  const percent = usagePercent ?? requestsPercent;

  return (
    <div className="flex items-center gap-2" title={totalsTitle(dimension, totals)}>
      <div
        className="relative h-2 flex-1 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800"
        role="meter"
        aria-label={dimension.label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent === undefined ? undefined : Math.round(percent)}
      >
        <div
          className={cx("h-full rounded-full transition-[width] duration-500", BAR_TONES[toneKey(percent)])}
          style={{ width: `${percent ?? 0}%` }}
        />
        {usagePercent !== undefined && requestsPercent !== undefined ? (
          <div
            className="absolute inset-y-0 w-0.5 bg-slate-400 dark:bg-slate-500"
            style={{ left: `${requestsPercent}%` }}
            aria-hidden
          />
        ) : null}
      </div>
      <span className="w-9 shrink-0 text-right text-xs tabular-nums text-slate-500 dark:text-slate-400">
        {percentLabel(percent)}
      </span>
    </div>
  );
}

// nodeListGrid keeps the header row and node rows on the same column raster.
const nodeListGrid = "grid grid-cols-[minmax(0,1.2fr)_1fr_1fr_3.5rem] items-center gap-3";

function NodeRow({ node }: { node: NodeUsage }) {
  return (
    <li className={cx(nodeListGrid, "py-2")}>
      <div className="flex min-w-0 items-center gap-2">
        <StatusDot
          tone={node.ready ? "ok" : "error"}
          className="shrink-0"
          title={node.ready ? "Ready" : "Not ready"}
        />
        <span className="truncate font-mono text-xs text-slate-700 dark:text-slate-200" title={node.name}>
          {node.name}
        </span>
        {node.controlPlane ? (
          <span className="shrink-0 rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-slate-500 dark:bg-slate-800 dark:text-slate-400">
            CP
          </span>
        ) : null}
      </div>
      {DIMENSIONS.map((dimension) => (
        <UsageBar key={dimension.key} dimension={dimension} totals={node[dimension.key]} />
      ))}
      <span className="text-right text-xs tabular-nums text-slate-500 dark:text-slate-400">
        {node.pods.count}
        {node.pods.allocatable > 0 ? ` / ${node.pods.allocatable}` : ""}
      </span>
    </li>
  );
}

// ConsumerList is the top-N pods for one dimension, from the metrics API.
function ConsumerList({ dimension, pods }: { dimension: Dimension; pods: PodConsumption[] }) {
  return (
    <div>
      <h4 className="mb-1.5 text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400">
        Top pods by {dimension.label.toLowerCase()}
      </h4>
      {pods.length === 0 ? (
        <p className="text-xs text-slate-500 dark:text-slate-400">No usage reported.</p>
      ) : (
        <ul className="space-y-1">
          {pods.map((pod) => (
            <li key={`${pod.namespace}/${pod.name}`} className="flex items-baseline justify-between gap-2 text-xs">
              <span className="min-w-0 truncate text-slate-600 dark:text-slate-300" title={`${pod.namespace}/${pod.name}`}>
                <span className="text-slate-400 dark:text-slate-500">{pod.namespace}/</span>
                {pod.name}
              </span>
              <span className="shrink-0 tabular-nums text-slate-500 dark:text-slate-400">
                {dimension.format(pod[dimension.key])}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// ResourceUsagePanel is the Overview's monitoring section: cluster-wide CPU/memory/pod gauges, a
// per-node utilisation breakdown, and the top pod consumers when a metrics API is available.
export function ResourceUsagePanel({
  usage,
  topPods,
  loading,
}: {
  usage: ClusterUsage | null;
  topPods: PodConsumption[] | null;
  loading: boolean;
}) {
  return (
    <Card title="Resource usage" icon={<Gauge className="size-3.5" aria-hidden />}>
      {!usage ? (
        <p className="text-sm text-slate-500 dark:text-slate-400">
          {loading ? "Loading…" : "Resource usage is unavailable."}
        </p>
      ) : usage.nodes.length === 0 ? (
        <p className="text-sm text-slate-500 dark:text-slate-400">No nodes reported.</p>
      ) : (
        <div className="space-y-4">
          {!usage.metricsAvailable ? (
            <p className="rounded-lg bg-slate-50 px-3 py-2 text-xs text-slate-500 dark:bg-slate-800/60 dark:text-slate-400">
              Live usage is unavailable on this cluster (no metrics API serving metrics.k8s.io) — gauges and bars
              show requested resources instead.
            </p>
          ) : null}

          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {DIMENSIONS.map((dimension) => (
              <ClusterGauge key={dimension.key} dimension={dimension} totals={usage[dimension.key]} />
            ))}
            <Donut
              percent={percentOf(usage.pods.count, usage.pods.allocatable)}
              label="Pods"
              lines={[`${usage.pods.count} of ${usage.pods.allocatable} schedulable pods`]}
            />
          </div>

          <div>
            <div className={cx(nodeListGrid, "border-b border-slate-100 pb-1.5 dark:border-slate-800")}>
              {["Node", "CPU", "Memory", "Pods"].map((heading) => (
                <span
                  key={heading}
                  className="text-[10px] font-semibold uppercase tracking-wide text-slate-400 last:text-right dark:text-slate-500"
                >
                  {heading}
                </span>
              ))}
            </div>
            <ul className="divide-y divide-slate-100 dark:divide-slate-800">
              {usage.nodes.map((node) => (
                <NodeRow key={node.name} node={node} />
              ))}
            </ul>
            {usage.metricsAvailable ? (
              <p className="mt-1.5 text-[10px] text-slate-400 dark:text-slate-500">
                Bar = live usage · tick = requested · % of allocatable
              </p>
            ) : null}
          </div>

          {usage.metricsAvailable && topPods && topPods.length > 0 ? (
            <div className="grid gap-4 border-t border-slate-100 pt-3 sm:grid-cols-2 dark:border-slate-800">
              {DIMENSIONS.map((dimension) => (
                <ConsumerList
                  key={dimension.key}
                  dimension={dimension}
                  pods={topConsumers(topPods, dimension.key, TOP_CONSUMERS)}
                />
              ))}
            </div>
          ) : null}
        </div>
      )}
    </Card>
  );
}
