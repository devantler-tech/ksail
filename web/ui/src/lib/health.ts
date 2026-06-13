// Live-health model for the Overview's dashboard: derives node and pod health, workload counts, live
// facts (Kubernetes version, OS, creation time), resource usage, and recent warnings from the
// read-only resource endpoints. Mirrors the repo's model-in-lib convention (see lib/usage.ts), so the
// derivation is unit-testable without rendering the view.

import { listResources, type Condition, type K8sObject } from "../api.ts";
import {
  nodeIsControlPlane,
  nodeProblems,
  nodeReady,
  nodeSystemInfo,
  podPhase,
  podReady,
  recentEvents,
  type EventFields,
} from "./k8s.ts";
import { buildClusterUsage, buildPodConsumption, type ClusterUsage, type PodConsumption } from "./usage.ts";
import type { StatusTone } from "../components/StatusBadge.tsx";

// PodSegment is one slice of the pod-health bar: a label, a count, its bar colour class, and the
// legend dot's tone.
export type PodSegment = { key: string; label: string; count: number; bar: string; dot: StatusTone };

// LiveHealth is the at-a-glance cluster state derived from the read-only resource endpoints: node and
// pod health, workload counts, live facts (version, OS, creation time) the cluster object itself does
// not carry on the local surface, resource usage, and recent warnings.
export interface LiveHealth {
  nodesReady: number;
  nodesTotal: number;
  controlPlanes: number;
  kubernetesVersion: string;
  osImage: string;
  createdAt?: string;
  segments: PodSegment[];
  podsTotal: number;
  workloads: { label: string; count: number }[];
  warnings: EventFields[];
  derivedConditions: Condition[];
  usage: ClusterUsage;
  topPods: PodConsumption[];
}

export const MAX_WARNINGS = 8;

// categorizePods buckets pods by phase/readiness into the pod-health bar segments.
export function categorizePods(pods: K8sObject[]): PodSegment[] {
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
    { key: "running", label: "Running", count: counts.running, bar: "bg-emerald-500", dot: "ok" },
    { key: "notReady", label: "Not ready", count: counts.notReady, bar: "bg-amber-500", dot: "warn" },
    { key: "pending", label: "Pending", count: counts.pending, bar: "bg-blue-500", dot: "info" },
    { key: "succeeded", label: "Succeeded", count: counts.succeeded, bar: "bg-slate-400", dot: "muted" },
    { key: "failed", label: "Failed", count: counts.failed, bar: "bg-red-500", dot: "error" },
    { key: "other", label: "Other", count: counts.other, bar: "bg-slate-300", dot: "muted" },
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

// distinctSummary collapses per-node values into one display string: the value every node shares, or
// the first plus how many differ ("v1.33.0 +2 more" on a mid-upgrade cluster).
export function distinctSummary(values: string[]): string {
  const distinct = [...new Set(values.filter((value) => value !== ""))];
  if (distinct.length === 0) {
    return "";
  }

  return distinct.length === 1 ? distinct[0] : `${distinct[0]} +${distinct.length - 1} more`;
}

// clusterCreatedAt approximates the cluster's creation time as the kube-system namespace's creation
// timestamp (it exists from first boot), falling back to the oldest namespace.
export function clusterCreatedAt(namespaces: K8sObject[]): string | undefined {
  const stamps = namespaces
    .map((ns) => ({ name: ns.metadata?.name, at: ns.metadata?.creationTimestamp ?? "" }))
    .filter((entry) => entry.at !== "");

  const kubeSystem = stamps.find((entry) => entry.name === "kube-system");
  if (kubeSystem) {
    return kubeSystem.at;
  }

  return stamps.map((entry) => entry.at).sort().at(0);
}

// segmentCount reads one segment's pod count by key.
export function segmentCount(segments: PodSegment[], key: string): number {
  return segments.find((segment) => segment.key === key)?.count ?? 0;
}

// deriveHealthConditions synthesizes health checks from live cluster state for clusters whose backend
// reports no status conditions (the local surface): node readiness, pod health, and the kubelets'
// pressure/availability signals. Status follows the Kubernetes convention that True is healthy.
export function deriveHealthConditions(
  nodes: K8sObject[],
  segments: PodSegment[],
  nodesReady: number,
  podsTotal: number,
): Condition[] {
  const conditions: Condition[] = [];

  if (nodes.length > 0) {
    const allReady = nodesReady === nodes.length;
    conditions.push({
      type: "NodesReady",
      status: allReady ? "True" : "False",
      reason: allReady ? "AllNodesReady" : "NodesNotReady",
      message: `${nodesReady} of ${nodes.length} nodes are ready.`,
    });

    const problems = nodes.flatMap(nodeProblems);
    conditions.push({
      type: "NodeConditions",
      status: problems.length === 0 ? "True" : "False",
      reason: problems.length === 0 ? "NoProblemsDetected" : "NodeProblems",
      message:
        problems.length === 0
          ? "No node pressure or availability problems."
          : problems.map((p) => `${p.node}: ${p.type}${p.message ? ` — ${p.message}` : ""}`).join("; "),
      lastTransitionTime: problems.map((p) => p.lastTransitionTime ?? "").sort().at(-1) || undefined,
    });
  }

  if (podsTotal > 0) {
    const unhealthy = segmentCount(segments, "failed") + segmentCount(segments, "notReady");
    const detail = [
      { count: segmentCount(segments, "failed"), label: "failed" },
      { count: segmentCount(segments, "notReady"), label: "not ready" },
      { count: segmentCount(segments, "pending"), label: "pending" },
    ]
      .filter((entry) => entry.count > 0)
      .map((entry) => `${entry.count} ${entry.label}`)
      .join(", ");

    conditions.push({
      type: "PodsHealthy",
      status: unhealthy === 0 ? "True" : "False",
      reason: unhealthy === 0 ? "AllPodsHealthy" : "UnhealthyPods",
      message: detail === "" ? `All ${segmentCount(segments, "running")} running pods are healthy.` : `${detail} of ${podsTotal} pods.`,
    });
  }

  return conditions;
}

// loadHealth composes the LiveHealth model by listing the dashboard's kinds in parallel (each fetch
// swallows its own error via listKind, so one missing/forbidden kind degrades to empty rather than
// failing the whole dashboard) and deriving node/pod/workload/usage/warning facts from them.
export async function loadHealth(namespace: string, name: string): Promise<LiveHealth> {
  const [nodes, pods, deployments, statefulSets, daemonSets, events, namespaces, nodeMetrics, podMetrics] =
    await Promise.all([
      listKind(namespace, name, "Node"),
      listKind(namespace, name, "Pod"),
      listKind(namespace, name, "Deployment"),
      listKind(namespace, name, "StatefulSet"),
      listKind(namespace, name, "DaemonSet"),
      listKind(namespace, name, "Event"),
      listKind(namespace, name, "Namespace"),
      listKind(namespace, name, "NodeMetrics"),
      listKind(namespace, name, "PodMetrics"),
    ]);

  const warnings = recentEvents(events, { type: "Warning", limit: MAX_WARNINGS });

  const segments = categorizePods(pods);
  const nodesReady = nodes.filter(nodeReady).length;
  const systemInfos = nodes.map(nodeSystemInfo);

  return {
    nodesReady,
    nodesTotal: nodes.length,
    controlPlanes: nodes.filter(nodeIsControlPlane).length,
    kubernetesVersion: distinctSummary(systemInfos.map((info) => info.kubeletVersion)),
    osImage: distinctSummary(systemInfos.map((info) => info.osImage)),
    createdAt: clusterCreatedAt(namespaces),
    segments,
    podsTotal: pods.length,
    workloads: [
      { label: "Deployments", count: deployments.length },
      { label: "StatefulSets", count: statefulSets.length },
      { label: "DaemonSets", count: daemonSets.length },
      { label: "Pods", count: pods.length },
    ],
    warnings,
    derivedConditions: deriveHealthConditions(nodes, segments, nodesReady, pods.length),
    usage: buildClusterUsage(nodes, pods, nodeMetrics),
    topPods: buildPodConsumption(podMetrics),
  };
}
